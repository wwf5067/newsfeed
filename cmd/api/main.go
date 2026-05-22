package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wwf5067/newsfeed/internal/api"
	"github.com/wwf5067/newsfeed/internal/config"
	"github.com/wwf5067/newsfeed/internal/logger"
	"github.com/wwf5067/newsfeed/internal/storage"
	"github.com/wwf5067/newsfeed/internal/subscribe"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	log := logger.New(cfg.LogLevel, cfg.Env)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := storage.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("db connect", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	repo := api.NewRepository(pool)
	subRepo := subscribe.NewRepository(pool)
	handler := api.NewHandler(log, repo).WithSubscribe(subRepo, cfg.DigestTo)

	// 启动时加载已转正的热度候选词,注入 gse 分词器 + trackerEntityLabelSet。
	// 失败不阻塞启动(降级为不注入,转正词下次请求时会重新检测)。
	if promoted, err := repo.ListPromotedCandidates(ctx); err == nil && len(promoted) > 0 {
		api.InjectPromotedWords(promoted)
		log.Info("loaded promoted heat candidates", "count", len(promoted))
	}

	// 启动时加载热词黑名单。
	if blacklist, err := repo.ListHeatBlacklist(ctx); err == nil && len(blacklist) > 0 {
		api.LoadHeatBlacklist(blacklist)
		log.Info("loaded heat blacklist", "count", len(blacklist))
	}

	// 启动时加载尚未转正的热词候选(total_hits >= 2),恢复跨窗口累积状态。
	// minHits=2 与 collectHeatDiscoveredWords 的 minArticles 对齐:至少命中过 2 篇才值得保留。
	if pending, err := repo.ListPendingHeatCandidates(ctx, 2); err == nil && len(pending) > 0 {
		handler.LoadPendingHeatWords(pending)
		log.Info("loaded pending heat candidates", "count", len(pending))
	}

	srv := &http.Server{
		Addr:         cfg.APIAddr,
		Handler:      api.NewRouter(handler),
		ReadTimeout:  cfg.APIReadTimeout,
		WriteTimeout: cfg.APIWriteTimeout,
	}

	// 热词发现算法评估 job:每小时跑一次,跑当前阈值 + 邻近候选阈值
	// 比较 precision/score,把报告写入 heat_eval_reports 表。
	// 不自动改算法,只输出建议供人工评估。失败不影响主流程(best-effort)。
	go runHeatEvalLoop(ctx, log, repo)

	go func() {
		log.Info("api listening", "addr", cfg.APIAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("api serve", "err", err)
			cancel()
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sigCh:
	case <-ctx.Done():
	}
	log.Info("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)

	log.Info("bye")
}

// runHeatEvalLoop 每小时跑一次热词发现算法评估,把报告写入 DB。
//
// 设计要点:
//   - 启动后等 5 分钟再首次评估(避开冷启动 + 让 articles 加载稳定)
//   - 每小时一次(对齐整点)
//   - 失败不传播(评估是观察工具,挂了也不影响主路径)
//   - 跑评估用 24h 窗口的文章池(够稳定不噪声)
func runHeatEvalLoop(ctx context.Context, log *slog.Logger, repo *api.Repository) {
	// 等 5 分钟再首次评估
	select {
	case <-ctx.Done():
		return
	case <-time.After(5 * time.Minute):
	}

	doEval := func() {
		evalCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		// 拉 24h 文章
		articles, err := repo.ListRecentArticles(evalCtx, 24, 500)
		if err != nil {
			log.Warn("heat-eval list articles failed", "err", err)
			return
		}
		blacklist, err := repo.ListHeatBlacklist(evalCtx)
		if err != nil {
			log.Warn("heat-eval list blacklist failed", "err", err)
			return
		}

		// 把 DB 最新快照同步回内存缓存(每小时对齐一次)。
		// 覆盖写、幂等:保证直接写 DB 的黑名单变更无需重启即可生效。
		api.LoadHeatBlacklist(blacklist)

		report := api.EvaluateHeatDiscovery(articles, blacklist)
		report.WindowHours = 24
		id, err := repo.InsertHeatEvalReport(evalCtx, report)
		if err != nil {
			log.Warn("heat-eval insert report failed", "err", err)
			return
		}

		log.Info("heat-eval report",
			"id", id,
			"articles", report.ArticlesCount,
			"blacklist", report.BlacklistCount,
			"baseline_count", report.Baseline.DiscoveredCount,
			"baseline_precision", report.Baseline.Precision,
			"best_variant", report.BestVariant,
			"has_suggestion", report.Suggestion != "",
		)
		if report.Suggestion != "" {
			log.Info("heat-eval suggestion", "msg", report.Suggestion)
		}
	}

	doEval()

	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			doEval()
		}
	}
}
