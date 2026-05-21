package api

import (
	"context"
	"fmt"
	"sort"
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

// ListRecentArticles 拉最近 windowHours 小时内"首次出现"的文章,用于热点/实体聚合。
//
// 这里使用 published_at 而不是 fetched_at:
//   - fetched_at 会在每次抓取时刷新,会让长期在榜内容在 3h/6h/24h 都持续命中
//   - published_at 在首次写入后锁定(上榜/首次看到时间语义),更符合窗口统计预期
//     (3h 应主要看近 3h 新进入视野的事件与实体)
func (r *Repository) ListRecentArticles(ctx context.Context, windowHours, limit int) ([]model.Article, error) {
	if windowHours <= 0 || windowHours > 168 {
		windowHours = 24
	}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	const q = `
SELECT id, source_key, url, title, content, author,
       heat, heat_value, prev_heat, prev_heat_value,
       published_at, fetched_at
FROM articles
WHERE published_at >= NOW() - make_interval(hours => $1)
ORDER BY heat_value DESC NULLS LAST, published_at DESC
LIMIT $2
`
	rows, err := r.pool.Query(ctx, q, windowHours, limit)
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

// ListArticlesByTerms 按多个别名做 ILIKE OR 匹配,返回 published_at 倒序。
//
// 用于实体页聚合:从 lexicon 拿到某个 Label 的所有别名(如"特朗普 / Trump / trump / 川普"),
// 一次 SQL 把全 30 天 retention 内匹配的文章捞出来,前端再做时间分组展示。
//
// 参数:
//   - terms:别名列表,空切片直接返回 nil
//   - sinceHours:0 表示不限时间,>0 限制 published_at >= NOW() - sinceHours
//   - limit:默认 200,>500 截断到 500
//
// 安全:每个 term 走 $N 参数化绑定,不拼字符串,杜绝 SQL 注入。
// terms 数量上限 30,防止某些极端 lexicon entry 别名爆炸把 SQL 撑爆。
func (r *Repository) ListArticlesByTerms(
	ctx context.Context,
	terms []string,
	sinceHours int,
	limit int,
) ([]model.Article, error) {
	if len(terms) == 0 {
		return nil, nil
	}
	if len(terms) > 30 {
		terms = terms[:30]
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}

	// title 匹配为主,content 匹配为辅(提升召回率)。
	// 实体提取基于 title,但 content 中命中同一实体说明文章确实相关,保留。
	conds := make([]string, 0, len(terms)*2)
	args := make([]any, 0, len(terms)+2)
	for _, t := range terms {
		args = append(args, "%"+t+"%")
		idx := len(args)
		conds = append(conds, fmt.Sprintf("title ILIKE $%d", idx))
		conds = append(conds, fmt.Sprintf("content ILIKE $%d", idx))
	}
	where := "WHERE (" + strings.Join(conds, " OR ") + ")"

	if sinceHours > 0 {
		args = append(args, sinceHours)
		where += fmt.Sprintf(" AND published_at >= NOW() - make_interval(hours => $%d)", len(args))
	}

	args = append(args, limit)
	limitIdx := len(args)

	q := fmt.Sprintf(`
SELECT id, source_key, url, title, content, author,
       heat, heat_value, prev_heat, prev_heat_value,
       published_at, fetched_at
FROM articles
%s
ORDER BY published_at DESC
LIMIT $%d
`, where, limitIdx)

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

// HotlistItem 热榜条目,包含当前排名及排名变化信息。
//
// RankChange > 0 表示上升(如从第5名升到第3名,RankChange=2)。
// RankChange < 0 表示下降。IsNew=true 表示首次进入该榜(prev_heat_value=0
// 或上次排名在 topN 之外)。
type HotlistItem struct {
	model.Article
	Rank       int  `json:"rank"`
	RankChange int  `json:"rank_change"`
	IsNew      bool `json:"is_new"`
}

// ListHotlistItems 按 heat_value 降序返回某来源的热榜前 topN 条,
// 并计算每条相对上一次抓取周期的排名变化。
//
// 实现思路:
//  1. 拉 topN*2 条当前 heat_value 最高的文章(扩大采样池,让 prev 排名计算更准)
//  2. 将同批文章按 prev_heat_value 降序排序,得到"上次"名次
//  3. 对当前 top N 里每条文章:
//     - prev_heat_value=0 或上次名次 > topN → IsNew=true
//     - 否则 RankChange = prev_rank - current_rank(正=上升,负=下降)
func (r *Repository) ListHotlistItems(ctx context.Context, sourceKey string, topN int) ([]HotlistItem, error) {
	if topN <= 0 || topN > 50 {
		topN = 15
	}
	fetchN := topN * 2

	// 所有热搜源(知乎/百度/微博)的 scraper 把官方榜位写到 source_rank 字段
	// (1-based,1 = 榜首)。取最新一次抓取批次,按 source_rank ASC 排序就是
	// 各平台官方榜单顺序。
	// 不再用 published_at 编码 rank — 那样跟"问题创建时间/上榜时间"语义打架,
	// 各 tab 列表想按时间序排会出问题。现在 published_at 回归本意,各 tab 用
	// published_at DESC 排,首页 HotPanel 用 source_rank ASC 排,各取所需。
	//
	// 用 DISTINCT ON (title) 去重:同一标题只保留首选那条。微博源历史上 URL
	// 含 band_rank 不稳定,DB 里同一热搜词被存成多行(commit 7f73981 已修但
	// 存量数据还在),首页 hotlist 直接 SQL 去重最干净。
	const q = `
WITH latest AS (
    SELECT date_trunc('minute', MAX(fetched_at)) AS cutoff
    FROM articles WHERE source_key = $1
)
SELECT id, source_key, url, title, content, author,
       heat, heat_value, prev_heat, prev_heat_value,
       COALESCE(source_rank, 0), published_at, fetched_at
FROM (
    SELECT DISTINCT ON (title)
           id, source_key, url, title, content, author,
           heat, heat_value, prev_heat, prev_heat_value,
           source_rank, published_at, fetched_at
    FROM articles, latest
    WHERE source_key = $1
      AND heat_value > 0
      AND fetched_at >= latest.cutoff
      AND source_rank IS NOT NULL
    ORDER BY title, source_rank ASC
) AS dedup
ORDER BY source_rank ASC
LIMIT $2
`
	rows, err := r.pool.Query(ctx, q, sourceKey, fetchN)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var all []model.Article
	for rows.Next() {
		var a model.Article
		if err := rows.Scan(&a.ID, &a.SourceKey, &a.URL, &a.Title, &a.Content,
			&a.Author, &a.Heat, &a.HeatValue, &a.PrevHeat, &a.PrevHeatValue,
			&a.SourceRank, &a.PublishedAt, &a.FetchedAt); err != nil {
			return nil, err
		}
		all = append(all, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 按 prev_heat_value 降序排列,得到"上次"名次 id → prevRank(1-based)
	prevSorted := make([]model.Article, len(all))
	copy(prevSorted, all)
	sort.Slice(prevSorted, func(i, j int) bool {
		return prevSorted[i].PrevHeatValue > prevSorted[j].PrevHeatValue
	})
	prevRankByID := make(map[int64]int, len(prevSorted))
	for i, a := range prevSorted {
		prevRankByID[a.ID] = i + 1
	}

	// 取当前 top N 并附上排名变化
	n := topN
	if len(all) < n {
		n = len(all)
	}
	out := make([]HotlistItem, 0, n)
	for i, a := range all[:n] {
		cur := i + 1
		prev := prevRankByID[a.ID]
		isNew := a.PrevHeatValue == 0 || prev > topN
		rc := 0
		if !isNew {
			rc = prev - cur
		}
		out = append(out, HotlistItem{
			Article:    a,
			Rank:       cur,
			RankChange: rc,
			IsNew:      isNew,
		})
	}
	return out, nil
}

// WindowDelta 一篇文章在 [windowStart, now] 时间窗口内的真实热度增量。
//
// CurrentHeat 来自 articles 表(等同于最新一次抓取的 heat_value)。
// BaselineHeat 是窗口起点之前最近一次的 snapshot 值;窗口起点之前没有 snapshot
// (即文章是在窗口内才首次抓到)时,BaselineHeat = 0,IsNewInWindow = true。
//
// Delta = CurrentHeat - BaselineHeat,可正可负(虽然热度通常单调递增,但快照
// 噪声 + 平台调整热度计算后可能下降)。
type WindowDelta struct {
	ArticleID     int64
	CurrentHeat   int64
	BaselineHeat  int64
	Delta         int64
	IsNewInWindow bool
}

// GetWindowDeltas 给定文章 id 列表和窗口起点,一次 SQL 拿所有文章的
// "窗口内真实热度增量",用于实体页 momentum / score_delta 的精确计算。
//
// windowStart 取零值(time.Time{})表示"全部时间":此时所有文章的 baseline
// 都找不到,Delta 等于 CurrentHeat,IsNewInWindow=true。语义上"全部时间内
// 增长 = 当前热度",合理。
//
// SQL 用 LATERAL 子查询,对每篇文章独立走 (article_id, captured_at DESC)
// 索引,O(N×log) 而非全表扫。
func (r *Repository) GetWindowDeltas(
	ctx context.Context,
	articleIDs []int64,
	windowStart time.Time,
) ([]WindowDelta, error) {
	if len(articleIDs) == 0 {
		return nil, nil
	}

	// windowStart 零值时,把它设成一个非常早的时间,让 baseline 子查询永远找不到。
	// 这样 IsNewInWindow=true、Delta=CurrentHeat,语义对应"全部时间"。
	if windowStart.IsZero() {
		windowStart = time.Unix(0, 0) // 1970-01-01,任何 snapshot 都晚于它
	}

	const q = `
SELECT
    a.id AS article_id,
    a.heat_value AS current_heat,
    COALESCE(baseline.heat_value, 0) AS baseline_heat,
    (a.heat_value - COALESCE(baseline.heat_value, 0)) AS delta,
    (baseline.heat_value IS NULL) AS is_new
FROM articles a
LEFT JOIN LATERAL (
    SELECT heat_value
    FROM article_heat_snapshots
    WHERE article_id = a.id AND captured_at <= $1
    ORDER BY captured_at DESC
    LIMIT 1
) baseline ON TRUE
WHERE a.id = ANY($2::bigint[])
`
	rows, err := r.pool.Query(ctx, q, windowStart, articleIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]WindowDelta, 0, len(articleIDs))
	for rows.Next() {
		var d WindowDelta
		if err := rows.Scan(&d.ArticleID, &d.CurrentHeat, &d.BaselineHeat, &d.Delta, &d.IsNewInWindow); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// === Heat Candidate (热度反馈候选词典) ===

// HeatCandidate 候选词表行。
type HeatCandidate struct {
	ID         int64
	Word       string
	Kind       string // "entity" or "keyword"
	HitDays    int
	TotalHits  int
	PromotedAt *time.Time
}

// UpsertHeatCandidate 插入或更新候选词。
// 如果已存在:
//   - last_hit_at 在 36h 内 → hit_days++, total_hits += hitCount
//   - last_hit_at 超过 36h → hit_days 重置为 1(中断了)
func (r *Repository) UpsertHeatCandidate(ctx context.Context, word, kind string, hitCount int) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO heat_candidates (word, kind, total_hits, hit_days, last_hit_at)
		VALUES ($1, $2, $3, 1, NOW())
		ON CONFLICT (word) DO UPDATE SET
			kind = EXCLUDED.kind,
			total_hits = heat_candidates.total_hits + EXCLUDED.total_hits,
			hit_days = CASE
				WHEN heat_candidates.last_hit_at > NOW() - INTERVAL '36 hours'
				THEN heat_candidates.hit_days + 1
				ELSE 1
			END,
			last_hit_at = NOW()
		WHERE heat_candidates.promoted_at IS NULL
	`, word, kind, hitCount)
	return err
}

// ListAllHeatCandidates 调试用:返回所有候选词(promoted + pending)的快照。
//
// 跟 ListPromotedCandidates 区别:前者只列 promoted_at IS NOT NULL 的,本函数
// 全列,方便观察"为什么 X 还没转正" — 检查 hit_days / total_hits 进度。
func (r *Repository) ListAllHeatCandidates(ctx context.Context, limit int) ([]HeatCandidate, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, word, kind, hit_days, total_hits, promoted_at
		FROM heat_candidates
		ORDER BY total_hits DESC, hit_days DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HeatCandidate
	for rows.Next() {
		var c HeatCandidate
		if err := rows.Scan(&c.ID, &c.Word, &c.Kind, &c.HitDays, &c.TotalHits, &c.PromotedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListPromotedCandidates 返回所有已转正的候选词(启动时加载注入 gse)。
func (r *Repository) ListPromotedCandidates(ctx context.Context) ([]HeatCandidate, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, word, kind, hit_days, total_hits, promoted_at
		FROM heat_candidates
		WHERE promoted_at IS NOT NULL
		ORDER BY promoted_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []HeatCandidate
	for rows.Next() {
		var c HeatCandidate
		if err := rows.Scan(&c.ID, &c.Word, &c.Kind, &c.HitDays, &c.TotalHits, &c.PromotedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PromoteCandidates 将满足阈值的候选词批量转正。
// 条件: hit_days >= minDays AND total_hits >= minHits AND promoted_at IS NULL
//
//	AND word 不在 heat_blacklist(防止删除后被自动复活)。
//
// 返回转正的词列表。
func (r *Repository) PromoteCandidates(ctx context.Context, minDays, minHits int) ([]HeatCandidate, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE heat_candidates
		SET promoted_at = NOW()
		WHERE promoted_at IS NULL
		  AND hit_days >= $1
		  AND total_hits >= $2
		  AND word NOT IN (SELECT word FROM heat_blacklist)
		RETURNING id, word, kind, hit_days, total_hits, promoted_at
	`, minDays, minHits)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []HeatCandidate
	for rows.Next() {
		var c HeatCandidate
		if err := rows.Scan(&c.ID, &c.Word, &c.Kind, &c.HitDays, &c.TotalHits, &c.PromotedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AddHeatBlacklist 将词加入热词黑名单,同时取消转正状态防止重启复活。
func (r *Repository) AddHeatBlacklist(ctx context.Context, word string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO heat_blacklist (word) VALUES ($1)
		ON CONFLICT (word) DO NOTHING
	`, word)
	if err != nil {
		return err
	}
	// 清除 promoted_at,使 ListPromotedCandidates 在重启时不再加载该词
	_, err = r.pool.Exec(ctx, `
		UPDATE heat_candidates SET promoted_at = NULL WHERE word = $1
	`, word)
	return err
}

// ListHeatBlacklist 返回所有黑名单词。
func (r *Repository) ListHeatBlacklist(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT word FROM heat_blacklist ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var w string
		if err := rows.Scan(&w); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
