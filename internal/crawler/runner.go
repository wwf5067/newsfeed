package crawler

import (
	"context"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"
)

// Runner 把 Source 注册到 cron 调度器,并在每次触发时执行抓取 + 入库。
type Runner struct {
	logger  *slog.Logger
	repo    *Repository
	cron    *cron.Cron
	sources []Source // 保留一份,便于 RunAllNow 手动触发
}

func NewRunner(logger *slog.Logger, repo *Repository) *Runner {
	return &Runner{
		logger: logger,
		repo:   repo,
		cron:   cron.New(cron.WithSeconds()),
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
	return nil
}

// RunAllNow 立即同步执行一次所有已注册的源,不等 cron。用于启动自检或手动触发。
func (r *Runner) RunAllNow() {
	for _, s := range r.sources {
		r.runOnce(s)
	}
}

// Start 启动调度器(非阻塞)。
func (r *Runner) Start() {
	r.cron.Start()
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
		log.Error("fetch failed", "err", err)
		return
	}

	var inserted, skipped int
	for _, a := range articles {
		a.SourceKey = s.Key()
		ok, err := r.repo.UpsertArticle(ctx, a)
		if err != nil {
			log.Error("upsert failed", "url", a.URL, "err", err)
			continue
		}
		if ok {
			inserted++
		} else {
			skipped++
		}
	}

	log.Info("fetch done",
		"total", len(articles),
		"inserted", inserted,
		"skipped", skipped,
		"elapsed", time.Since(start))
}
