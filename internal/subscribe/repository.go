// Package subscribe 关键词订阅:抓取到新文章命中关键词时,聚合发邮件通知。
//
// 设计:
//   - 写路径:每次 runOnce 抓完一个源后调用 Match,查未通知过的命中条目
//   - 去重:keyword_notifications 表记录"哪个订阅通知过哪篇文章",反向 LEFT JOIN 过滤
//   - 邮件:命中聚合成一封,按关键词分组列出,无命中不发
package subscribe

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Subscription 一条订阅记录。
type Subscription struct {
	ID        int64     `json:"id"`
	Keyword   string    `json:"keyword"`
	CreatedAt time.Time `json:"created_at"`
}

// Hit 一次匹配命中:某订阅关键词命中某篇文章。
// Matcher 把所有 Hit 聚合成一封邮件后,批量写入 keyword_notifications 表完成去重。
type Hit struct {
	SubscriptionID int64
	Keyword        string
	ArticleID      int64
	Title          string
	URL            string
	SourceKey      string
	Heat           string
}

// Repository 关键词订阅的读写。读路径供 API,写路径供 matcher。
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// List 全部订阅,按创建时间倒序。
func (r *Repository) List(ctx context.Context) ([]Subscription, error) {
	const q = `SELECT id, keyword, created_at FROM keyword_subscriptions ORDER BY created_at DESC`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Subscription
	for rows.Next() {
		var s Subscription
		if err := rows.Scan(&s.ID, &s.Keyword, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Add 添加一条订阅。重复(LOWER 唯一)时返回已有记录,不报错。
// keyword 会做 trim 但不改大小写;唯一性由 LOWER 索引保证。
//
// 实现:Postgres 的 ON CONFLICT 不直接支持 expression index,必须先查再插。
// 用一个事务保证"查 + 插"原子,避免并发下双写(虽然单实例 crawler 不太会发生)。
func (r *Repository) Add(ctx context.Context, keyword string) (*Subscription, error) {
	keyword = strings.TrimSpace(keyword)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var s Subscription
	// 先按 LOWER(keyword) 查找已有
	err = tx.QueryRow(ctx,
		`SELECT id, keyword, created_at FROM keyword_subscriptions WHERE LOWER(keyword) = LOWER($1)`,
		keyword,
	).Scan(&s.ID, &s.Keyword, &s.CreatedAt)
	if err == nil {
		// 已存在,直接返回
		return &s, tx.Commit(ctx)
	}
	// 没找到 → 插入
	if err := tx.QueryRow(ctx,
		`INSERT INTO keyword_subscriptions (keyword) VALUES ($1)
         RETURNING id, keyword, created_at`,
		keyword,
	).Scan(&s.ID, &s.Keyword, &s.CreatedAt); err != nil {
		return nil, err
	}
	return &s, tx.Commit(ctx)
}

// Delete 按 id 删订阅;关联的 keyword_notifications 由 ON DELETE CASCADE 自动清理。
// 返回是否真删掉一行(false = id 不存在)。
func (r *Repository) Delete(ctx context.Context, id int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM keyword_subscriptions WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// FindHits 在指定的 article id 列表中,查所有"还没通知过"的关键词命中。
// 一次性把"取订阅 + 文章字段 + 去重过滤"做完,避免在 Go 里循环查 N 次。
//
// 实现思路:
//   - articles ⨯ keyword_subscriptions 笛卡尔配对
//   - WHERE title || content ILIKE '%' || keyword || '%'
//   - LEFT JOIN keyword_notifications 过滤掉已通知组合
func (r *Repository) FindHits(ctx context.Context, articleIDs []int64) ([]Hit, error) {
	if len(articleIDs) == 0 {
		return nil, nil
	}
	const q = `
SELECT
    s.id, s.keyword,
    a.id, a.title, a.url, a.source_key, a.heat
FROM keyword_subscriptions s
CROSS JOIN articles a
LEFT JOIN keyword_notifications n
       ON n.subscription_id = s.id AND n.article_id = a.id
WHERE a.id = ANY($1)
  AND n.subscription_id IS NULL
  AND (a.title ILIKE '%' || s.keyword || '%'
       OR a.content ILIKE '%' || s.keyword || '%')
ORDER BY s.id, a.id
`
	rows, err := r.pool.Query(ctx, q, articleIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Hit
	for rows.Next() {
		var h Hit
		if err := rows.Scan(
			&h.SubscriptionID, &h.Keyword,
			&h.ArticleID, &h.Title, &h.URL, &h.SourceKey, &h.Heat,
		); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// PreviewMatch 一条预览样本:命中关键词的文章简要信息。
// 仅供前端"预估命中量"使用,不写库、不去重。
type PreviewMatch struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	SourceKey string `json:"source_key"`
}

// PreviewMatches 给定关键词,在最近 7 天 articles 里数有多少篇能匹配,并返回最多 5 条样本。
// 语义与 FindHits 一致(title/content ILIKE),所以预估数 vs 未来真实通知数大致对得上。
//
// 没有读 keyword_notifications,所以"老订阅已通知过的"也会被算进去——
// 但这是给"新订阅前评估关键词好坏"用的,看的就是命中体量,不是看新增量,行为是对的。
//
// 7 天窗口是为了避免全表扫;articles 表会持续增长,无时间过滤的 ILIKE 在大表上会很慢。
func (r *Repository) PreviewMatches(ctx context.Context, keyword string) (count int, samples []PreviewMatch, err error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return 0, nil, nil
	}
	// 单次 query 同时取 count 和 sample 比较麻烦,直接两条 SQL 顺序跑,
	// 都走同一个 ILIKE 表达式,Postgres 自然能利用同一份扫描结果(或索引)。
	const cutoff = "NOW() - INTERVAL '7 days'"
	const countQ = `
SELECT COUNT(*) FROM articles
WHERE published_at >= ` + cutoff + `
  AND (title ILIKE '%' || $1 || '%' OR content ILIKE '%' || $1 || '%')
`
	if err = r.pool.QueryRow(ctx, countQ, keyword).Scan(&count); err != nil {
		return 0, nil, err
	}
	if count == 0 {
		return 0, nil, nil
	}
	const sampleQ = `
SELECT id, title, source_key FROM articles
WHERE published_at >= ` + cutoff + `
  AND (title ILIKE '%' || $1 || '%' OR content ILIKE '%' || $1 || '%')
ORDER BY published_at DESC
LIMIT 5
`
	rows, err := r.pool.Query(ctx, sampleQ, keyword)
	if err != nil {
		return count, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m PreviewMatch
		if err := rows.Scan(&m.ID, &m.Title, &m.SourceKey); err != nil {
			return count, nil, err
		}
		samples = append(samples, m)
	}
	return count, samples, rows.Err()
}

// MarkNotified 批量写入 keyword_notifications 完成去重登记。
// ON CONFLICT DO NOTHING 兼容并发场景(理论上不会发生,但加上不损失成本)。
func (r *Repository) MarkNotified(ctx context.Context, hits []Hit) error {
	if len(hits) == 0 {
		return nil
	}
	const q = `
INSERT INTO keyword_notifications (subscription_id, article_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING
`
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, h := range hits {
		if _, err := tx.Exec(ctx, q, h.SubscriptionID, h.ArticleID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
