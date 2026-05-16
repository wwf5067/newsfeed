package model

import "time"

// Announcement 顶部公告栏的一条记录。
// EndsAt 为 nil 表示无截止时间(常驻公告)。
type Announcement struct {
	ID        int64      `json:"id"`
	Content   string     `json:"content"`
	Level     string     `json:"level"` // info|warn|critical
	Priority  int        `json:"priority"`
	StartsAt  time.Time  `json:"starts_at"`
	EndsAt    *time.Time `json:"ends_at"`
	IsActive  bool       `json:"is_active"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}
