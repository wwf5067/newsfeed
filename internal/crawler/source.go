package crawler

import (
	"context"

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
