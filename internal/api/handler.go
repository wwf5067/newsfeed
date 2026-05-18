package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/wwf5067/newsfeed/internal/model"
)

// Handler 聚合所有 HTTP 处理器的依赖。
type Handler struct {
	logger *slog.Logger
	repo   *Repository
}

func NewHandler(logger *slog.Logger, repo *Repository) *Handler {
	return &Handler{logger: logger, repo: repo}
}

func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ListArticles(w http.ResponseWriter, r *http.Request) {
	limit := parseIntDefault(r.URL.Query().Get("limit"), 20)
	offset := parseIntDefault(r.URL.Query().Get("offset"), 0)
	if limit <= 0 || limit > 100 {
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

	articles, err := h.repo.ListArticles(r.Context(), limit, offset, q, source)
	if err != nil {
		h.logger.Error("list articles", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if articles == nil {
		articles = []model.Article{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":  articles,
		"limit":  limit,
		"offset": offset,
		"q":      q,
		"source": source,
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

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
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
