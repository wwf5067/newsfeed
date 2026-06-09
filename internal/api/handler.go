package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/wwf5067/newsfeed/internal/model"
	"github.com/wwf5067/newsfeed/internal/subscribe"
)

// trackerCacheKey 按 window+limit 两个维度缓存 /trackers 结果。
// 这两个参数完全决定输出,其他请求参数(如 source)暂无,故作为完整 key 使用。
type trackerCacheKey struct{ window, limit int }

type trackerCacheEntry struct {
	resp     trackerResp
	cachedAt time.Time
}

// trackerCacheTTL 缓存有效期。爬虫最快 15 分钟一次,60s 既能大幅降低 pipeline 重算频率,
// 也保证新数据在 1 分钟内可见。
const trackerCacheTTL = 60 * time.Second

// Handler 聚合所有 HTTP 处理器的依赖。
type Handler struct {
	logger        *slog.Logger
	repo          *Repository
	subscribeRepo *subscribe.Repository // 可选;nil 时订阅 API 返回 503
	notifyTo      string                // 用于在 list 响应里提示用户邮件发往哪里

	// /trackers 短 TTL 内存缓存:避免每次请求对 500 篇文章跑完整 10 步 pipeline。
	trackerMu    sync.RWMutex
	trackerCache map[trackerCacheKey]trackerCacheEntry

	// pendingHeatWords 跨窗口热词发现累积集合。
	// collectHeatDiscoveredWords 是无状态的,每次请求只能看到当前窗口内的文章,
	// 单窗口可能因文章池太小而发现不了某些词。把历史发现过但尚未转正的词保留在内存,
	// 下次请求的 heatDiscovered 与之合并,让较小窗口也能把历史热词传入 clusterTrackerEvents。
	pendingMu        sync.RWMutex
	pendingHeatWords map[string]struct{}
}

// NewHandler 默认构造,不带订阅功能。
func NewHandler(logger *slog.Logger, repo *Repository) *Handler {
	return &Handler{
		logger:           logger,
		repo:             repo,
		trackerCache:     make(map[trackerCacheKey]trackerCacheEntry),
		pendingHeatWords: make(map[string]struct{}),
	}
}

// WithSubscribe 注入订阅依赖。返回 *Handler 自身便于链式调用。
// notifyTo 是 .env 里的 DIGEST_TO,前端展示用(会在响应里做模糊处理)。
func (h *Handler) WithSubscribe(repo *subscribe.Repository, notifyTo string) *Handler {
	h.subscribeRepo = repo
	h.notifyTo = notifyTo
	return h
}

// LoadPendingHeatWords 从 words 切片初始化 pendingHeatWords。
// 在服务启动时调用(从 DB 加载尚未转正的热词候选),
// 保证重启后跨窗口热词累积状态不丢失。
func (h *Handler) LoadPendingHeatWords(words []string) {
	if len(words) == 0 {
		return
	}
	h.pendingMu.Lock()
	defer h.pendingMu.Unlock()
	for _, w := range words {
		h.pendingHeatWords[w] = struct{}{}
	}
}

func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ListArticles(w http.ResponseWriter, r *http.Request) {
	limit := parseIntDefault(r.URL.Query().Get("limit"), 20)
	offset := parseIntDefault(r.URL.Query().Get("offset"), 0)
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	// q: 标题/正文模糊匹配;source: 按 source_key 精确筛选(空=全部)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 100 {
		q = q[:100]
	}
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	if len(source) > 64 {
		source = ""
	}

	articles, total, err := h.repo.ListArticlesPage(r.Context(), limit, offset, q, source)
	if err != nil {
		h.logger.Error("list articles", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if articles == nil {
		articles = []model.Article{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":       articles,
		"limit":       limit,
		"offset":      offset,
		"total":       total,
		"has_more":    offset+len(articles) < total,
		"next_offset": offset + len(articles),
		"q":           q,
		"source":      source,
	})
}

// GetArticleByID 按 id 路径参数查单条。未命中 → 404。
func (h *Handler) GetArticleByID(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	a, err := h.repo.GetArticle(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		h.logger.Error("get article", "id", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// GetHeatHistory 返回某文章的热度时序(最近 N 条 snapshot,正序)。
// 前端用于画 sparkline。
func (h *Handler) GetHeatHistory(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	limit := parseIntDefault(r.URL.Query().Get("limit"), 48)
	if limit <= 0 || limit > 500 {
		limit = 48
	}
	points, err := h.repo.GetHeatHistory(r.Context(), id, limit)
	if err != nil {
		h.logger.Error("get heat history", "id", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if points == nil {
		points = []HeatPoint{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": points,
		"limit": limit,
	})
}

// ListAnnouncements 返回当前生效的公告。无分页,公告量不大。
// 响应里若无活跃公告,items 归一为空数组而非 null,简化前端处理。
func (h *Handler) ListAnnouncements(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListActiveAnnouncements(r.Context())
	if err != nil {
		h.logger.Error("list announcements", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if items == nil {
		items = []model.Announcement{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) ListTrackers(w http.ResponseWriter, r *http.Request) {
	window := parseIntDefault(r.URL.Query().Get("window"), 24)
	if window <= 0 || window > 168 {
		window = 24
	}
	limit := parseIntDefault(r.URL.Query().Get("limit"), 12)
	if limit <= 0 || limit > 30 {
		limit = 12
	}

	cacheKey := trackerCacheKey{window: window, limit: limit}

	// 先查缓存(读锁):命中且未过期则直接返回,避免重跑完整 pipeline。
	h.trackerMu.RLock()
	entry, hit := h.trackerCache[cacheKey]
	h.trackerMu.RUnlock()
	if hit && time.Since(entry.cachedAt) < trackerCacheTTL {
		writeJSON(w, http.StatusOK, entry.resp)
		return
	}

	// 拉窗口期内的文章。旧版拉 window*2 是为了 buildTrackerTopics 的 prev 段对比,
	// 现在改用 snapshot 真实增量,不再需要 prev 段,只拉 window 即可。
	// 文章池大小随窗口自适应:72h 事件更分散,需要更大样本覆盖。
	articleLimit := 500
	if window >= 72 {
		articleLimit = 800
	} else if window <= 3 {
		articleLimit = 300
	}
	articles, err := h.repo.ListRecentArticles(r.Context(), window, articleLimit)
	if err != nil {
		h.logger.Error("list trackers", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// 一次 SQL 拿所有文章的"窗口起点 vs 当前"真实热度增量。
	// snapshot 表查询失败不挡主路径:deltas=nil 时 buildTrackerTopics 内部按零值
	// 兜底,momentum 退化到 flat,排序仍走 acc.Score。
	windowStart := time.Now().Add(-time.Duration(window) * time.Hour)
	ids := make([]int64, 0, len(articles))
	for _, a := range articles {
		ids = append(ids, a.ID)
	}
	deltas, derr := h.repo.GetWindowDeltas(r.Context(), ids, windowStart)
	if derr != nil {
		h.logger.Warn("get window deltas failed for /trackers, degrading", "err", derr)
		deltas = nil
	}

	resp := trackerResp{
		Window: trackerWindow{Hours: window},
		Items:  buildTrackerTopics(articles, deltas, window, limit),
	}

	// 事件聚类:把共享实体的文章合并为事件组,提升首页信息密度。
	heatDiscovered := collectHeatDiscoveredWords(articles)
	// 跨窗口合并:把历史窗口发现过但尚未转正的词也带入当前聚类,
	// 避免小窗口(3h/6h)文章池不足导致跨批次热词被遗漏。
	h.pendingMu.RLock()
	for word := range h.pendingHeatWords {
		if !isBlacklisted(word) {
			heatDiscovered[word] = struct{}{}
		}
	}
	h.pendingMu.RUnlock()
	// 构建 deltaByID 供 clusterTrackerEvents 计算事件 momentum。
	deltaByID := make(map[int64]WindowDelta, len(deltas))
	for _, d := range deltas {
		deltaByID[d.ArticleID] = d
	}
	resp.Events = clusterTrackerEvents(articles, heatDiscovered, deltaByID, 8, window)

	// 热度候选词持久化(异步,不阻塞响应):将发现的热词写入 DB,检查转正。
	if len(heatDiscovered) > 0 {
		go h.persistHeatCandidates(heatDiscovered, articles)
	}

	// 写入缓存(写锁)。并发场景下可能有多个请求同时穿透到这里,
	// 最后写入者覆盖前者,结果一致,可接受。
	h.trackerMu.Lock()
	h.trackerCache[cacheKey] = trackerCacheEntry{resp: resp, cachedAt: time.Now()}
	h.trackerMu.Unlock()

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetTrackerStoryline(w http.ResponseWriter, r *http.Request) {
	term := strings.TrimSpace(r.URL.Query().Get("term"))
	if term == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "term required"})
		return
	}
	if len([]rune(term)) > 100 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "term too long"})
		return
	}

	// window=0 表示"全部"(不限时间);其它值 clamp 到 [1, 2160] 范围(2160h = 90 天 retention 上限)。
	rawWindow := parseIntDefault(r.URL.Query().Get("window"), 24)
	window := rawWindow
	if window < 0 {
		window = 24
	}
	if window > 2160 {
		window = 2160
	}

	// 把用户传的 term 通过 lexicon 展开为完整别名集合,SQL 层一次匹配所有别名。
	// "特朗普" → ["特朗普", "Trump", "trump", "川普"]
	// 不在 lexicon 里的 term 退化为 [term],保持原行为可用(自由词搜索)。
	terms := expandTermAliases(term)

	articles, err := h.repo.ListArticlesByTerms(r.Context(), terms, window, 200)
	if err != nil {
		h.logger.Error("tracker storyline", "term", term, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// 过滤仅 content 命中的弱相关文章:如果 title 中完全不包含任何一个别名,
	// 说明关联仅来自摘要偶然提及,降级排除(避免"手机店不卖新机"出现在"中国"实体下)。
	// 保留 title 命中(score>=3) 或 content 命中且 title 也有部分匹配的文章。
	articles = filterWeakContentMatches(articles, terms)

	// 算窗口起点:window=0(全部)时传零值,GetWindowDeltas 会把所有文章算成"窗口内新增"。
	var windowStart time.Time
	if window > 0 {
		windowStart = time.Now().Add(-time.Duration(window) * time.Hour)
	}
	ids := make([]int64, 0, len(articles))
	for _, a := range articles {
		ids = append(ids, a.ID)
	}
	deltas, derr := h.repo.GetWindowDeltas(r.Context(), ids, windowStart)
	if derr != nil {
		// snapshot 查询失败不挡主路径,降级成空 deltas:storyline 仍能返回文章列表,
		// 只是 score_delta=0、momentum=flat、new_count=0,前端 chip 不显示。
		h.logger.Warn("get window deltas failed, degrading to empty", "term", term, "err", derr)
		deltas = nil
	}

	writeJSON(w, http.StatusOK, buildTrackerStoryline(term, articles, deltas, window))
}

// expandTermAliases 把 term 通过 lexicon 展开为所有别名;非 lexicon 词退化为 [term]。
// 用于实体页 SQL 多别名匹配,让"Trump"也能命中"特朗普"那批文章。
func expandTermAliases(term string) []string {
	normalized := normalizeTrackerToken(term)
	if normalized == "" {
		return []string{term}
	}
	canonical := canonicalizeTrackerToken(normalized)
	if canonical != "" {
		if aliases := trackerEntityTermsByLabel[canonical]; len(aliases) > 0 {
			return aliases
		}
	}
	return []string{normalized}
}

func (h *Handler) ListTrackerRelated(w http.ResponseWriter, r *http.Request) {
	term := strings.TrimSpace(r.URL.Query().Get("term"))
	if term == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "term required"})
		return
	}
	if len([]rune(term)) > 100 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "term too long"})
		return
	}

	window := parseIntDefault(r.URL.Query().Get("window"), 24)
	if window <= 0 || window > 168 {
		window = 24
	}

	articles, err := h.repo.ListRecentArticles(r.Context(), window, 300)
	if err != nil {
		h.logger.Error("tracker related", "term", term, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"term":  term,
		"items": buildRelatedTrackers(term, articles, 8),
	})
}

// allowedSurgeWindows 限制窗口枚举,防止任意值导致非预期的 SQL 扫描区间。
var allowedSurgeWindows = map[int]bool{1: true, 6: true, 24: true}

// ListSurging 返回飙升榜:窗口期内热度增量最大的文章。
// 参数:
//
//	source     可选,按 source_key 过滤(空=全部)
//	limit      默认 20,最大 50
//	window     窗口小时数,允许 1/6/24,默认 6
//	min_heat   最小热度门槛,默认 5000(过滤量级噪声)
func (h *Handler) ListSurging(w http.ResponseWriter, r *http.Request) {
	limit := parseIntDefault(r.URL.Query().Get("limit"), 20)
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	window := parseIntDefault(r.URL.Query().Get("window"), 6)
	if !allowedSurgeWindows[window] {
		window = 6
	}
	minHeat := parseIntDefault(r.URL.Query().Get("min_heat"), 5000)
	if minHeat < 0 {
		minHeat = 0
	}
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	if len(source) > 64 {
		source = ""
	}

	items, err := h.repo.ListSurging(r.Context(), source, limit, window, minHeat)
	if err != nil {
		h.logger.Error("list surging", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if items == nil {
		items = []SurgingArticle{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":    items,
		"limit":    limit,
		"window":   window,
		"min_heat": minHeat,
		"source":   source,
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// decodeJSON 限制 body 大小到 64KB,防止异常 payload 撑爆内存。
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// GetArticleKeywords 从文章标题+内容中提取关键词候选,供前端"订阅关键词"和"查看时间线"使用。
// 复用 tracker 模块已有的 extractTrackerCandidates 逻辑,返回按相关性排序的前 5 个关键词。
func (h *Handler) GetArticleKeywords(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	a, err := h.repo.GetArticle(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		h.logger.Error("get article keywords", "id", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	candidates := extractTrackerCandidates(*a, nil)
	// 优先 entity,再 keyword;最多返回 5 个
	keywords := make([]string, 0, 5)
	// 先挑 entity
	for _, c := range candidates {
		if c.Kind == "entity" && !isBlacklisted(c.Label) && len(keywords) < 5 {
			keywords = append(keywords, c.Label)
		}
	}
	// 再补 keyword
	for _, c := range candidates {
		if c.Kind == "keyword" && !isBlacklisted(c.Label) && len(keywords) < 5 {
			// 去重
			dup := false
			for _, k := range keywords {
				if k == c.Label {
					dup = true
					break
				}
			}
			if !dup {
				keywords = append(keywords, c.Label)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"keywords": keywords})
}

// GetHotlist 返回知乎、B站和百度热榜各 top 条,附带排名变化信息。
// 参数:
//
//	top  默认 15,最大 30
func (h *Handler) GetHotlist(w http.ResponseWriter, r *http.Request) {
	top := parseIntDefault(r.URL.Query().Get("top"), 15)
	if top <= 0 || top > 30 {
		top = 15
	}

	type result struct {
		key   string
		items []HotlistItem
		err   error
	}

	sources := []string{"zhihu_hot", "baidu_hot", "weibo_hot", "sogou_hot"}
	resultCh := make(chan result, len(sources))

	for _, src := range sources {
		src := src
		go func() {
			items, err := h.repo.ListHotlistItems(r.Context(), src, top)
			resultCh <- result{key: src, items: items, err: err}
		}()
	}

	results := make(map[string][]HotlistItem, len(sources))
	for range sources {
		res := <-resultCh
		if res.err != nil {
			h.logger.Error("hotlist "+res.key, "err", res.err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		if res.items == nil {
			res.items = []HotlistItem{}
		}
		results[res.key] = res.items
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"zhihu": results["zhihu_hot"],
		"baidu": results["baidu_hot"],
		"weibo": results["weibo_hot"],
		"sogou": results["sogou_hot"],
	})
}

// persistHeatCandidates 异步持久化热度发现词到 DB,并检查是否有新词达到转正阈值。
// 在独立 goroutine 中执行,不阻塞 HTTP 响应。
func (h *Handler) persistHeatCandidates(discovered map[string]struct{}, articles []model.Article) {
	const (
		promoteMinDays = 2 // 连续 2 天出现(个人项目流量较低,降低门槛)
		promoteMinHits = 5 // 累计至少 5 篇文章命中
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 统计每个发现词在当前文章集合中的命中次数。
	// 注意:用 strings.Contains 而非 token 等值比较 — bigram 合并出来的复合词
	// (如"韩红基金""中俄关系")在 gse 单次切词中绝不会作为整体 token 出现,
	// 用 token == word 永远 hit=0,导致 bigram 类候选词总积累不到 5 hits,
	// 永远无法转正。改成子串包含后,只要标题里出现该词就计 1 次。
	for word := range discovered {
		hitCount := 0
		for _, a := range articles {
			if strings.Contains(a.Title, word) {
				hitCount++
			}
		}

		// 推断词性
		kind := inferWordKind(word)

		// 写入/更新 DB
		if err := h.repo.UpsertHeatCandidate(ctx, word, kind, hitCount); err != nil {
			h.logger.Warn("upsert heat candidate failed", "word", word, "err", err)
		}
	}

	// 跨窗口累积:把本次发现的词加入 pendingHeatWords,让后续更小窗口的请求
	// 也能感知到这些词(即使文章池较小、单独统计不过阈值)。
	h.pendingMu.Lock()
	for word := range discovered {
		h.pendingHeatWords[word] = struct{}{}
	}
	h.pendingMu.Unlock()

	// 检查并执行转正
	promoted, err := h.repo.PromoteCandidates(ctx, promoteMinDays, promoteMinHits)
	if err != nil {
		h.logger.Warn("promote heat candidates failed", "err", err)
		return
	}
	if len(promoted) > 0 {
		// 注入运行时词典
		InjectPromotedWords(promoted)
		// 转正后 promotedWordSet 已更新,旧缓存里 is_promoted / promoted_terms 字段已过期,
		// 必须清缓存让下次请求重算,否则首页会在缓存有效期内持续显示蓝底(未入库)。
		// 同时把已转正的词从 pendingHeatWords 移除,避免重复标注为"发现中"。
		h.trackerMu.Lock()
		h.trackerCache = map[trackerCacheKey]trackerCacheEntry{}
		h.trackerMu.Unlock()
		h.pendingMu.Lock()
		for _, c := range promoted {
			delete(h.pendingHeatWords, c.Word)
		}
		h.pendingMu.Unlock()
		for _, c := range promoted {
			h.logger.Info("heat candidate promoted",
				"word", c.Word, "kind", c.Kind,
				"hit_days", c.HitDays, "total_hits", c.TotalHits)
		}
	}
}

func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

// filterWeakContentMatches 过滤超短泛词(≤2汉字)仅靠 content 匹配的弱相关文章。
//
// 背景:像"中国""美国"这种 2 字地名在大量文章 content 里偶然出现,如果仅靠
// content 命中就归入该实体会引入大量噪声。但 3 字以上的词(如"武汉大学""特斯拉")
// 在 content 里出现时大概率是真相关,应保留。
//
// 规则:
//   - title 命中任一 term → 一定保留(强信号)
//   - title 未命中但 content 命中 → 只有当所有 terms 都 ≤ 2 汉字时才过滤;
//     如果 terms 中有 ≥ 3 汉字(或含英文字母)的长词,content 匹配视为有效召回
func filterWeakContentMatches(articles []model.Article, terms []string) []model.Article {
	// 预计算:是否存在长 term(≥3 汉字或含英文字母),如果有则 content 匹配有效
	hasLongTerm := false
	for _, t := range terms {
		if hanRuneCount(t) >= 3 || hasLetterASCII(t) {
			hasLongTerm = true
			break
		}
	}
	// terms 中有长词 → content 匹配可靠,不做过滤
	if hasLongTerm {
		return articles
	}

	// 所有 terms 都是 ≤ 2 汉字的超短泛词(如"中国""美国"),
	// 只保留 title 命中的文章,过滤仅 content 命中的噪声
	out := make([]model.Article, 0, len(articles))
	for _, a := range articles {
		titleLower := strings.ToLower(a.Title)
		matched := false
		for _, t := range terms {
			if strings.Contains(titleLower, strings.ToLower(t)) {
				matched = true
				break
			}
		}
		if matched {
			out = append(out, a)
		}
	}
	return out
}

// hasLetterASCII 检查字符串是否包含 ASCII 字母
func hasLetterASCII(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

// ListHeatWords 调试用:返回当前已转正热词 + 黑名单 + pending 候选词。
// GET /api/v1/trackers/heat-words?include_pending=1
//
// 主要用于 prod 排查"两字新词为何切不出来" — 直接看 promotedWordSet
// 是不是真的注入了用户期待的词。
// include_pending=1 时附带 heat_candidates 表全部行(promoted + pending),
// 用于查"X 词为什么还没转正"(看 hit_days / total_hits 进度)。
func (h *Handler) ListHeatWords(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	promoted, err := h.repo.ListPromotedCandidates(ctx)
	if err != nil {
		h.logger.Error("list promoted", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	blacklist, _ := h.repo.ListHeatBlacklist(ctx)

	type promotedItem struct {
		Word       string `json:"word"`
		Kind       string `json:"kind"`
		HitDays    int    `json:"hit_days"`
		TotalHits  int    `json:"total_hits"`
		PromotedAt string `json:"promoted_at,omitempty"`
		InMemSet   bool   `json:"in_memory_set"` // promotedWordSet 中是否真有
	}
	out := make([]promotedItem, 0, len(promoted))
	for _, p := range promoted {
		ts := ""
		if p.PromotedAt != nil {
			ts = p.PromotedAt.Format(time.RFC3339)
		}
		out = append(out, promotedItem{
			Word: p.Word, Kind: p.Kind, HitDays: p.HitDays,
			TotalHits: p.TotalHits, PromotedAt: ts,
			InMemSet: IsPromotedWord(p.Word),
		})
	}

	resp := map[string]any{
		"promoted":        out,
		"promoted_count":  len(out),
		"blacklist":       blacklist,
		"blacklist_count": len(blacklist),
	}

	// include_pending=1 时附带候选词全表,用于查"X 词为什么还没转正"
	if r.URL.Query().Get("include_pending") == "1" {
		all, err := h.repo.ListAllHeatCandidates(ctx, 200)
		if err == nil {
			type candItem struct {
				Word       string `json:"word"`
				Kind       string `json:"kind"`
				HitDays    int    `json:"hit_days"`
				TotalHits  int    `json:"total_hits"`
				Status     string `json:"status"` // promoted / pending
				PromotedAt string `json:"promoted_at,omitempty"`
			}
			items := make([]candItem, 0, len(all))
			for _, c := range all {
				status := "pending"
				ts := ""
				if c.PromotedAt != nil {
					status = "promoted"
					ts = c.PromotedAt.Format(time.RFC3339)
				}
				items = append(items, candItem{
					Word: c.Word, Kind: c.Kind, HitDays: c.HitDays,
					TotalHits: c.TotalHits, Status: status, PromotedAt: ts,
				})
			}
			resp["candidates_all"] = items
			resp["candidates_total"] = len(items)
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// DeleteHeatWord 删除热词(加入黑名单)。
// DELETE /api/v1/trackers/heat-words/{word}
func (h *Handler) DeleteHeatWord(w http.ResponseWriter, r *http.Request) {
	word := chi.URLParam(r, "word")
	if word == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "word required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.repo.AddHeatBlacklist(ctx, word); err != nil {
		h.logger.Error("add heat blacklist", "word", word, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// 更新内存黑名单
	AddToHeatBlacklist(word)

	// 同步清除运行时词典(立即生效,不等重启)
	RemovePromotedWord(word)

	// 从跨窗口累积集合中移除(避免被再次合入 heatDiscovered)
	h.pendingMu.Lock()
	delete(h.pendingHeatWords, word)
	h.pendingMu.Unlock()

	// 清除 tracker 缓存,下次请求重算
	h.trackerMu.Lock()
	h.trackerCache = map[trackerCacheKey]trackerCacheEntry{}
	h.trackerMu.Unlock()

	h.logger.Info("heat word blacklisted", "word", word)
	w.WriteHeader(http.StatusNoContent)
}

// ListHeatEvalReports 列出最近的热词发现算法评估报告。
// GET /api/v1/heat-eval-reports?limit=20
//
// 用途:运维通过该接口观察"当前算法 vs 候选阈值"的指标趋势,
// 判断是否要手动调整 collectHeatDiscoveredWords 的常数。
// 不暴露给前端用户(无业务价值,纯调试用)。
func (h *Handler) ListHeatEvalReports(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	reports, err := h.repo.ListHeatEvalReports(ctx, limit)
	if err != nil {
		h.logger.Error("list heat eval reports", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"reports": reports,
		"count":   len(reports),
	})
}
