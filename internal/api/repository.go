package api

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wwf5067/newsfeed/internal/model"
)

// Repository api 服务的只读数据层。建议使用只读 DB 账号连接。
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// ListArticles 简单分页查询,后续按业务再扩展过滤条件。
func (r *Repository) ListArticles(ctx context.Context, limit, offset int) ([]model.Article, error) {
	const q = `
SELECT id, source_key, url, title, content, author,
       heat, heat_value, prev_heat, prev_heat_value,
       published_at, fetched_at
FROM articles
ORDER BY published_at DESC
LIMIT $1 OFFSET $2
`
	rows, err := r.pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Article
	for rows.Next() {
		var a model.Article
		if err := rows.Scan(&a.ID, &a.SourceKey, &a.URL, &a.Title, &a.Content,
			&a.Author, &a.Heat, &a.HeatValue, &a.PrevHeat, &a.PrevHeatValue,
			&a.PublishedAt, &a.FetchedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListActiveAnnouncements 返回当前生效的公告(按 priority 降序、创建时间倒序)。
// 过滤条件:软开关 is_active=TRUE,starts_at 已到达,ends_at 未到达(或为 NULL)。
// 公告量不大,这里不分页;前端一次性取全部已生效条目。
func (r *Repository) ListActiveAnnouncements(ctx context.Context) ([]model.Announcement, error) {
	const q = `
SELECT id, content, level, priority, starts_at, ends_at, is_active, created_at, updated_at
FROM announcements
WHERE is_active = TRUE
  AND starts_at <= NOW()
  AND (ends_at IS NULL OR ends_at > NOW())
ORDER BY priority DESC, created_at DESC
`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Announcement
	for rows.Next() {
		var a model.Announcement
		if err := rows.Scan(&a.ID, &a.Content, &a.Level, &a.Priority,
			&a.StartsAt, &a.EndsAt, &a.IsActive, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
