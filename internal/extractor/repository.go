package extractor

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository extractor 服务的数据层。同时负责读 articles 待处理行 + 写
// entities/events/关联表 + 标记 articles.extracted_at。
//
// 设计上希望连这一份 pool 的 PG 账号有:
//   - articles SELECT (id,title,content,published_at) + UPDATE (extracted_at,extract_attempts)
//   - entities/events SELECT/INSERT/UPDATE
//   - article_entities/article_events SELECT/INSERT
//
// 不需要 articles 的 INSERT/DELETE,确保 extractor 不能伪造或抹掉文章。
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// PendingArticle 是 worker 处理批次的精简载荷,只取抽取需要的字段。
type PendingArticle struct {
	ID          int64
	Title       string
	Content     string
	PublishedAt time.Time
}

// PickPending 取一批待抽取的文章。
//
// 用 FOR UPDATE SKIP LOCKED 保证多 worker 实例同时跑也不会重复处理同一行。
// 注意:行锁会持续到事务提交,所以调用方应当尽快处理完并提交,或在外层维持事务。
//
// 当前 Runner 的策略是:每条 article 单独开事务,这里只读不锁——
// 多 worker 场景下用 extract_attempts 的递增做天然去重(并发处理同一行只会
// 让 attempts +2,不会写坏数据,因为关联表是 ON CONFLICT DO NOTHING)。
// 如果未来需要严格互斥,改用 SKIP LOCKED + 单事务即可。
func (r *Repository) PickPending(ctx context.Context, limit int) ([]PendingArticle, error) {
	const q = `
SELECT id, title, content, published_at
FROM articles
WHERE extracted_at IS NULL AND extract_attempts < 3
ORDER BY published_at DESC
LIMIT $1
`
	rows, err := r.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PendingArticle
	for rows.Next() {
		var a PendingArticle
		if err := rows.Scan(&a.ID, &a.Title, &a.Content, &a.PublishedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// WithTx 在单事务里跑 fn。fn 返回错误则回滚,nil 则提交。
// 这是处理一篇 article 的"原子化抽取写入"的标准入口。
func (r *Repository) WithTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // 提交后 Rollback 是 no-op

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// UpsertEntity 按 (name, type) 去重。已存在则刷新 updated_at,返回 id。
func (r *Repository) UpsertEntity(ctx context.Context, tx pgx.Tx, name, typ string) (int64, error) {
	const q = `
INSERT INTO entities (name, type) VALUES ($1, $2)
ON CONFLICT (name, type) DO UPDATE SET updated_at = NOW()
RETURNING id
`
	var id int64
	if err := tx.QueryRow(ctx, q, name, typ).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// UpsertEvent 按 fingerprint 去重。冲突时不覆盖原 title(首次写入的标题更可能完整),
// 但 last_seen_at 会通过 TouchEvent 单独维护。
func (r *Repository) UpsertEvent(ctx context.Context, tx pgx.Tx, fingerprint, title string, publishedAt time.Time) (int64, error) {
	const q = `
INSERT INTO events (title, fingerprint, first_seen_at, last_seen_at)
VALUES ($1, $2, $3, $3)
ON CONFLICT (fingerprint) DO UPDATE SET updated_at = NOW()
RETURNING id
`
	var id int64
	if err := tx.QueryRow(ctx, q, title, fingerprint, publishedAt).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// LinkArticleEntity 建立文章 ↔ 实体关联。重复关联静默跳过。
func (r *Repository) LinkArticleEntity(ctx context.Context, tx pgx.Tx, articleID, entityID int64) error {
	const q = `
INSERT INTO article_entities (article_id, entity_id) VALUES ($1, $2)
ON CONFLICT DO NOTHING
`
	_, err := tx.Exec(ctx, q, articleID, entityID)
	return err
}

// LinkArticleEvent 建立文章 ↔ 事件关联。
// 返回 created=true 表示这是该事件的新文章(用于 TouchEvent 决定是否要 +1)。
func (r *Repository) LinkArticleEvent(ctx context.Context, tx pgx.Tx, articleID, eventID int64) (created bool, err error) {
	const q = `
INSERT INTO article_events (article_id, event_id) VALUES ($1, $2)
ON CONFLICT DO NOTHING
`
	tag, err := tx.Exec(ctx, q, articleID, eventID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// TouchEvent 更新事件的 last_seen_at(取最大值)与 article_count(+1)。
// 仅当 LinkArticleEvent 返回 created=true 时才应该调用,避免重复计数。
func (r *Repository) TouchEvent(ctx context.Context, tx pgx.Tx, eventID int64, publishedAt time.Time) error {
	const q = `
UPDATE events SET
    last_seen_at  = GREATEST(last_seen_at, $2),
    first_seen_at = LEAST(first_seen_at, $2),
    article_count = article_count + 1,
    updated_at    = NOW()
WHERE id = $1
`
	_, err := tx.Exec(ctx, q, eventID, publishedAt)
	return err
}

// MarkExtracted 把文章标记为已抽取,worker 下次不再捡到它。
// 同时把 extract_attempts 清零(成功路径不留痕)。
func (r *Repository) MarkExtracted(ctx context.Context, tx pgx.Tx, articleID int64) error {
	const q = `UPDATE articles SET extracted_at = NOW(), extract_attempts = 0 WHERE id = $1`
	_, err := tx.Exec(ctx, q, articleID)
	return err
}

// IncrAttempts 失败路径专用:在事务外单独累加重试次数。
// 不写 extracted_at,文章下个周期还会被捞起,直到 attempts 达到 3 后被部分索引排除。
func (r *Repository) IncrAttempts(ctx context.Context, articleID int64) error {
	const q = `UPDATE articles SET extract_attempts = extract_attempts + 1 WHERE id = $1`
	_, err := r.pool.Exec(ctx, q, articleID)
	return err
}
