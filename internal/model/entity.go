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
