package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 集中管理所有运行时配置。两个服务共用一份结构,通过环境变量按需读取。
type Config struct {
	// 通用
	Env      string // dev | prod
	LogLevel string // debug | info | warn | error

	// 数据库
	DatabaseURL string

	// API 服务
	APIAddr         string // 监听地址,如 :8080
	APIReadTimeout  time.Duration
	APIWriteTimeout time.Duration

	// Crawler 服务
	CrawlerAddr string // 仅用于内部 health/metrics,绑定 127.0.0.1

	// 数据源:知乎热榜
	// 直接复制浏览器登录后的整段 Cookie 头(如 "_zap=...; z_c0=...; ...")
	ZhihuCookie   string
	ZhihuSchedule string // cron 表达式(支持秒),默认 30 分钟一次

	// 数据源:B 站热门(无需 cookie,显式开关控制是否启用)
	BilibiliEnabled  bool
	BilibiliSchedule string // cron 表达式(支持秒),默认 30 分钟一次

	// 每日名言 job:从内置库随机选 2 条以 announcements 形式发布
	QuotesSchedule string // cron 表达式(支持秒),默认每天 8:00

	// 调试开关:启动时立即执行一次所有源(不等 cron 触发)
	RunOnStart bool

	// 数据保留天数,超过此天数的文章将被自动清理(基于 fetched_at)
	RetentionDays int
}

// Load 从环境变量读取配置。缺失必填项会返回错误。
func Load() (*Config, error) {
	cfg := &Config{
		Env:              getEnv("APP_ENV", "dev"),
		LogLevel:         getEnv("LOG_LEVEL", "info"),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		APIAddr:          getEnv("API_ADDR", ":8080"),
		APIReadTimeout:   getEnvDuration("API_READ_TIMEOUT", 10*time.Second),
		APIWriteTimeout:  getEnvDuration("API_WRITE_TIMEOUT", 15*time.Second),
		CrawlerAddr:      getEnv("CRAWLER_ADDR", "127.0.0.1:8081"),
		ZhihuCookie:      os.Getenv("ZHIHU_COOKIE"),
		ZhihuSchedule:    getEnv("ZHIHU_SCHEDULE", "0 */30 * * * *"),
		BilibiliEnabled:  strings.EqualFold(os.Getenv("BILIBILI_ENABLED"), "true"),
		BilibiliSchedule: getEnv("BILIBILI_SCHEDULE", "0 */30 * * * *"),
		QuotesSchedule:   getEnv("QUOTES_SCHEDULE", "0 0 8 * * *"),
		RunOnStart:       strings.EqualFold(os.Getenv("RUN_ON_START"), "true"),
		RetentionDays:    getEnvInt("RETENTION_DAYS", 30),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	// 兼容纯数字(秒)
	if n, err := strconv.Atoi(v); err == nil {
		return time.Duration(n) * time.Second
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	if n, err := strconv.Atoi(v); err == nil {
		return n
	}
	return fallback
}
