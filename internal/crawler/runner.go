package crawler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// SourceHealth 记录某个 Source 的运行健康状态。
type SourceHealth struct {
	ConsecutiveFailures int       `json:"consecutive_failures"`
	LastError           string    `json:"last_error,omitempty"`
	LastSuccess         time.Time `json:"last_success,omitempty"`
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
}

func NewRunner(logger *slog.Logger, repo *Repository, retentionDays int) *Runner {
	return &Runner{
		logger:        logger,
		repo:          repo,
		cron:          cron.New(cron.WithSeconds()),
		health:        make(map[string]*SourceHealth),
		retentionDays: retentionDays,
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

// Health 返回所有 Source 的健康状态快照(线程安全)。
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

// Start 启动调度器(非阻塞)。
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
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	log := r.logger.With("source", s.Key())
	log.Info("fetch started")

	articles, err := s.Fetch(ctx)
	if err != nil {
		r.recordFailure(s.Key(), err)
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
		r.logger.Error(fmt.Sprintf("ALERT: source %s failed %d times consecutively, cookie may be expired",
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
}
