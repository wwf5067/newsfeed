// extractor 是独立的实体/事件抽取 worker 进程。
//
// 周期性从 articles 取一批未抽取的行,调用 Extractor(规则或 LLM),
// 把结果写入 entities/events/article_entities/article_events,
// 然后在 articles.extracted_at 上盖时间戳。
//
// 部署上与 crawler/api 解耦:崩了不影响抓取与查询,只是数据延迟入库。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wwf5067/newsfeed/internal/config"
	"github.com/wwf5067/newsfeed/internal/extractor"
	"github.com/wwf5067/newsfeed/internal/logger"
	"github.com/wwf5067/newsfeed/internal/storage"
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

	// 按配置选择抽取实现。新增 backend 在这里加一个 case 即可。
	var ex extractor.Extractor
	switch cfg.ExtractorBackend {
	case "rule":
		ex = extractor.NewRuleExtractor()
	case "llm":
		ex = extractor.NewLLMExtractor()
	default:
		log.Error("unknown EXTRACTOR_BACKEND", "value", cfg.ExtractorBackend)
		os.Exit(1)
	}

	repo := extractor.NewRepository(pool)
	runner := extractor.NewRunner(log, repo, ex, cfg.ExtractorSchedule, cfg.ExtractorBatchSize)

	if err := runner.Start(); err != nil {
		log.Error("runner start", "err", err)
		os.Exit(1)
	}

	// 启动时立即跑一次,不等 cron 第一次触发(便于本地调试与新部署快速产出数据)
	if cfg.RunOnStart {
		go func() {
			runCtx, runCancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer runCancel()
			runner.RunNow(runCtx)
		}()
	}

	// 内部 health 端点
	healthMux := http.NewServeMux()
	healthMux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"backend": ex.Backend(),
		})
	})
	healthSrv := &http.Server{
		Addr:              cfg.ExtractorAddr,
		Handler:           healthMux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := healthSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("health server", "err", err)
		}
	}()

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Info("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	runner.Stop(shutdownCtx)
	_ = healthSrv.Shutdown(shutdownCtx)

	log.Info("bye")
}
