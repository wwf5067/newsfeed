// Package sources 聚合所有具体爬虫源的实现。
// 每个源对外暴露一个 New(...) 构造函数,在 cmd/crawler/main.go 中按需注册到 Runner。
package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/wwf5067/newsfeed/internal/crawler"
	"github.com/wwf5067/newsfeed/internal/model"
)

// 知乎热榜接口。该接口未公开但稳定,带登录 Cookie 即可返回完整 JSON。
const zhihuHotAPI = "https://www.zhihu.com/api/v3/feed/topstory/hot-lists/total?limit=50&desktop=true"

// 知乎接口返回的最小字段子集。完整结构含图片、热度、作者等,这里先只取入库需要的部分。
type zhihuHotResp struct {
	Data []struct {
		Target struct {
			ID         json.Number `json:"id"`
			Title      string      `json:"title"`
			URL        string      `json:"url"`         // API 形式: https://api.zhihu.com/questions/123
			Created    int64       `json:"created"`     // 秒级时间戳
			Excerpt    string      `json:"excerpt"`
			AuthorName string      `json:"author_name"`
		} `json:"target"`
		DetailText string `json:"detail_text"` // 形如 "1234 万热度",入库放到 author 字段先占位也行,这里暂不入库
	} `json:"data"`
}

// ZhihuHot 实现 crawler.Source。
type ZhihuHot struct {
	cookie   string
	schedule string
	client   *http.Client
}

// NewZhihuHot 构造一个知乎热榜源。cookie 为浏览器 F12 复制的整段 Cookie 头。
func NewZhihuHot(cookie, schedule string) *ZhihuHot {
	return &ZhihuHot{
		cookie:   cookie,
		schedule: schedule,
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (z *ZhihuHot) Key() string      { return "zhihu_hot" }
func (z *ZhihuHot) Schedule() string { return z.schedule }

func (z *ZhihuHot) Fetch(ctx context.Context) ([]model.Article, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, zhihuHotAPI, nil)
	if err != nil {
		return nil, err
	}

	// 关键 header:必须带 Cookie 和一个像样的 User-Agent,否则会被重定向到登录或返回空 data
	req.Header.Set("Cookie", z.cookie)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Referer", "https://www.zhihu.com/hot")

	resp, err := z.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// 截断 body 防止日志爆炸
		snippet := string(body)
		if len(snippet) > 300 {
			snippet = snippet[:300]
		}
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, snippet)
	}

	var parsed zhihuHotResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	articles := make([]model.Article, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		t := item.Target
		if t.Title == "" {
			continue
		}

		// API 返回的是 api.zhihu.com/questions/<id>,转成网页地址更友好
		webURL := t.URL
		if id := t.ID.String(); id != "" {
			webURL = "https://www.zhihu.com/question/" + id
		}

		published := time.Now()
		if t.Created > 0 {
			published = time.Unix(t.Created, 0)
		}

		articles = append(articles, model.Article{
			URL:         webURL,
			Title:       t.Title,
			Content:     t.Excerpt,
			Author:      t.AuthorName,
			PublishedAt: published,
		})
	}

	return articles, nil
}

// 编译期接口断言,防止未来误改签名。
var _ crawler.Source = (*ZhihuHot)(nil)
