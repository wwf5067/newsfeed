package crawler

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wwf5067/newsfeed/internal/model"
)

// Repository 抓取服务的数据写入层。
// 暂时用裸 SQL 占位,后续接 sqlc 生成的代码替换实现即可。
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// UpsertArticle 按 url 唯一约束去重写入,并多写一条 heat snapshot 用于趋势图。
// 若 url 已存在,更新 title/content/author/heat/published_at/fetched_at,
// 并把上一次的 heat / heat_value 搬到 prev_* 字段(用于前端展示趋势)。
// 返回值 inserted=true 表示新插入,false 表示更新已有记录。
//
// articles UPSERT 和 snapshot INSERT 包在同一事务里,保证一致性:
// 失败回滚,绝不出现"主表更新成功但 snapshot 缺失"的状态。
func (r *Repository) UpsertArticle(ctx context.Context, a model.Article) (inserted bool, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() {
		// 任何错误回滚;成功 commit 后这次 rollback 是 no-op
		_ = tx.Rollback(ctx)
	}()

	const upsertQ = `
INSERT INTO articles (source_key, url, title, content, author, heat, heat_value, published_at, fetched_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
ON CONFLICT (url) DO UPDATE SET
    title           = EXCLUDED.title,
    content         = EXCLUDED.content,
    author          = EXCLUDED.author,
    prev_heat       = articles.heat,
    prev_heat_value = articles.heat_value,
    heat            = EXCLUDED.heat,
    heat_value      = EXCLUDED.heat_value,
    published_at    = EXCLUDED.published_at,
    fetched_at      = NOW()
RETURNING id, (xmax = 0) AS is_new
`
	var (
		id    int64
		isNew bool
	)
	if err := tx.QueryRow(ctx, upsertQ,
		a.SourceKey, a.URL, a.Title, a.Content, a.Author, a.Heat, a.HeatValue, a.PublishedAt,
	).Scan(&id, &isNew); err != nil {
		return false, err
	}

	// 写 snapshot:同一秒内重复触发(理论不会但保险)走 ON CONFLICT DO NOTHING
	const snapshotQ = `
INSERT INTO article_heat_snapshots (article_id, heat_value)
VALUES ($1, $2)
ON CONFLICT (article_id, captured_at) DO NOTHING
`
	if _, err := tx.Exec(ctx, snapshotQ, id, a.HeatValue); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return isNew, nil
}

// PurgeOldArticles 删除 fetched_at 早于 days 天前的文章,返回被删除的行数。
func (r *Repository) PurgeOldArticles(ctx context.Context, days int) (int64, error) {
	const q = `DELETE FROM articles WHERE fetched_at < NOW() - INTERVAL '1 day' * $1`
	tag, err := r.pool.Exec(ctx, q, days)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
