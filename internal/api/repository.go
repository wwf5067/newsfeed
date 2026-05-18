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

func buildArticleWhereClause(query, sourceKey string) (string, []any) {
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

	if len(conds) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}

func (r *Repository) queryArticles(
	ctx context.Context,
	where string,
	args []any,
	limit, offset int,
) ([]model.Article, error) {
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

// ListArticlesPage 返回分页数据和命中的总数,用于前端“加载更多”。
func (r *Repository) ListArticlesPage(
	ctx context.Context,
	limit, offset int,
	query, sourceKey string,
) ([]model.Article, int, error) {
	where, args := buildArticleWhereClause(query, sourceKey)

	countQ := fmt.Sprintf(`SELECT COUNT(*) FROM articles %s`, where)
	var total int
	if err := r.pool.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []model.Article{}, 0, nil
	}

	items, err := r.queryArticles(ctx, where, args, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// ListArticles 分页查询。
// query 非空时按 title/content ILIKE 过滤;sourceKey 非空时按源精确匹配。
// 当前数据量(几千行 + 30 天 retention)ILIKE 完全跑得动,等量级到十万再考虑全文索引。
func (r *Repository) ListArticles(ctx context.Context, limit, offset int, query, sourceKey string) ([]model.Article, error) {
	where, args := buildArticleWhereClause(query, sourceKey)
	return r.queryArticles(ctx, where, args, limit, offset)
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

// SurgingArticle 飙升榜返回的复合结构:Article + 窗口期内的增量信息。
// SurgeDelta = 当前 heat_value - 窗口起点 heat_value;前端用它做"↑ XX 万 / 6h"展示。
// WindowStartHeat 给前端做参考用(可选展示),也方便排查。
type SurgingArticle struct {
	model.Article
	SurgeDelta      int64 `json:"surge_delta"`
	WindowStartHeat int64 `json:"window_start_heat"`
}

// ListSurging 返回时间窗口内热度增长最大的文章,按源过滤、按增量降序。
//
// 思路:对每条 article 取它在窗口起点(NOW - windowHours)及之前最近的一条 snapshot
// 作为基准热度,用当前 articles.heat_value 减去基准得到增量。没有窗口起点之前的
// snapshot(即首次抓取在窗口内)的条目排除——它们没有可比基准,放进飙升榜会扭曲排序。
//
// minHeat 用于过滤量级太小的"伪飙升"(如 0→500,百分比看着炸但实际是噪声)。
func (r *Repository) ListSurging(
	ctx context.Context,
	sourceKey string,
	limit, windowHours, minHeat int,
) ([]SurgingArticle, error) {
	// 限定窗口取值范围,防止外部传 windowHours=99999 拖慢扫描
	if windowHours <= 0 || windowHours > 168 {
		windowHours = 6
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if minHeat < 0 {
		minHeat = 0
	}

	var (
		conds = []string{"a.heat_value >= $1"}
		args  = []any{minHeat}
	)
	args = append(args, windowHours)
	windowIdx := len(args) // $2

	if sourceKey != "" {
		args = append(args, sourceKey)
		conds = append(conds, fmt.Sprintf("a.source_key = $%d", len(args)))
	}
	args = append(args, limit)
	limitIdx := len(args)

	// LATERAL 子查询拿每条 article 在窗口起点之前最近的一条 snapshot 作为基准。
	// 没有这种 snapshot 的(首次抓取就在窗口内)被 INNER JOIN 过滤掉。
	// make_interval(hours => $N) 比 ($N || ' hours')::interval 安全:后者要求
	// 两侧都是 text,pgx 默认把 Go int 传成 integer,会触发类型不匹配。
	q := fmt.Sprintf(`
SELECT a.id, a.source_key, a.url, a.title, a.content, a.author,
       a.heat, a.heat_value, a.prev_heat, a.prev_heat_value,
       a.published_at, a.fetched_at,
       s.heat_value AS window_start_heat,
       (a.heat_value - s.heat_value) AS surge_delta
FROM articles a
JOIN LATERAL (
    SELECT heat_value
    FROM article_heat_snapshots
    WHERE article_id = a.id
      AND captured_at <= NOW() - make_interval(hours => $%d)
    ORDER BY captured_at DESC
    LIMIT 1
) s ON TRUE
WHERE %s
  AND a.heat_value > s.heat_value
ORDER BY surge_delta DESC
LIMIT $%d
`, windowIdx, strings.Join(conds, " AND "), limitIdx)

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SurgingArticle
	for rows.Next() {
		var s SurgingArticle
		if err := rows.Scan(
			&s.ID, &s.SourceKey, &s.URL, &s.Title, &s.Content,
			&s.Author, &s.Heat, &s.HeatValue, &s.PrevHeat, &s.PrevHeatValue,
			&s.PublishedAt, &s.FetchedAt,
			&s.WindowStartHeat, &s.SurgeDelta,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
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
