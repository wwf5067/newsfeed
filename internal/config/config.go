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

	// 数据源:百度热搜实时榜(无需 cookie,显式开关控制是否启用)
	BaiduEnabled  bool
	BaiduSchedule string // cron 表达式(支持秒),默认 30 分钟一次

	// 数据源:微博热搜(无需 cookie,通过 visitor 系统自动获取访客身份)
	WeiboEnabled  bool
	WeiboSchedule string // cron 表达式(支持秒),默认 30 分钟一次

	// 数据源:搜狗热搜(无需 cookie,显式开关控制是否启用)
	SogouEnabled  bool
	SogouSchedule string // cron 表达式(支持秒),默认 30 分钟一次

	// 今日数据摘要 job:每 30 分钟更新一条 announcements,内容为今日抓取统计。
	// 历史:此前是"每日名言",故环境变量旧名 QUOTES_SCHEDULE 仍然兼容(降级回退)。
	SummarySchedule string // cron 表达式(支持秒),默认 30 分钟一次

	// 每日精选邮件 job:把 top10 热门发到指定邮箱。
	// SMTPHost 空时该 job 不注册,crawler 仍正常启动。
	DigestSchedule string // cron 表达式(支持秒),默认每天 8:00
	SMTPHost       string // 如 smtp.qq.com
	SMTPPort       int    // 默认 465(SMTPS)
	SMTPUser       string
	SMTPPass       string // QQ/163 邮箱注意用"授权码"而不是登录密码
	SMTPFrom       string // 发件人地址,通常等于 SMTPUser
	DigestTo       string // 收件人(单值)
	SiteURL        string // 邮件正文里拼绝对链接用,如 https://newsfeed.example.com

	// 调试开关:启动时立即执行一次所有源(不等 cron 触发)
	RunOnStart bool

	// 数据保留天数,超过此天数的文章将被自动清理(基于 fetched_at)
	RetentionDays int

	// === Extractor 服务 ===
	// 抽取实现:rule(规则) | llm(大模型,尚未实现)
	ExtractorBackend string
	// cron 表达式(支持秒位),默认 5 分钟一次
	ExtractorSchedule string
	// 每次批处理的文章上限
	ExtractorBatchSize int
	// 内部 health 端点(仅本机)
	ExtractorAddr string
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
		BaiduEnabled:     strings.EqualFold(os.Getenv("BAIDU_ENABLED"), "true"),
		BaiduSchedule:    getEnv("BAIDU_SCHEDULE", "0 */30 * * * *"),
		WeiboEnabled:     strings.EqualFold(os.Getenv("WEIBO_ENABLED"), "true"),
		WeiboSchedule:    getEnv("WEIBO_SCHEDULE", "0 */30 * * * *"),
		SogouEnabled:     strings.EqualFold(os.Getenv("SOGOU_ENABLED"), "true"),
		SogouSchedule:    getEnv("SOGOU_SCHEDULE", "0 */30 * * * *"),
		// SUMMARY_SCHEDULE 为新名字,QUOTES_SCHEDULE 是旧名字保留兼容,
		// 都没设时默认每 30 分钟一次,与抓取节奏一致。
		SummarySchedule: getEnv("SUMMARY_SCHEDULE", getEnv("QUOTES_SCHEDULE", "0 */30 * * * *")),
		DigestSchedule:  getEnv("DIGEST_SCHEDULE", "0 0 8 * * *"),
		SMTPHost:        os.Getenv("SMTP_HOST"),
		SMTPPort:        getEnvInt("SMTP_PORT", 465),
		SMTPUser:        os.Getenv("SMTP_USER"),
		SMTPPass:        os.Getenv("SMTP_PASS"),
		SMTPFrom:        getEnv("SMTP_FROM", os.Getenv("SMTP_USER")),
		DigestTo:        os.Getenv("DIGEST_TO"),
		SiteURL:         getEnv("SITE_URL", "http://localhost:3000"),
		RunOnStart:      strings.EqualFold(os.Getenv("RUN_ON_START"), "true"),
		RetentionDays:   getEnvInt("RETENTION_DAYS", 90),

		ExtractorBackend:   getEnv("EXTRACTOR_BACKEND", "rule"),
		ExtractorSchedule:  getEnv("EXTRACTOR_SCHEDULE", "0 */5 * * * *"),
		ExtractorBatchSize: getEnvInt("EXTRACTOR_BATCH_SIZE", 50),
		ExtractorAddr:      getEnv("EXTRACTOR_ADDR", "127.0.0.1:8082"),
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
