// Package digest 每日精选邮件 job:从 articles 表挑 top10 热门发到指定邮箱。
//
// 设计:
//   - 跨包引 internal/api 的只读 Repository,避免在 crawler 包重写一份 SELECT
//   - SMTP 发送复用 internal/mailer 共享实现(QQ/163 SMTPS 都支持)
//   - 失败优雅降级:logger.Error 记日志,绝不 panic、不阻塞下一次 cron
package digest

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"sort"
	"time"

	"github.com/wwf5067/newsfeed/internal/api"
	"github.com/wwf5067/newsfeed/internal/mailer"
	"github.com/wwf5067/newsfeed/internal/model"
)

// SMTPConfig SMTP 凭据集合,从 config 包传进来。
// 保留 SiteURL 字段(邮件正文里拼绝对链接用),因此不能直接用 mailer.Config。
type SMTPConfig struct {
	Host    string
	Port    int
	User    string
	Pass    string
	From    string
	To      string
	SiteURL string
}

// Digest 每日精选邮件发送器。
type Digest struct {
	logger *slog.Logger
	repo   *api.Repository
	smtp   SMTPConfig
}

func New(logger *slog.Logger, repo *api.Repository, cfg SMTPConfig) *Digest {
	return &Digest{
		logger: logger.With("job", "digest"),
		repo:   repo,
		smtp:   cfg,
	}
}

// Run 拉最近 50 条 → 按 heat_value 降序取 top10 → 渲染邮件 → 发出。
func (d *Digest) Run(ctx context.Context) {
	d.logger.Info("digest run start")
	start := time.Now()

	// 拉一个较大窗口(50 条)再排序,保证两个源都有机会进 top10
	articles, err := d.repo.ListArticles(ctx, 50, 0, "", "")
	if err != nil {
		d.logger.Error("list articles", "err", err)
		return
	}
	if len(articles) == 0 {
		d.logger.Warn("no articles to digest, skip")
		return
	}

	// 按 heat_value 降序;0 值排最后(避免没有热度的条目挤掉真热门)
	sort.SliceStable(articles, func(i, j int) bool {
		return articles[i].HeatValue > articles[j].HeatValue
	})
	if len(articles) > 10 {
		articles = articles[:10]
	}

	body, err := renderHTML(articles, d.smtp.SiteURL)
	if err != nil {
		d.logger.Error("render html", "err", err)
		return
	}

	subject := fmt.Sprintf("Newsfeed 每日精选 - %s", time.Now().Format("2006-01-02"))
	if err := mailer.Send(mailer.Config{
		Host: d.smtp.Host, Port: d.smtp.Port,
		User: d.smtp.User, Pass: d.smtp.Pass,
		From: d.smtp.From, To: d.smtp.To,
	}, subject, body); err != nil {
		d.logger.Error("send mail", "err", err)
		return
	}

	d.logger.Info("digest run done", "items", len(articles), "elapsed", time.Since(start))
}

// 邮件 HTML 模板:简洁列表 + 链接到 SITE_URL/article/{id}
const digestTpl = `<!DOCTYPE html>
<html><head><meta charset="UTF-8"><title>Newsfeed 每日精选</title></head>
<body style="font-family:-apple-system,Segoe UI,sans-serif;max-width:640px;margin:0 auto;padding:24px;color:#333;line-height:1.6">
<h2 style="border-bottom:2px solid #eee;padding-bottom:8px;margin-top:0">Newsfeed 每日精选</h2>
<p style="color:#888;font-size:14px">{{.Date}} · 共 {{len .Items}} 条</p>
<ol style="padding-left:24px">
{{range .Items}}
<li style="margin-bottom:16px">
  <a href="{{$.SiteURL}}/article?id={{.ID}}" style="font-size:16px;color:#18181b;text-decoration:none;font-weight:500">{{.Title}}</a>
  <div style="color:#888;font-size:13px;margin-top:4px">
    {{.SourceKey}}{{if .Heat}} · {{.Heat}}{{end}}{{if .Author}} · {{.Author}}{{end}}
  </div>
  {{if .Content}}<div style="color:#555;font-size:14px;margin-top:6px">{{.Content}}</div>{{end}}
</li>
{{end}}
</ol>
<p style="color:#aaa;font-size:12px;border-top:1px solid #eee;padding-top:12px;margin-top:24px">
  <a href="{{.SiteURL}}" style="color:#888">访问 Newsfeed</a> · 自动生成于 {{.Date}}
</p>
</body></html>`

var digestTemplate = template.Must(template.New("digest").Parse(digestTpl))

type digestData struct {
	Date    string
	Items   []model.Article
	SiteURL string
}

func renderHTML(items []model.Article, siteURL string) (string, error) {
	// 截断每条 content 防邮件过长
	for i := range items {
		items[i].Content = excerptOf(items[i].Content, 80)
	}
	var buf bytes.Buffer
	err := digestTemplate.Execute(&buf, digestData{
		Date:    time.Now().Format("2006-01-02"),
		Items:   items,
		SiteURL: siteURL,
	})
	return buf.String(), err
}

func excerptOf(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
