package main

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wwf5067/newsfeed/internal/config"
	"github.com/wwf5067/newsfeed/internal/crawler"
	"github.com/wwf5067/newsfeed/internal/crawler/sources"
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

	repo := crawler.NewRepository(pool)
	announcementsRepo := crawler.NewAnnouncementsRepository(pool)
	runner := crawler.NewRunner(log, repo, announcementsRepo, cfg.RetentionDays, cfg.QuotesSchedule)

	// 显式注册数据源。新增源 = 在这里加一行。
	// 没配置必需凭据的源会被跳过,不影响其它源运行。
	registered := 0
	if cfg.ZhihuCookie != "" {
		s := sources.NewZhihuHot(cfg.ZhihuCookie, cfg.ZhihuSchedule)
		if err := runner.Register(s); err != nil {
			log.Error("register source", "key", s.Key(), "err", err)
			os.Exit(1)
		}
		log.Info("source registered", "key", s.Key(), "schedule", s.Schedule())
		registered++
	} else {
		log.Warn("zhihu_hot skipped: ZHIHU_COOKIE is empty")
	}

	if registered == 0 {
		log.Warn("no sources registered, crawler will idle")
	}

	runner.Start()
	log.Info("crawler started", "retention_days", cfg.RetentionDays)

	if cfg.RunOnStart && registered > 0 {
		// 启动保护:随机延迟 5~30 秒后再执行首次抓取,
		// 避免容器重启风暴导致瞬时并发请求
		startDelay := time.Duration(5+rand.Intn(25)) * time.Second
		log.Info("RUN_ON_START=true, will execute all sources after delay", "delay", startDelay)
		go func() {
			time.Sleep(startDelay)
			runner.RunAllNow()
		}()
	}

	// 启动时执行一次过期清理
	if cfg.RetentionDays > 0 {
		go runner.PurgeNow()
	}

	// 内部 health 端点,仅监听 127.0.0.1
	healthMux := http.NewServeMux()
	healthMux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		sourceHealth := runner.Health()

		status := "ok"
		for _, h := range sourceHealth {
			if h.ConsecutiveFailures >= 3 {
				status = "degraded"
				break
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if status == "degraded" {
			w.WriteHeader(http.StatusServiceUnavailable) // 503
		} else {
			w.WriteHeader(http.StatusOK)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  status,
			"sources": sourceHealth,
		})
	})

	healthSrv := &http.Server{
		Addr:              cfg.CrawlerAddr,
		Handler:           healthMux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := healthSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("health server", "err", err)
		}
	}()

	// 等待信号
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
