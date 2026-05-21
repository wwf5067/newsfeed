package model

import "time"

// Entity 抽取出的命名实体。同 (Name, Type) 在库内唯一。
type Entity struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`  // person|org|location|work|other
	Alias     string    `json:"alias"` // 逗号分隔别名,后续可拆 JSONB
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EntityResponse 实体的 API 响应格式(简化版,包含文章数和关联事件数)
type EntityResponse struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Alias        string `json:"alias,omitempty"`
	ArticleCount int    `json:"article_count"`
	EventCount   int    `json:"event_count"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}
