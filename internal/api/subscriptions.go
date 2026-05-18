package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/wwf5067/newsfeed/internal/subscribe"
)

// 关键词长度上限,防止恶意超长字符串。
const maxKeywordLen = 64

// ListSubscriptions GET /api/v1/subscriptions
// 返回当前所有订阅 + notify_to 模糊提示("xxx***@yyy.com")。
func (h *Handler) ListSubscriptions(w http.ResponseWriter, r *http.Request) {
	if h.subscribeRepo == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "subscribe disabled"})
		return
	}
	items, err := h.subscribeRepo.List(r.Context())
	if err != nil {
		h.logger.Error("list subscriptions", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if items == nil {
		items = []subscribe.Subscription{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":     items,
		"notify_to": maskEmail(h.notifyTo),
	})
}

// AddSubscription POST /api/v1/subscriptions  body: {"keyword": "AI"}
// 重复关键词返回 200(幂等),不报错。
func (h *Handler) AddSubscription(w http.ResponseWriter, r *http.Request) {
	if h.subscribeRepo == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "subscribe disabled"})
		return
	}
	var body struct {
		Keyword string `json:"keyword"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	keyword := strings.TrimSpace(body.Keyword)
	if keyword == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "keyword required"})
		return
	}
	if len(keyword) > maxKeywordLen {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "keyword too long"})
		return
	}
	s, err := h.subscribeRepo.Add(r.Context(), keyword)
	if err != nil {
		h.logger.Error("add subscription", "keyword", keyword, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// DeleteSubscription DELETE /api/v1/subscriptions/{id}
// id 不存在也返回 200,前端不需要区分。
func (h *Handler) DeleteSubscription(w http.ResponseWriter, r *http.Request) {
	if h.subscribeRepo == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "subscribe disabled"})
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	if _, err := h.subscribeRepo.Delete(r.Context(), id); err != nil {
		h.logger.Error("delete subscription", "id", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// PreviewSubscription GET /api/v1/subscriptions/preview?keyword=xxx
// 评估关键词在近 7 天文章中的命中量,返回 {count, samples}。仅用于前端订阅前预览,不写库。
// 空 keyword 返回 {count: 0, samples: []},前端不必拦截调用。
func (h *Handler) PreviewSubscription(w http.ResponseWriter, r *http.Request) {
	if h.subscribeRepo == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "subscribe disabled"})
		return
	}
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	if keyword == "" {
		writeJSON(w, http.StatusOK, map[string]any{"count": 0, "samples": []subscribe.PreviewMatch{}})
		return
	}
	if len(keyword) > maxKeywordLen {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "keyword too long"})
		return
	}
	count, samples, err := h.subscribeRepo.PreviewMatches(r.Context(), keyword)
	if err != nil {
		h.logger.Error("preview subscription", "keyword", keyword, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if samples == nil {
		samples = []subscribe.PreviewMatch{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": count, "samples": samples})
}

// maskEmail 把 "user@example.com" 简单遮蔽成 "use***@example.com",
// 前端展示用,告诉用户邮件会发到哪里但不暴露完整地址。
// 空串返回空串(前端据此判断"未配置邮件")。
func maskEmail(s string) string {
	if s == "" {
		return ""
	}
	at := strings.LastIndexByte(s, '@')
	if at <= 1 {
		return s // 不像邮箱,原样返回(让前端原样展示)
	}
	local := s[:at]
	domain := s[at:]
	keep := 3
	if len(local) < keep {
		keep = len(local)
	}
	return local[:keep] + "***" + domain
}