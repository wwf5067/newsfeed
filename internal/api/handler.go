package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/wwf5067/newsfeed/internal/model"
	"github.com/wwf5067/newsfeed/internal/subscribe"
)

// Handler 聚合所有 HTTP 处理器的依赖。
type Handler struct {
	logger        *slog.Logger
	repo          *Repository
	subscribeRepo *subscribe.Repository // 可选;nil 时订阅 API 返回 503
	notifyTo      string                // 用于在 list 响应里提示用户邮件发往哪里
}

// NewHandler 默认构造,不带订阅功能。
func NewHandler(logger *slog.Logger, repo *Repository) *Handler {
	return &Handler{logger: logger, repo: repo}
}

// WithSubscribe 注入订阅依赖。返回 *Handler 自身便于链式调用。
// notifyTo 是 .env 里的 DIGEST_TO,前端展示用(会在响应里做模糊处理)。
func (h *Handler) WithSubscribe(repo *subscribe.Repository, notifyTo string) *Handler {
	h.subscribeRepo = repo
	h.notifyTo = notifyTo
	return h
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

	articles, err := h.repo.ListRecentArticles(r.Context(), window*2, 500)
	if err != nil {
		h.logger.Error("list trackers", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	items := buildTrackerTopics(articles, time.Now(), window, limit)
	writeJSON(w, http.StatusOK, trackerResp{
		Window: trackerWindow{Hours: window},
		Items:  items,
	})
}

func (h *Handler) GetTrackerStoryline(w http.ResponseWriter, r *http.Request) {
	term := strings.TrimSpace(r.URL.Query().Get("term"))
	if term == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "term required"})
		return
	}
	if len([]rune(term)) > 32 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "term too long"})
		return
	}

	window := parseIntDefault(r.URL.Query().Get("window"), 24)
	if window <= 0 || window > 168 {
		window = 24
	}

	articles, err := h.repo.ListRecentArticles(r.Context(), window, 200)
	if err != nil {
		h.logger.Error("tracker storyline", "term", term, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusOK, buildTrackerStoryline(term, articles, window))
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
func decodeJSON(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 64*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
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
