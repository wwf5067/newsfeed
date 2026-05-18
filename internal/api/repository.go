package api

import (
	"context"
	"fmt"
	"strings"
	"time"

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

// ListArticles 分页查询。
// query 非空时按 title/content ILIKE 过滤;sourceKey 非空时按源精确匹配。
// 当前数据量(几千行 + 30 天 retention)ILIKE 完全跑得动,等量级到十万再考虑全文索引。
func (r *Repository) ListArticles(ctx context.Context, limit, offset int, query, sourceKey string) ([]model.Article, error) {
	var (
		conds []string
		args  []any
	)

	if sourceKey != "" {
		args = append(args, sourceKey)
		conds = append(conds, fmt.Sprintf("source_key = $%d", len(args)))
	}
	if query != "" {
		args = append(args, "%"+query+"%")
		idx := len(args)
		conds = append(conds, fmt.Sprintf("(title ILIKE $%d OR content ILIKE $%d)", idx, idx))
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	args = append(args, limit, offset)
	limitIdx, offsetIdx := len(args)-1, len(args)
	q := fmt.Sprintf(`
SELECT id, source_key, url, title, content, author,
       heat, heat_value, prev_heat, prev_heat_value,
       published_at, fetched_at
FROM articles
%s
ORDER BY published_at DESC
LIMIT $%d OFFSET $%d
`, where, limitIdx, offsetIdx)

	rows, err := r.pool.Query(ctx, q, args...)
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

// GetArticle 按主键查单条文章。未命中返回 pgx.ErrNoRows,上层据此转 404。
func (r *Repository) GetArticle(ctx context.Context, id int64) (*model.Article, error) {
	const q = `
SELECT id, source_key, url, title, content, author,
       heat, heat_value, prev_heat, prev_heat_value,
       published_at, fetched_at
FROM articles
WHERE id = $1
`
	var a model.Article
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&a.ID, &a.SourceKey, &a.URL, &a.Title, &a.Content,
		&a.Author, &a.Heat, &a.HeatValue, &a.PrevHeat, &a.PrevHeatValue,
		&a.PublishedAt, &a.FetchedAt,
	)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// HeatPoint 单个时序数据点(用于前端 sparkline)。
type HeatPoint struct {
	HeatValue  int64     `json:"heat_value"`
	CapturedAt time.Time `json:"captured_at"`
}

// GetHeatHistory 拉某条文章最近 limit 条 heat snapshot,按时间正序返回(便于前端直接画线)。
// limit 应由调用方限制在合理范围(如 48 = 24h × 30min)。
func (r *Repository) GetHeatHistory(ctx context.Context, articleID int64, limit int) ([]HeatPoint, error) {
	if limit <= 0 || limit > 500 {
		limit = 48
	}
	// 子查询拿最近 N 条(降序),再外层按时间升序;前端无需反转
	const q = `
SELECT heat_value, captured_at FROM (
    SELECT heat_value, captured_at
    FROM article_heat_snapshots
    WHERE article_id = $1
    ORDER BY captured_at DESC
    LIMIT $2
) sub
ORDER BY captured_at ASC
`
	rows, err := r.pool.Query(ctx, q, articleID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []HeatPoint
	for rows.Next() {
		var p HeatPoint
		if err := rows.Scan(&p.HeatValue, &p.CapturedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
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
