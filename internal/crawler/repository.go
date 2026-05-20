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
// 若 url 已存在,更新 title/content/author/heat/fetched_at,
// 并把上一次的 heat / heat_value 搬到 prev_* 字段(用于前端展示趋势)。
//
// published_at 锁定首次写入值,后续 UPSERT 不再覆写。语义:
//   - "published_at" 在我们的语境里 = 这条热搜首次被我们看到的时间(=上榜时间)
//   - 热搜在榜期间每 30 分钟抓一次,如果每次都更 published_at = NOW(),前端
//     按时间排序会让长期占榜的热搜永远显示在最前,失去时序意义。
//   - 知乎源传的是 t.Created(问题创建时间,不变);百度/微博源传的是 time.Now()
//     每次都新。COALESCE 兜底统一行为,让所有源都"首次时间锁住"。
//
// fetched_at 仍每次更新,用作 retention purge(超 N 天没抓到 → 已下榜 → 删)。
//
// 返回值:
//   - id:文章主键(无论新插入还是更新都返回)
//   - inserted=true 表示新插入,false 表示更新已有记录
//
// articles UPSERT 和 snapshot INSERT 包在同一事务里,保证一致性:
// 失败回滚,绝不出现"主表更新成功但 snapshot 缺失"的状态。
func (r *Repository) UpsertArticle(ctx context.Context, a model.Article) (id int64, inserted bool, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer func() {
		// 任何错误回滚;成功 commit 后这次 rollback 是 no-op
		_ = tx.Rollback(ctx)
	}()

	const upsertQ = `
INSERT INTO articles (source_key, url, title, content, author, heat, heat_value, source_rank, published_at, fetched_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, 0), $9, NOW())
ON CONFLICT (url) DO UPDATE SET
    title           = EXCLUDED.title,
    content         = EXCLUDED.content,
    author          = EXCLUDED.author,
    prev_heat       = articles.heat,
    prev_heat_value = articles.heat_value,
    heat            = EXCLUDED.heat,
    heat_value      = EXCLUDED.heat_value,
    source_rank     = EXCLUDED.source_rank,
    published_at    = COALESCE(articles.published_at, EXCLUDED.published_at),
    fetched_at      = NOW()
RETURNING id, (xmax = 0) AS is_new
`
	var isNew bool
	if err := tx.QueryRow(ctx, upsertQ,
		a.SourceKey, a.URL, a.Title, a.Content, a.Author, a.Heat, a.HeatValue, a.SourceRank, a.PublishedAt,
	).Scan(&id, &isNew); err != nil {
		return 0, false, err
	}

	// 写 snapshot:同一秒内重复触发(理论不会但保险)走 ON CONFLICT DO NOTHING
	const snapshotQ = `
INSERT INTO article_heat_snapshots (article_id, heat_value)
VALUES ($1, $2)
ON CONFLICT (article_id, captured_at) DO NOTHING
`
	if _, err := tx.Exec(ctx, snapshotQ, id, a.HeatValue); err != nil {
		return 0, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, false, err
	}
	return id, isNew, nil
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

// SourceStat 单个 source 的当日统计:总数 + 最热条目。
type SourceStat struct {
	SourceKey string
	Count     int
	TopTitle  string
	TopHeat   string // 源原文文本(如 "571 万热度");为空时调用方决定要不要兜底
}

// TodayStatsBySource 按"今日"(NOW() 当地时区 0 点起)统计每个源的文章数 + 该源最热一条。
// 用于 daily summary 公告生成。
//
// 最热口径:每个源取最新一次抓取批次中 source_rank 最小的那条(= 该源官方
// 榜单 rank 1)。所有 scraper 都把官方榜位写到 source_rank 字段(1-based)。
// 不直接用 heat_value 是因为各平台官方 rank 不完全跟 heat_value 一致(还含
// 推荐权重 / 互动量 / 关键词权重),按 heat_value 取的"最热"可能跟用户在源
// 网站首页看到的 rank 1 不是同一条。
//
// 数据量小(单日 100~200 条),用 LATERAL 风格的子查询直接出结果,不必优化。
func (r *Repository) TodayStatsBySource(ctx context.Context) ([]SourceStat, error) {
	const q = `
WITH today AS (
    SELECT source_key, title, heat, heat_value, source_rank, published_at, fetched_at
    FROM articles
    WHERE fetched_at >= date_trunc('day', NOW())
),
latest_per_source AS (
    SELECT source_key, date_trunc('minute', MAX(fetched_at)) AS cutoff
    FROM today
    GROUP BY source_key
)
SELECT
    t.source_key,
    COUNT(*) AS cnt,
    (
        SELECT title FROM today AS x
        WHERE x.source_key = t.source_key
          AND x.fetched_at >= (
              SELECT cutoff FROM latest_per_source WHERE source_key = t.source_key
          )
          AND x.source_rank IS NOT NULL
        ORDER BY x.source_rank ASC
        LIMIT 1
    ) AS top_title,
    (
        SELECT COALESCE(heat, '') FROM today AS x
        WHERE x.source_key = t.source_key
          AND x.fetched_at >= (
              SELECT cutoff FROM latest_per_source WHERE source_key = t.source_key
          )
          AND x.source_rank IS NOT NULL
        ORDER BY x.source_rank ASC
        LIMIT 1
    ) AS top_heat
FROM today AS t
GROUP BY t.source_key
ORDER BY cnt DESC
`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SourceStat
	for rows.Next() {
		var s SourceStat
		if err := rows.Scan(&s.SourceKey, &s.Count, &s.TopTitle, &s.TopHeat); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
