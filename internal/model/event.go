package model

import "time"

// Event 一组文章聚合的话题。Fingerprint 是归一化标题的哈希,用于跨文章归并。
type Event struct {
	ID           int64     `json:"id"`
	Title        string    `json:"title"`
	Fingerprint  string    `json:"fingerprint"`
	Summary      string    `json:"summary"`
	FirstSeenAt  time.Time `json:"first_seen_at"`
	LastSeenAt   time.Time `json:"last_seen_at"`
	ArticleCount int       `json:"article_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
