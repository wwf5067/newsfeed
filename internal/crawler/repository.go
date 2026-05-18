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

// UpsertArticle 按 url 唯一约束去重写入。
// 若 url 已存在,更新 title/content/author/heat/published_at/fetched_at,
// 并把上一次的 heat / heat_value 搬到 prev_* 字段(用于前端展示趋势)。
// 返回值 inserted=true 表示新插入,false 表示更新已有记录。
func (r *Repository) UpsertArticle(ctx context.Context, a model.Article) (inserted bool, err error) {
	const q = `
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
RETURNING (xmax = 0) AS is_new
`
	var isNew bool
	err = r.pool.QueryRow(ctx, q,
		a.SourceKey, a.URL, a.Title, a.Content, a.Author, a.Heat, a.HeatValue, a.PublishedAt,
	).Scan(&isNew)
	if err != nil {
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
