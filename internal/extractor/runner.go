package extractor

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/robfig/cron/v3"
)

// Runner 把 Extractor 注册到 cron 调度器,周期性地从 articles 取一批
// 待处理行,逐条抽取并落库。
type Runner struct {
	log      *slog.Logger
	repo     *Repository
	ex       Extractor
	cron     *cron.Cron
	schedule string
	batch    int
}

func NewRunner(log *slog.Logger, repo *Repository, ex Extractor, schedule string, batch int) *Runner {
	return &Runner{
		log:      log,
		repo:     repo,
		ex:       ex,
		cron:     cron.New(cron.WithSeconds()),
		schedule: schedule,
		batch:    batch,
	}
}

// Start 注册调度任务并启动 cron(非阻塞)。
func (r *Runner) Start() error {
	_, err := r.cron.AddFunc(r.schedule, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		r.processBatch(ctx)
	})
	if err != nil {
		return err
	}
	r.cron.Start()
	r.log.Info("extractor started",
		"backend", r.ex.Backend(),
		"schedule", r.schedule,
		"batch_size", r.batch)
	return nil
}

// RunNow 立即同步执行一次批处理,不等 cron。供启动自检或手动触发使用。
func (r *Runner) RunNow(ctx context.Context) {
	r.processBatch(ctx)
}

// Stop 优雅关停,等待正在执行的任务完成。
func (r *Runner) Stop(ctx context.Context) {
	stopCtx := r.cron.Stop()
	select {
	case <-stopCtx.Done():
	case <-ctx.Done():
	}
}

// processBatch 拉一批待处理 → 逐条 processOne。
// 单条失败不阻塞后续,同时累加 extract_attempts。
func (r *Runner) processBatch(ctx context.Context) {
	start := time.Now()
	pending, err := r.repo.PickPending(ctx, r.batch)
	if err != nil {
		r.log.Error("pick pending", "err", err)
		return
	}
	if len(pending) == 0 {
		r.log.Debug("no pending articles")
		return
	}

	var ok, fail int
	for _, art := range pending {
		if err := r.processOne(ctx, art); err != nil {
			fail++
			r.log.Error("extract one", "article_id", art.ID, "err", err)
			// 累加重试次数,失败 3 次后被部分索引排除
			if ierr := r.repo.IncrAttempts(ctx, art.ID); ierr != nil {
				r.log.Error("incr attempts", "article_id", art.ID, "err", ierr)
			}
			continue
		}
		ok++
	}

	r.log.Info("extract batch done",
		"total", len(pending),
		"ok", ok,
		"fail", fail,
		"elapsed", time.Since(start))
}

// processOne 单条文章的事务化处理:抽取 → upsert 实体 → upsert 事件 →
// 关联 → 标记已抽取。任一步失败,事务回滚,文章保持 NULL 待重试。
func (r *Runner) processOne(ctx context.Context, art PendingArticle) error {
	// 抽取在事务外做(纯函数,无 DB),即便失败也不污染事务。
	res, err := r.ex.Extract(ctx, art.Title, art.Content)
	if err != nil {
		return err
	}

	return r.repo.WithTx(ctx, func(tx pgx.Tx) error {
		// 实体
		for _, e := range res.Entities {
			id, err := r.repo.UpsertEntity(ctx, tx, e.Name, e.Type)
			if err != nil {
				return err
			}
			if err := r.repo.LinkArticleEntity(ctx, tx, art.ID, id); err != nil {
				return err
			}
		}

		// 事件
		for _, ev := range res.Events {
			id, err := r.repo.UpsertEvent(ctx, tx, ev.Fingerprint, ev.Title, art.PublishedAt)
			if err != nil {
				return err
			}
			created, err := r.repo.LinkArticleEvent(ctx, tx, art.ID, id)
			if err != nil {
				return err
			}
			if created {
				if err := r.repo.TouchEvent(ctx, tx, id, art.PublishedAt); err != nil {
					return err
				}
			}
		}

		// 标记已抽取(同事务内,确保抽取结果与状态原子提交)
		return r.repo.MarkExtracted(ctx, tx, art.ID)
	})
}
