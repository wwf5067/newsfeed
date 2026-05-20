package main

import (
	"context"
	"errors"
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

	srv := &http.Server{
		Addr:         cfg.APIAddr,
		Handler:      api.NewRouter(handler),
		ReadTimeout:  cfg.APIReadTimeout,
		WriteTimeout: cfg.APIWriteTimeout,
	}

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
