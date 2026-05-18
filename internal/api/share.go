package api

import (
	"errors"
	"html/template"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// /share/{id} 是给微信等社交平台爬虫吃 OG meta 的"分享专用页"。
// 微信抓取 URL 时不执行 JS,所以这里返回纯静态 HTML(由 Go 渲染),
// <head> 完整带上 OG 标签;真实用户 0 秒 meta refresh 跳到前端
// /article/{id} 详情页,体验跟正常详情页一致。
//
// 不放 /api/v1/ 路径下,而是放在顶层 /share/{id},便于后续:
//  1. URL 短便于分享/记忆
//  2. nginx 单独配 location /share/ proxy_pass(配置干净)
//  3. 跟 JSON API 用不同 Content-Type,语义更清晰

const shareTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta http-equiv="refresh" content="0;url=/article/{{.ID}}">
<title>{{.Title}} - Newsfeed</title>
<meta name="description" content="{{.Excerpt}}">
<meta property="og:type" content="article">
<meta property="og:title" content="{{.Title}}">
<meta property="og:description" content="{{.Excerpt}}">
<meta property="og:url" content="{{.ShareURL}}">
<meta name="twitter:card" content="summary">
<meta name="twitter:title" content="{{.Title}}">
<meta name="twitter:description" content="{{.Excerpt}}">
<style>
body{font-family:-apple-system,Segoe UI,sans-serif;max-width:600px;margin:40px auto;padding:0 16px;color:#333;line-height:1.6}
h1{font-size:20px;margin:0 0 12px}
.meta{color:#888;font-size:14px;margin-bottom:16px}
.excerpt{margin-bottom:24px}
.actions a{display:inline-block;margin-right:12px;padding:8px 16px;border-radius:6px;text-decoration:none;font-size:14px}
.primary{background:#18181b;color:#fff}
.secondary{background:#f4f4f5;color:#333}
</style>
</head>
<body>
<h1>{{.Title}}</h1>
<div class="meta">{{.Source}}{{if .Author}} · {{.Author}}{{end}}{{if .Heat}} · {{.Heat}}{{end}}</div>
<div class="excerpt">{{.Excerpt}}</div>
<div class="actions">
<a href="/article/{{.ID}}" class="primary">在 Newsfeed 中查看</a>
<a href="{{.OriginURL}}" class="secondary" target="_blank" rel="noreferrer">访问原文</a>
</div>
</body>
</html>`

const shareNotFoundTemplate = `<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="UTF-8"><title>未找到 - Newsfeed</title>
<style>body{font-family:sans-serif;max-width:600px;margin:80px auto;padding:0 16px;text-align:center;color:#666}</style>
</head><body><h1>未找到该文章</h1><p>它可能已经过期被清理了。<a href="/">返回首页</a></p></body></html>`

// 预编译模板,启动时解析一次。
var (
	shareTpl   = template.Must(template.New("share").Parse(shareTemplate))
	shareNFTpl = template.Must(template.New("notfound").Parse(shareNotFoundTemplate))
)

type shareData struct {
	ID        int64
	Title     string
	Excerpt   string // content 截断到 100 字
	Source    string
	Author    string
	Heat      string
	ShareURL  string
	OriginURL string
}

// excerptOf 取 s 的前 n 个 rune,超长加省略号。
func excerptOf(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func (h *Handler) ShareArticle(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = shareNFTpl.Execute(w, nil)
		return
	}

	a, err := h.repo.GetArticle(r.Context(), id)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if errors.Is(err, pgx.ErrNoRows) {
			w.WriteHeader(http.StatusNotFound)
			_ = shareNFTpl.Execute(w, nil)
			return
		}
		h.logger.Error("share get article", "id", id, "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		_ = shareNFTpl.Execute(w, nil)
		return
	}

	// share URL 用请求的 host + path,反向代理时也能拿到正确的对外地址
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	shareURL := scheme + "://" + r.Host + "/share/" + idStr

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = shareTpl.Execute(w, shareData{
		ID:        a.ID,
		Title:     a.Title,
		Excerpt:   excerptOf(a.Content, 100),
		Source:    a.SourceKey,
		Author:    a.Author,
		Heat:      a.Heat,
		ShareURL:  shareURL,
		OriginURL: a.URL,
	})
}
