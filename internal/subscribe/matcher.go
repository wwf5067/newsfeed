package subscribe

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"sort"
	"time"

	"github.com/wwf5067/newsfeed/internal/mailer"
)

// Matcher 在一轮抓取后处理"新文章 → 命中关键词 → 发邮件"的整链路。
// 失败任何一步只记日志,不返回错误,不阻塞抓取主流程。
type Matcher struct {
	logger  *slog.Logger
	repo    *Repository
	smtp    mailer.Config
	siteURL string
}

func New(logger *slog.Logger, repo *Repository, smtp mailer.Config, siteURL string) *Matcher {
	return &Matcher{
		logger:  logger.With("job", "subscribe_match"),
		repo:    repo,
		smtp:    smtp,
		siteURL: siteURL,
	}
}

// HandleNewArticles 一轮抓取后调用。articleIDs 是本轮"首次入库"(isNew=true) 的 id。
// 已存在文章的 heat 更新不会触发,避免反复打扰。
//
// 流程:查命中 → 按订阅分组 → 渲染邮件 → 发送 → 写入去重表。
// 邮件发送失败 → 不写去重表,下次抓取还会重试。
func (m *Matcher) HandleNewArticles(ctx context.Context, articleIDs []int64) {
	if len(articleIDs) == 0 {
		return
	}
	hits, err := m.repo.FindHits(ctx, articleIDs)
	if err != nil {
		m.logger.Error("find hits", "err", err)
		return
	}
	if len(hits) == 0 {
		return
	}

	// SMTP 没配齐:直接登记去重,不再发邮件。这样以后即使配上 SMTP 也不会
	// 突然把过去命中过的旧文章批量补发(避免一次"配置变更"炸出几百封邮件)。
	if !m.smtp.Valid() {
		m.logger.Warn("smtp not configured, marking hits as notified without sending",
			"hits", len(hits))
		if err := m.repo.MarkNotified(ctx, hits); err != nil {
			m.logger.Error("mark notified", "err", err)
		}
		return
	}

	body, err := renderMail(hits, m.siteURL)
	if err != nil {
		m.logger.Error("render mail", "err", err)
		return
	}
	subject := fmt.Sprintf("Newsfeed 关键词命中(%d 条) - %s",
		len(hits), time.Now().Format("01-02 15:04"))
	if err := mailer.Send(m.smtp, subject, body); err != nil {
		// 发送失败:不登记去重,下次抓取重试
		m.logger.Error("send mail", "err", err, "hits", len(hits))
		return
	}
	if err := m.repo.MarkNotified(ctx, hits); err != nil {
		// 登记失败的代价是下次重复发一封,可接受
		m.logger.Error("mark notified", "err", err)
		return
	}
	m.logger.Info("notification sent", "hits", len(hits))
}

// 邮件正文模板:按关键词分组,组内列出文章。
const mailTpl = `<!DOCTYPE html>
<html><head><meta charset="UTF-8"><title>Newsfeed 关键词命中</title></head>
<body style="font-family:-apple-system,Segoe UI,sans-serif;max-width:640px;margin:0 auto;padding:24px;color:#333;line-height:1.6">
<h2 style="border-bottom:2px solid #eee;padding-bottom:8px;margin-top:0">Newsfeed 关键词命中</h2>
<p style="color:#888;font-size:14px">{{.Time}} · 共命中 {{.Total}} 条</p>
{{range .Groups}}
<div style="margin-bottom:20px">
  <h3 style="font-size:14px;color:#18181b;margin:16px 0 8px;padding:4px 8px;background:#f4f4f5;border-radius:4px;display:inline-block">
    🔔 {{.Keyword}}
  </h3>
  <ul style="padding-left:20px;margin:4px 0">
    {{range .Articles}}
    <li style="margin-bottom:8px">
      <a href="{{$.SiteURL}}/article?id={{.ArticleID}}" style="color:#18181b;text-decoration:none;font-weight:500">{{.Title}}</a>
      <div style="color:#888;font-size:12px">{{.SourceKey}}{{if .Heat}} · {{.Heat}}{{end}}</div>
    </li>
    {{end}}
  </ul>
</div>
{{end}}
<p style="color:#aaa;font-size:12px;border-top:1px solid #eee;padding-top:12px;margin-top:24px">
  <a href="{{.SiteURL}}" style="color:#888">访问 Newsfeed</a> · 管理订阅请到首页"邮件订阅"
</p>
</body></html>`

var mailTemplate = template.Must(template.New("subscribe").Parse(mailTpl))

type mailGroup struct {
	Keyword  string
	Articles []Hit
}

type mailData struct {
	Time    string
	Total   int
	Groups  []mailGroup
	SiteURL string
}

// renderMail 把 hits 按 keyword 聚合后渲染成 HTML。
func renderMail(hits []Hit, siteURL string) (string, error) {
	// 同一关键词的文章聚成一组。Hit 已按 (subscription_id, article_id) 排序,
	// 同一 subscription 的会连续出现,直接 group-by 即可。
	groupsByKeyword := map[string]*mailGroup{}
	var keywords []string
	for _, h := range hits {
		g, ok := groupsByKeyword[h.Keyword]
		if !ok {
			g = &mailGroup{Keyword: h.Keyword}
			groupsByKeyword[h.Keyword] = g
			keywords = append(keywords, h.Keyword)
		}
		g.Articles = append(g.Articles, h)
	}
	sort.Strings(keywords) // 稳定输出顺序
	groups := make([]mailGroup, 0, len(keywords))
	for _, k := range keywords {
		groups = append(groups, *groupsByKeyword[k])
	}

	var buf bytes.Buffer
	err := mailTemplate.Execute(&buf, mailData{
		Time:    time.Now().Format("2006-01-02 15:04"),
		Total:   len(hits),
		Groups:  groups,
		SiteURL: siteURL,
	})
	return buf.String(), err
}
