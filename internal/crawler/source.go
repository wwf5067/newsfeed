package crawler

import (
	"context"
	"errors"

	"github.com/wwf5067/newsfeed/internal/model"
)

// Source 一个数据源就是一个 Source 实现。新增源 = 新增一个文件实现该接口并注册。
type Source interface {
	// Key 唯一标识,落库时写入 articles.source_key
	Key() string
	// Schedule cron 表达式,如 "*/30 * * * *" 表示每 30 分钟
	Schedule() string
	// Fetch 执行一次抓取,返回若干 Article。具体爬虫逻辑后续实现。
	Fetch(ctx context.Context) ([]model.Article, error)
}

// Sentinel errors —— 各 Source 实现在发现封禁/限流信号时返回这些错误,
// Runner 据此决定退避策略。用 errors.Is 判断。
var (
	// ErrBanned 服务端返回 403 或明确拒绝访问
	ErrBanned = errors.New("source: banned (403)")
	// ErrRateLimited 服务端返回 429 Too Many Requests
	ErrRateLimited = errors.New("source: rate limited (429)")
	// ErrCookieExpired 请求被重定向到登录页,Cookie 已过期
	ErrCookieExpired = errors.New("source: cookie expired (redirect to login)")
	// ErrEmptyData 接口返回 200 但数据为空,可能被静默封禁
	ErrEmptyData = errors.New("source: empty data (possible silent ban)")
)
