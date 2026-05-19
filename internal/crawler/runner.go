package crawler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/wwf5067/newsfeed/internal/crawler/digest"
	"github.com/wwf5067/newsfeed/internal/subscribe"
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

	// 每日数据摘要 job(可选,announcementsRepo=nil 时不启用)。
	// 历史:此 job 一开始是"每日名言",后来改成"今日数据摘要",故 schedule 来源
	// 变量名也从 QUOTES_SCHEDULE 改成 SUMMARY_SCHEDULE。但 announcement.level 仍用
	// 'quote' 这个枚举值——只是因为 DB 已经有这个 CHECK,改起来牵涉 migration 不值。
	announcementsRepo *AnnouncementsRepository
	summarySchedule   string // cron 表达式,空字符串则不注册

	// 每日精选邮件 job(可选,digest=nil 时不启用)
	digest         *digest.Digest
	digestSchedule string

	// 关键词订阅匹配器(可选,nil 时不启用)。
	// 抓完一个源就调一次,把"本轮新增 article id"传给它。
	subscribe *subscribe.Matcher
}

func NewRunner(
	logger *slog.Logger,
	repo *Repository,
	announcementsRepo *AnnouncementsRepository,
	retentionDays int,
	summarySchedule string,
	digestJob *digest.Digest,
	digestSchedule string,
	subscribeMatcher *subscribe.Matcher,
) *Runner {
	return &Runner{
		logger:            logger,
		repo:              repo,
		cron:              cron.New(cron.WithSeconds()),
		health:            make(map[string]*SourceHealth),
		retentionDays:     retentionDays,
		announcementsRepo: announcementsRepo,
		summarySchedule:   summarySchedule,
		digest:            digestJob,
		digestSchedule:    digestSchedule,
		subscribe:         subscribeMatcher,
	}
}

// Register 注册一个 Source 到调度器。
// 使用最细粒度(15 分钟)作为基础 cron,通过 shouldRunAtHour 动态决定是否执行。
func (r *Runner) Register(s Source) error {
	// 用 15 分钟固定间隔替代 source 自带的 schedule。
	// 实际执行与否由 shouldRunThisTick 在 runOnce 里判定。
	_, err := r.cron.AddFunc("0 */15 * * * *", func() {
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

	// 注册今日数据摘要 job(配置完整才启用)
	if r.announcementsRepo != nil && r.summarySchedule != "" {
		_, err := r.cron.AddFunc(r.summarySchedule, r.runDailySummaryJob)
		if err != nil {
			r.logger.Error("register daily summary job", "err", err)
		} else {
			r.logger.Info("daily summary job registered", "schedule", r.summarySchedule)
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

	// ---- 分时段频率控制 ----
	// 基础 tick 是 15 分钟。按当前小时决定是否跳过本次 tick:
	//   9:00-12:00  每 15 分钟执行(每次都跑)
	//   12:00-18:00 每 20 分钟执行(约 3/4 的 tick 跑)→ 简化为隔一次跳过:00 和 :30 执行,:15 和 :45 跳过
	//   18:00-24:00 每 30 分钟执行(隔一次)
	//   0:00-9:00   每 60 分钟执行(4 次 tick 跑 1 次)
	if !r.shouldRunThisTick(s.Key()) {
		return
	}

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
	var newIDs []int64
	for _, a := range articles {
		a.SourceKey = s.Key()
		id, isNew, err := r.repo.UpsertArticle(ctx, a)
		if err != nil {
			log.Error("upsert failed", "url", a.URL, "err", err)
			continue
		}
		if isNew {
			inserted++
			newIDs = append(newIDs, id)
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

	// 每次抓取成功后刷新首页公告,让"最热"实时反映最新数据。
	// 保留独立 cron 作为兜底(抓取全失败时仍有定时公告)。
	if r.announcementsRepo != nil {
		r.runDailySummaryJob()
	}

	// 抓完后跑订阅匹配:有新文章 + matcher 已注册才执行。
	// 用独立 ctx 防止 fetch 的 timeout 限制邮件发送(SMTP 有时要 10+ 秒)。
	if r.subscribe != nil && len(newIDs) > 0 {
		matchCtx, matchCancel := context.WithTimeout(context.Background(), 60*time.Second)
		r.subscribe.HandleNewArticles(matchCtx, newIDs)
		matchCancel()
	}
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

// runDailySummaryJob 一次"今日数据摘要"流程:
//  1. 软删此前所有 level='quote' 的公告(让页面始终只显示最新的一条摘要)
//  2. 查 articles 表当日各源统计,构造一条公告插入
//
// 任一步失败都只记 error,不阻塞下一步,保证最大努力可用性。
// 摘要文案样例:
//
//	📊 今日 60 条 · 知乎最热「普京访华」571 万 · B 站最高「..」320 万播放
//
// announcements 用 level='quote' 沿用历史 schema,不引入新枚举值。
func (r *Runner) runDailySummaryJob() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log := r.logger.With("job", "summary")
	log.Info("daily summary refresh started")

	stats, err := r.repo.TodayStatsBySource(ctx)
	if err != nil {
		log.Error("query today stats failed", "err", err)
		return
	}
	if len(stats) == 0 {
		log.Info("no articles today, skipping summary")
		return
	}

	content := buildSummaryContent(stats)
	if content == "" {
		log.Info("summary content empty, skipping")
		return
	}

	if n, err := r.announcementsRepo.DeactivateActiveQuotes(ctx); err != nil {
		log.Error("deactivate previous summary failed", "err", err)
	} else {
		log.Info("deactivated previous summary", "count", n)
	}

	// 设个 25h 兜底过期时间,万一 cron 完全停摆也不会一直挂着昨天的数据
	endsAt := time.Now().Add(25 * time.Hour)
	if id, err := r.announcementsRepo.InsertQuote(ctx, content, &endsAt); err != nil {
		log.Error("insert summary failed", "err", err)
	} else {
		log.Info("summary inserted", "id", id, "content", content)
	}
}

// sourceLabels 把 source_key 转成展示名;未知源回落原 key。
var sourceLabels = map[string]string{
	"zhihu_hot":        "知乎",
	"bilibili_popular": "B 站",
}

// sourceMetricNoun 不同源的"最热"指标名(避免拿"播放量"和"热度"做心理换算)。
var sourceMetricNoun = map[string]string{
	"zhihu_hot":        "最热",
	"bilibili_popular": "最高",
}

// buildSummaryContent 拼装摘要文本。stats 为空返回空串,调用方负责跳过。
func buildSummaryContent(stats []SourceStat) string {
	if len(stats) == 0 {
		return ""
	}
	total := 0
	for _, s := range stats {
		total += s.Count
	}

	// 头部:总数 + 各源细分(按 stats 顺序,即 count 降序)
	parts := []string{fmt.Sprintf("📊 今日 %d 条", total)}
	if len(stats) > 1 {
		breakdown := make([]string, 0, len(stats))
		for _, s := range stats {
			label := sourceLabels[s.SourceKey]
			if label == "" {
				label = s.SourceKey
			}
			breakdown = append(breakdown, fmt.Sprintf("%s %d", label, s.Count))
		}
		parts[0] += " (" + strings.Join(breakdown, " / ") + ")"
	}

	// 每个源的 Top1
	for _, s := range stats {
		if s.TopTitle == "" {
			continue
		}
		label := sourceLabels[s.SourceKey]
		if label == "" {
			label = s.SourceKey
		}
		metric := sourceMetricNoun[s.SourceKey]
		if metric == "" {
			metric = "最热"
		}
		title := truncateRunes(s.TopTitle, 22)
		seg := fmt.Sprintf("%s%s「%s」", label, metric, title)
		if s.TopHeat != "" {
			seg += " " + s.TopHeat
		}
		parts = append(parts, seg)
	}

	return strings.Join(parts, " · ")
}

// truncateRunes 按 rune 数截断,超出补省略号。中英文都按 1 算长度。
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// runDigestJob 触发一次每日精选邮件发送。失败仅记日志,不影响下次 cron。
func (r *Runner) runDigestJob() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	r.digest.Run(ctx)
}

// shouldRunThisTick 分时段频率控制。
// 基础 tick 是每 15 分钟(:00, :15, :30, :45),按当前小时+分钟决定本次是否真正执行。
//
// 策略(上海时区):
//
//	09:00-12:00 → 每 15 分钟(所有 tick 都执行)
//	12:00-18:00 → 每 30 分钟(只在 :00 和 :30 执行)
//	18:00-24:00 → 每 30 分钟(只在 :00 和 :30 执行)
//	00:00-09:00 → 每 60 分钟(只在 :00 执行)
//
// 每日预估请求量: 知乎 12+12+12+9 = 45 次(vs 原来 48 次,总量差不多但分布更合理)
func (r *Runner) shouldRunThisTick(sourceKey string) bool {
	now := time.Now()
	hour := now.Hour()
	minute := now.Minute()

	switch {
	case hour >= 9 && hour < 12:
		// 黄金时段:每 15 分钟,全部执行
		return true
	case hour >= 12 && hour < 24:
		// 白天+晚间:每 30 分钟,只在 :00 和 :30 执行
		return minute < 15 || (minute >= 30 && minute < 45)
	default:
		// 凌晨 0:00-9:00:每 60 分钟,只在 :00 执行
		return minute < 15
	}
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
