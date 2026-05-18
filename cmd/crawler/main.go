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

	"github.com/wwf5067/newsfeed/internal/api"
	"github.com/wwf5067/newsfeed/internal/config"
	"github.com/wwf5067/newsfeed/internal/crawler"
	"github.com/wwf5067/newsfeed/internal/crawler/digest"
	"github.com/wwf5067/newsfeed/internal/crawler/sources"
	"github.com/wwf5067/newsfeed/internal/logger"
	"github.com/wwf5067/newsfeed/internal/mailer"
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

	repo := crawler.NewRepository(pool)
	announcementsRepo := crawler.NewAnnouncementsRepository(pool)

	// 每日精选邮件:跨包用 api.Repository 读 articles。
	// SMTP 配置不全时 digestJob = nil,runner 会自动跳过注册。
	apiRepo := api.NewRepository(pool)
	var digestJob *digest.Digest
	if cfg.SMTPHost != "" && cfg.DigestTo != "" {
		digestJob = digest.New(log, apiRepo, digest.SMTPConfig{
			Host:    cfg.SMTPHost,
			Port:    cfg.SMTPPort,
			User:    cfg.SMTPUser,
			Pass:    cfg.SMTPPass,
			From:    cfg.SMTPFrom,
			To:      cfg.DigestTo,
			SiteURL: cfg.SiteURL,
		})
		log.Info("digest job configured", "to", cfg.DigestTo, "schedule", cfg.DigestSchedule)
	} else {
		log.Warn("digest job skipped: SMTP_HOST or DIGEST_TO empty")
	}

	// 关键词订阅匹配器:抓完一个源后调,命中关键词聚合发邮件。
	// SMTP 没配齐 matcher 也注册,只是会"登记去重不发邮件"——避免后续配上 SMTP
	// 后突然把过去命中过的旧文章批量补发(matcher.go 里的处理)。
	subRepo := subscribe.NewRepository(pool)
	subscribeMatcher := subscribe.New(log, subRepo, mailer.Config{
		Host: cfg.SMTPHost, Port: cfg.SMTPPort,
		User: cfg.SMTPUser, Pass: cfg.SMTPPass,
		From: cfg.SMTPFrom, To: cfg.DigestTo,
	}, cfg.SiteURL)
	log.Info("subscribe matcher configured", "smtp_ready", cfg.SMTPHost != "" && cfg.DigestTo != "")

	runner := crawler.NewRunner(
		log, repo, announcementsRepo,
		cfg.RetentionDays,
		cfg.SummarySchedule,
		digestJob, cfg.DigestSchedule,
		subscribeMatcher,
	)

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

	if cfg.BilibiliEnabled {
		s := sources.NewBilibili(cfg.BilibiliSchedule)
		if err := runner.Register(s); err != nil {
			log.Error("register source", "key", s.Key(), "err", err)
			os.Exit(1)
		}
		log.Info("source registered", "key", s.Key(), "schedule", s.Schedule())
		registered++
	} else {
		log.Warn("bilibili_popular skipped: BILIBILI_ENABLED is not true")
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
