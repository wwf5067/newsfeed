// Package sources 聚合所有具体爬虫源的实现。
// 每个源对外暴露一个 New(...) 构造函数,在 cmd/crawler/main.go 中按需注册到 Runner。
package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"

	"github.com/wwf5067/newsfeed/internal/crawler"
	"github.com/wwf5067/newsfeed/internal/model"
)

// 知乎热榜接口。该接口未公开但稳定,带登录 Cookie 即可返回完整 JSON。
const zhihuHotAPI = "https://www.zhihu.com/api/v3/feed/topstory/hot-lists/total?limit=50&desktop=true"

// UA 池:主流浏览器 + 操作系统组合,每次请求随机选取。
var userAgents = []string{
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36 Edg/124.0.0.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:125.0) Gecko/20100101 Firefox/125.0",
}

// Referer 池:模拟用户从不同页面进入热榜。
var referers = []string{
	"https://www.zhihu.com/hot",
	"https://www.zhihu.com/",
	"https://www.zhihu.com/hot",
	"https://www.zhihu.com/explore",
}

// Accept-Language 池
var acceptLanguages = []string{
	"zh-CN,zh;q=0.9,en;q=0.8",
	"zh-CN,zh;q=0.9",
	"zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7",
	"zh-CN,zh;q=0.8,zh-TW;q=0.7,zh-HK;q=0.5,en;q=0.3",
}

// 知乎接口返回的最小字段子集。完整结构含图片、热度、作者等,这里先只取入库需要的部分。
type zhihuHotResp struct {
	Data []struct {
		Target struct {
			ID         json.Number `json:"id"`
			Title      string      `json:"title"`
			URL        string      `json:"url"`     // API 形式: https://api.zhihu.com/questions/123
			Created    int64       `json:"created"` // 秒级时间戳
			Excerpt    string      `json:"excerpt"`
			AuthorName string      `json:"author_name"`
		} `json:"target"`
		DetailText string `json:"detail_text"` // 形如 "1234 万热度"
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
			// 禁止自动跟随重定向——知乎 Cookie 失效时会 302 到登录页,
			// 我们需要捕获这个信号而不是跟随过去拿到一个 HTML 登录页。
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (z *ZhihuHot) Key() string      { return "zhihu_hot" }
func (z *ZhihuHot) Schedule() string { return z.schedule }

// FetchRaw 发起一次请求并返回原始响应 body。封装了通用的 ban 信号检测,
// 供调试工具(如 cmd/zhihu-probe)直接观察未解析的 JSON。
func (z *ZhihuHot) FetchRaw(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, zhihuHotAPI, nil)
	if err != nil {
		return nil, err
	}

	// 设置随机化的请求头,降低指纹识别概率
	z.setRandomHeaders(req)

	resp, err := z.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	// ---- Ban 信号分级检测 ----

	// 302/301 重定向 → Cookie 过期,被踢到登录页
	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently {
		location := resp.Header.Get("Location")
		return nil, fmt.Errorf("redirect to %q: %w", location, crawler.ErrCookieExpired)
	}

	// 403 → 被封禁(IP 或 Cookie)
	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("HTTP 403: %w", crawler.ErrBanned)
	}

	// 429 → 请求频率过高
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("HTTP 429: %w", crawler.ErrRateLimited)
	}

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

	return body, nil
}

func (z *ZhihuHot) Fetch(ctx context.Context) ([]model.Article, error) {
	body, err := z.FetchRaw(ctx)
	if err != nil {
		return nil, err
	}

	var parsed zhihuHotResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	// 200 但 data 为空 → 可能被静默封禁
	if len(parsed.Data) == 0 {
		return nil, fmt.Errorf("API returned 200 but data is empty: %w", crawler.ErrEmptyData)
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
			Heat:        item.DetailText,
			PublishedAt: published,
		})
	}

	return articles, nil
}

// setRandomHeaders 为请求设置随机化的 HTTP header,模拟真实浏览器行为。
func (z *ZhihuHot) setRandomHeaders(req *http.Request) {
	req.Header.Set("Cookie", z.cookie)
	req.Header.Set("User-Agent", userAgents[rand.Intn(len(userAgents))])
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", acceptLanguages[rand.Intn(len(acceptLanguages))])
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Referer", referers[rand.Intn(len(referers))])

	// 知乎前端常见的额外 header
	req.Header.Set("x-requested-with", "fetch")

	// 偶尔带上 sec-fetch 系列 header(~70% 概率),模拟现代浏览器
	if rand.Float64() < 0.7 {
		req.Header.Set("Sec-Fetch-Dest", "empty")
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	}

	// 偶尔带 DNT header(~30% 概率)
	if rand.Float64() < 0.3 {
		req.Header.Set("DNT", "1")
	}

	// Connection: keep-alive(大多数浏览器默认行为)
	if rand.Float64() < 0.8 {
		req.Header.Set("Connection", "keep-alive")
	}
}

// 编译期接口断言,防止未来误改签名。
var _ crawler.Source = (*ZhihuHot)(nil)
