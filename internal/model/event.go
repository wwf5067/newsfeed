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

// EventResponse 事件的 API 响应格式
type EventResponse struct {
	ID           int64    `json:"id"`
	Title        string   `json:"title"`
	Fingerprint  string   `json:"fingerprint"`
	Summary      string   `json:"summary,omitempty"`
	ArticleCount int      `json:"article_count"`
	FirstSeenAt  string   `json:"first_seen_at"`
	LastSeenAt   string   `json:"last_seen_at"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

// EventTimelineItem 事件时间线中的一篇文章
type EventTimelineItem struct {
	ArticleID   int64    `json:"article_id"`
	Title       string   `json:"title"`
	URL         string   `json:"url"`
	SourceKey   string   `json:"source_key"`
	PublishedAt string   `json:"published_at"`
	Entities    []string `json:"entities"`
}
