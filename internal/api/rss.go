package api

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// RSS 2.0 输出端点。XML 序列化和 JSON 流分文件,避免在 handler.go 里混入 xml/cdata 类型。
//
// 设计要点:
//   - 复用 ListArticles 的 q + source 参数,允许用户在 RSS 阅读器里订阅
//     特定源(如知乎热榜)或特定关键词
//   - description 字段用 CDATA 包热度文本 + content,让阅读器能看到热度信息
//   - guid 用 URL + isPermaLink=true,跟阅读器的去重策略契合
//   - 错误时回纯文本 500,不输出半吊子 XML(避免欺骗阅读器解析失败)

// rssFeed / rssChannel / rssItem 按 RSS 2.0 规范最小子集组织。
type rssFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title         string    `xml:"title"`
	Link          string    `xml:"link"`
	Description   string    `xml:"description"`
	Language      string    `xml:"language,omitempty"`
	LastBuildDate string    `xml:"lastBuildDate,omitempty"`
	Generator     string    `xml:"generator,omitempty"`
	Items         []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string  `xml:"title"`
	Link        string  `xml:"link"`
	Description cdata   `xml:"description"`
	GUID        rssGUID `xml:"guid"`
	PubDate     string  `xml:"pubDate,omitempty"`
	Author      string  `xml:"author,omitempty"`
	Category    string  `xml:"category,omitempty"`
}

type rssGUID struct {
	Value       string `xml:",chardata"`
	IsPermaLink bool   `xml:"isPermaLink,attr"`
}

// cdata 让 description 用 <![CDATA[...]]> 包裹,避免 HTML 实体被双重转义。
type cdata struct {
	Value string `xml:",cdata"`
}

// 源 key → 中文 channel 标题映射。新增源时在此追加一项,默认值是"全站热门"。
var rssSourceTitles = map[string]string{
	"":                 "全站热门",
	"zhihu_hot":        "知乎热榜",
	"bilibili_popular": "B站热门",
}

// FeedRSS 输出 RSS 2.0 feed。复用 ListArticles 的 q + source 过滤参数,
// limit 默认 50(不开放 offset:RSS 阅读器拉的是"最新一页",分页无意义)。
func (h *Handler) FeedRSS(w http.ResponseWriter, r *http.Request) {
	limit := parseIntDefault(r.URL.Query().Get("limit"), 50)
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 100 {
		q = q[:100]
	}
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	if len(source) > 64 {
		source = ""
	}

	articles, err := h.repo.ListArticles(r.Context(), limit, 0, q, source)
	if err != nil {
		h.logger.Error("rss list articles", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// channel 元信息:title 跟随 source,description 在搜索时追加关键词提示。
	sourceLabel, ok := rssSourceTitles[source]
	if !ok {
		sourceLabel = source // 不在映射里时直接用 key,容错
	}
	title := "Newsfeed - " + sourceLabel
	desc := "聚合的实时热门内容"
	if q != "" {
		desc += "(关键词:" + q + ")"
	}

	// link 用请求来的 host,这样 self-link 能正确指向当前部署。
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Host
	siteLink := fmt.Sprintf("%s://%s/", scheme, host)

	items := make([]rssItem, 0, len(articles))
	for _, a := range articles {
		// description 把热度放最前(阅读器显示更直观),空 content 时仅展示热度。
		var descBody strings.Builder
		if a.Heat != "" {
			descBody.WriteString("[")
			descBody.WriteString(a.Heat)
			descBody.WriteString("] ")
		}
		descBody.WriteString(a.Content)

		items = append(items, rssItem{
			Title:       a.Title,
			Link:        a.URL,
			Description: cdata{Value: descBody.String()},
			GUID:        rssGUID{Value: a.URL, IsPermaLink: true},
			PubDate:     a.PublishedAt.Format(time.RFC1123Z),
			Author:      a.Author,
			Category:    a.SourceKey,
		})
	}

	feed := rssFeed{
		Version: "2.0",
		Channel: rssChannel{
			Title:         title,
			Link:          siteLink,
			Description:   desc,
			Language:      "zh-cn",
			LastBuildDate: time.Now().Format(time.RFC1123Z),
			Generator:     "newsfeed",
			Items:         items,
		},
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(xml.Header)); err != nil {
		return
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(feed); err != nil {
		// header 已发出,无法再改 status;只能记日志。
		h.logger.Error("rss encode", "err", err)
	}
}
