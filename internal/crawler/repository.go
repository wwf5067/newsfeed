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

// UpsertArticle 按 url 唯一约束去重写入。返回是否新插入。
func (r *Repository) UpsertArticle(ctx context.Context, a model.Article) (inserted bool, err error) {
	const q = `
INSERT INTO articles (source_key, url, title, content, author, published_at, fetched_at)
VALUES ($1, $2, $3, $4, $5, $6, NOW())
ON CONFLICT (url) DO NOTHING
RETURNING id
`
	var id int64
	err = r.pool.QueryRow(ctx, q,
		a.SourceKey, a.URL, a.Title, a.Content, a.Author, a.PublishedAt,
	).Scan(&id)
	if err != nil {
		// pgx: ON CONFLICT DO NOTHING + 命中冲突时,QueryRow 返回 ErrNoRows
		if err.Error() == "no rows in result set" {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
