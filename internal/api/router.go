package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter 装配路由和中间件。
func NewRouter(h *Handler) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(15 * time.Second))

	r.Get("/healthz", h.Health)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/articles", h.ListArticles)
		r.Get("/announcements", h.ListAnnouncements)
	})

	return r
}
