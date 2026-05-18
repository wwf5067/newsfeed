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
	Heat          string    `json:"heat"`            // 源原文热度文本,如 "1234 万热度"
	HeatValue     int64     `json:"heat_value"`      // 解析后的数值,用于趋势比较(0 = 未知/无单位)
	PrevHeat      string    `json:"prev_heat"`       // 上一次抓取时的 heat 文本(首次插入为空)
	PrevHeatValue int64     `json:"prev_heat_value"` // 上一次抓取时的 heat_value(首次插入为 0)
	PublishedAt   time.Time `json:"published_at"`
	FetchedAt     time.Time `json:"fetched_at"`
}
