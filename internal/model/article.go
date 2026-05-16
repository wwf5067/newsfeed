package model

import "time"

// Article 抓取入库的最小通用字段。后续按业务再补充。
// JSON tag 统一用 snake_case 方便前端消费。
type Article struct {
	ID          int64     `json:"id"`
	SourceKey   string    `json:"source_key"` // 源标识,如 "zhihu_hot"
	URL         string    `json:"url"`        // 唯一,用于去重
	Title       string    `json:"title"`
	Content     string    `json:"content"`
	Author      string    `json:"author"`
	PublishedAt time.Time `json:"published_at"`
	FetchedAt   time.Time `json:"fetched_at"`
}
