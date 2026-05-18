package crawler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/wwf5067/newsfeed/internal/crawler/digest"
	"github.com/wwf5067/newsfeed/internal/crawler/quotes"
)

// SourceHealth 记录某个 Source 的运行健康状态。
type SourceHealth struct {
	ConsecutiveFailures int       `json:"consecutive_failures"`
	LastError           string    `json:"last_error,omitempty"`
	LastSuccess         time.Time `json:"last_success,omitempty"`
	BackoffUntil        time.Time `json:"backoff_until,omitempty"` // 退避截止时间,在此之前跳过抓取
}

// Runner 把 Source 注册到 cron 调度器,并在每次触发时执行抓取 + 入库。
type Runner struct {
	logger  *slog.Logger
	repo    *Repository
	cron    *cron.Cron
	sources []Source // 保留一份,便于 RunAllNow 手动触发

	mu     sync.RWMutex
	health map[string]*SourceHealth

	retentionDays int // 文章保留天数,<=0 表示不清理

	// 每日名言 job(可选,nil 时不启用)
	announcementsRepo *AnnouncementsRepository
	quotesSchedule    string // cron 表达式,空字符串则不注册

	// 每日精选邮件 job(可选,digest=nil 时不启用)
	digest         *digest.Digest
	digestSchedule string
}

func NewRunner(
	logger *slog.Logger,
	repo *Repository,
	announcementsRepo *AnnouncementsRepository,
	retentionDays int,
	quotesSchedule string,
	digestJob *digest.Digest,
	digestSchedule string,
) *Runner {
	return &Runner{
		logger:            logger,
		repo:              repo,
		cron:              cron.New(cron.WithSeconds()),
		health:            make(map[string]*SourceHealth),
		retentionDays:     retentionDays,
		announcementsRepo: announcementsRepo,
		quotesSchedule:    quotesSchedule,
		digest:            digestJob,
		digestSchedule:    digestSchedule,
	}
}

// Register 注册一个 Source 到调度器。
func (r *Runner) Register(s Source) error {
	_, err := r.cron.AddFunc(s.Schedule(), func() {
		r.runOnce(s)
	})
	if err != nil {
		return err
	}
	r.sources = append(r.sources, s)
	r.mu.Lock()
	r.health[s.Key()] = &SourceHealth{}
	r.mu.Unlock()
	return nil
}

// RunAllNow 立即同步执行一次所有已注册的源,不等 cron。用于启动自检或手动触发。
func (r *Runner) RunAllNow() {
	for _, s := range r.sources {
		r.runOnce(s)
	}
}

// Health 返回所有 Source 的健康状态快照（线程安全）。
func (r *Runner) Health() map[string]*SourceHealth {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make(map[string]*SourceHealth, len(r.health))
	for k, v := range r.health {
		cp := *v
		out[k] = &cp
	}
	return out
}

// Start 启动调度器（非阻塞）。
func (r *Runner) Start() {
	// 注册数据清理任务:每天凌晨 3 点
	if r.retentionDays > 0 {
		_, err := r.cron.AddFunc("0 0 3 * * *", r.purge)
		if err != nil {
			r.logger.Error("register purge job", "err", err)
		} else {
			r.logger.Info("purge job registered", "retention_days", r.retentionDays, "schedule", "03:00 daily")
		}
	}

	// 注册每日名言 job(配置完整才启用)
	if r.announcementsRepo != nil && r.quotesSchedule != "" {
		_, err := r.cron.AddFunc(r.quotesSchedule, r.runQuotesJob)
		if err != nil {
			r.logger.Error("register quotes job", "err", err)
		} else {
			r.logger.Info("quotes job registered", "schedule", r.quotesSchedule)
		}
	}

	// 注册每日精选邮件 job(配置完整才启用)
	if r.digest != nil && r.digestSchedule != "" {
		_, err := r.cron.AddFunc(r.digestSchedule, r.runDigestJob)
		if err != nil {
			r.logger.Error("register digest job", "err", err)
		} else {
			r.logger.Info("digest job registered", "schedule", r.digestSchedule)
		}
	}

	r.cron.Start()
}

// PurgeNow 立即执行一次过期数据清理。
func (r *Runner) PurgeNow() {
	r.purge()
}

// Stop 优雅关停,等待正在执行的任务完成。
func (r *Runner) Stop(ctx context.Context) {
	stopCtx := r.cron.Stop()
	select {
	case <-stopCtx.Done():
	case <-ctx.Done():
	}
}

func (r *Runner) runOnce(s Source) {
	log := r.logger.With("source", s.Key())

	// ---- 退避检查:如果还在退避期内,直接跳过 ----
	if until := r.getBackoffUntil(s.Key()); !until.IsZero() && time.Now().Before(until) {
		log.Warn("skipping fetch due to backoff",
			"backoff_until", until.Format(time.RFC3339),
			"remaining", time.Until(until).Round(time.Second))
		return
	}

	// ---- 随机延迟 (jitter):打破精确定时的机器人特征 ----
	jitter := time.Duration(rand.Int63n(60)+1) * time.Second // 1~60 秒
	log.Info("fetch scheduled", "jitter", jitter.Round(time.Second))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	select {
	case <-time.After(jitter):
		// jitter 等待完成,继续执行
	case <-ctx.Done():
		log.Warn("jitter wait cancelled")
		return
	}

	start := time.Now()
	log.Info("fetch started")

	articles, err := s.Fetch(ctx)
	if err != nil {
		r.recordFailure(s.Key(), err)
		r.applyBackoff(s.Key(), err)
		log.Error("fetch failed", "err", err)
		return
	}

	var inserted, updated int
	for _, a := range articles {
		a.SourceKey = s.Key()
		isNew, err := r.repo.UpsertArticle(ctx, a)
		if err != nil {
			log.Error("upsert failed", "url", a.URL, "err", err)
			continue
		}
		if isNew {
			inserted++
		} else {
			updated++
		}
	}

	r.recordSuccess(s.Key())

	log.Info("fetch done",
		"total", len(articles),
		"inserted", inserted,
		"updated", updated,
		"elapsed", time.Since(start))
}

func (r *Runner) purge() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	deleted, err := r.repo.PurgeOldArticles(ctx, r.retentionDays)
	if err != nil {
		r.logger.Error("purge failed", "err", err)
		return
	}
	r.logger.Info("purge done", "retention_days", r.retentionDays, "deleted", deleted)
}

// runQuotesJob 一次"换今日名言"流程:
//  1. 软删此前 active 的 quote 公告(只动 level='quote',运维公告不受影响)
//  2. 从内置库随机挑 2 条插入,ends_at 设为下一轮 cron 后 1 小时(漂移容错)
//
// 任一步失败都只记 error,不阻塞下一步,保证最大努力可用性。
func (r *Runner) runQuotesJob() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log := r.logger.With("job", "quotes")
	log.Info("quotes refresh started")

	if n, err := r.announcementsRepo.DeactivateActiveQuotes(ctx); err != nil {
		log.Error("deactivate previous quotes failed", "err", err)
	} else {
		log.Info("deactivated previous quotes", "count", n)
	}

	picks := quotes.PickN(2)
	// 25h 给每天一次的 cron 留 1h 漂移容错;若 cron 漏跑,旧名言也会自然过期消失
	endsAt := time.Now().Add(25 * time.Hour)
	for _, q := range picks {
		text := q.Text
		if q.Source != "" {
			text = fmt.Sprintf("%s —— %s", q.Text, q.Source)
		}
		if id, err := r.announcementsRepo.InsertQuote(ctx, text, &endsAt); err != nil {
			log.Error("insert quote failed", "err", err)
		} else {
			log.Info("quote inserted", "id", id)
		}
	}
}

// runDigestJob 触发一次每日精选邮件发送。失败仅记日志,不影响下次 cron。
func (r *Runner) runDigestJob() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	r.digest.Run(ctx)
}

// getBackoffUntil 读取某 Source 当前的退避截止时间。
func (r *Runner) getBackoffUntil(key string) time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if h := r.health[key]; h != nil {
		return h.BackoffUntil
	}
	return time.Time{}
}

// applyBackoff 根据错误类型设置退避时长。
// 不同错误类型对应不同的退避策略,严重的封禁信号退避更久。
func (r *Runner) applyBackoff(key string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	h := r.health[key]
	if h == nil {
		h = &SourceHealth{}
		r.health[key] = h
	}

	var backoff time.Duration

	switch {
	case errors.Is(err, ErrBanned):
		// 403 封禁 → 退避 6~12 小时
		backoff = time.Duration(6*3600+rand.Int63n(6*3600)) * time.Second
		r.logger.Error(fmt.Sprintf("ALERT: source %s is BANNED (403), backing off %.1f hours. Check cookie or IP!",
			key, backoff.Hours()))

	case errors.Is(err, ErrCookieExpired):
		// Cookie 过期 → 退避 6~12 小时,等人工更换
		backoff = time.Duration(6*3600+rand.Int63n(6*3600)) * time.Second
		r.logger.Error(fmt.Sprintf("ALERT: source %s cookie EXPIRED, backing off %.1f hours. Replace ZHIHU_COOKIE!",
			key, backoff.Hours()))

	case errors.Is(err, ErrRateLimited):
		// 429 限流 → 退避 1~2 小时
		backoff = time.Duration(3600+rand.Int63n(3600)) * time.Second
		r.logger.Warn(fmt.Sprintf("source %s rate limited (429), backing off %.1f hours",
			key, backoff.Hours()))

	case errors.Is(err, ErrEmptyData):
		// 空数据 → 退避 30~60 分钟,可能是临时问题
		backoff = time.Duration(1800+rand.Int63n(1800)) * time.Second
		r.logger.Warn(fmt.Sprintf("source %s returned empty data, backing off %.0f minutes",
			key, backoff.Minutes()))

	default:
		// 普通错误(网络超时等) → 不额外退避,只跳过 1 个周期(靠连续失败计数器自然处理)
		// 但如果已经连续失败多次,开始渐进退避
		if h.ConsecutiveFailures >= 3 {
			// 连续失败 3+ 次:退避 30 分钟 × 失败次数(上限 6 小时)
			multiplier := int64(h.ConsecutiveFailures)
			if multiplier > 12 {
				multiplier = 12
			}
			backoff = time.Duration(multiplier*1800) * time.Second
			r.logger.Warn(fmt.Sprintf("source %s failed %d times consecutively, backing off %.0f minutes",
				key, h.ConsecutiveFailures, backoff.Minutes()))
		}
		return // 前几次普通失败不设退避
	}

	h.BackoffUntil = time.Now().Add(backoff)
}

func (r *Runner) recordFailure(key string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	h := r.health[key]
	if h == nil {
		h = &SourceHealth{}
		r.health[key] = h
	}
	h.ConsecutiveFailures++
	h.LastError = err.Error()

	if h.ConsecutiveFailures >= 3 {
		r.logger.Error(fmt.Sprintf("ALERT: source %s failed %d times consecutively",
			key, h.ConsecutiveFailures), "last_error", h.LastError)
	}
}

func (r *Runner) recordSuccess(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	h := r.health[key]
	if h == nil {
		h = &SourceHealth{}
		r.health[key] = h
	}
	h.ConsecutiveFailures = 0
	h.LastError = ""
	h.LastSuccess = time.Now()
	h.BackoffUntil = time.Time{} // 成功后清除退避
}
