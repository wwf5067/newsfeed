package sources

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wwf5067/newsfeed/internal/crawler"
	"github.com/wwf5067/newsfeed/internal/model"
)

// 微博热搜页面地址。该页面需要 visitor cookie 但不需要用户登录。
const weiboHotURL = "https://s.weibo.com/top/summary?cate=realtimehot"

// 微博 visitor 系统:自动获取访客身份,无需用户提供 Cookie。
const weiboVisitorAPI = "https://passport.weibo.com/visitor/genvisitor2"

// weiboEntryRegex 匹配热搜条目: <a href="...band_rank=N...">标题</a> + <span>热度</span>
var weiboEntryRegex = regexp.MustCompile(
	`<a\s+href="(/weibo\?q=[^"]*band_rank=(\d+)[^"]*)"[^>]*target="_blank">([^<]+)</a>\s*<span[^>]*>\s*([^<]*)</span>`,
)

// weiboHeatRegex 从 span 内容中提取纯数字部分(可能有"电影""盛典"等前缀标签)
var weiboHeatRegex = regexp.MustCompile(`(\d+)\s*$`)

// WeiboHot 实现 crawler.Source。无需用户 Cookie,通过 visitor 系统自动获取访客身份。
type WeiboHot struct {
	schedule string
	client   *http.Client
}

// NewWeiboHot 构造一个微博热搜源。
func NewWeiboHot(schedule string) *WeiboHot {
	return &WeiboHot{
		schedule: schedule,
		client: &http.Client{
			Timeout: 20 * time.Second,
			// 禁止自动跟随重定向 — 需要手动处理 visitor 流程
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (w *WeiboHot) Key() string      { return "weibo_hot" }
func (w *WeiboHot) Schedule() string { return w.schedule }

func (w *WeiboHot) Fetch(ctx context.Context) ([]model.Article, error) {
	// Step 1: 获取 visitor cookie(无需用户登录)
	visitorCookie, err := w.getVisitorCookie(ctx)
	if err != nil {
		return nil, fmt.Errorf("get visitor cookie: %w", err)
	}

	// Step 2: 带 visitor cookie 请求热搜页面
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, weiboHotURL, nil)
	if err != nil {
		return nil, err
	}
	w.setRandomHeaders(req)
	req.Header.Set("Cookie", visitorCookie)

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently {
		return nil, fmt.Errorf("redirect after visitor cookie: %w", crawler.ErrCookieExpired)
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("HTTP 403: %w", crawler.ErrBanned)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("HTTP 429: %w", crawler.ErrRateLimited)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		snippet := string(body)
		if len(snippet) > 300 {
			snippet = snippet[:300]
		}
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, snippet)
	}

	// Step 3: 解析 HTML 提取热搜条目
	articles := parseWeiboHotHTML(string(body))
	if len(articles) == 0 {
		return nil, fmt.Errorf("no entries parsed: %w", crawler.ErrEmptyData)
	}

	return articles, nil
}

// getVisitorCookie 调用微博 visitor 系统获取访客 SUB/SUBP cookie。
// 该接口对外开放,无需任何认证凭据。
func (w *WeiboHot) getVisitorCookie(ctx context.Context) (string, error) {
	data := url.Values{
		"cb": {"gen_callback"},
		"fp": {""},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, weiboVisitorAPI,
		strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgents[rand.Intn(len(userAgents))])
	req.Header.Set("Referer", "https://s.weibo.com/top/summary")

	resp, err := w.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// 响应格式: window.gen_callback && gen_callback({"retcode":20000000,...,"data":{"sub":"...","subp":"..."}});
	text := string(body)
	sub := extractJSONField(text, "sub")
	subp := extractJSONField(text, "subp")
	if sub == "" {
		return "", fmt.Errorf("visitor response missing sub field: %s", truncate(text, 200))
	}

	cookie := "SUB=" + sub
	if subp != "" {
		cookie += "; SUBP=" + subp
	}
	return cookie, nil
}

// extractJSONField 从 JSONP 响应中提取指定字段值(简单字符串匹配,避免引入 JSON 解析复杂度)。
func extractJSONField(text, field string) string {
	// 查找 "field":"value"
	needle := `"` + field + `":"`
	idx := strings.Index(text, needle)
	if idx < 0 {
		return ""
	}
	start := idx + len(needle)
	end := strings.Index(text[start:], `"`)
	if end < 0 {
		return ""
	}
	return text[start : start+end]
}

// parseWeiboHotHTML 从热搜页面 HTML 中提取条目。
func parseWeiboHotHTML(html string) []model.Article {
	matches := weiboEntryRegex.FindAllStringSubmatch(html, -1)
	articles := make([]model.Article, 0, len(matches))

	for _, m := range matches {
		if len(m) < 5 {
			continue
		}
		href := m[1]    // /weibo?q=...&band_rank=N...
		rank := m[2]    // band_rank 数字
		title := m[3]   // 热搜标题
		heatStr := m[4] // "1947813" 或 "电影 954267" 或空(广告)

		title = strings.TrimSpace(title)
		if title == "" {
			continue
		}

		// 跳过广告条目(href 含 ad 相关标记或无 band_rank)
		if strings.Contains(href, "topic_ad=1") {
			continue
		}

		// 解析热度值
		var heatValue int64
		heatText := strings.TrimSpace(heatStr)
		if hm := weiboHeatRegex.FindStringSubmatch(heatText); len(hm) >= 2 {
			if v, err := strconv.ParseInt(hm[1], 10, 64); err == nil {
				heatValue = v
			}
		}

		// 构造文章 URL
		webURL := "https://s.weibo.com" + href

		// 热度文本:用于前端显示
		var heatDisplay string
		if heatValue > 0 {
			heatDisplay = formatWeiboHeat(heatValue) + " · 排名" + rank
		}

		articles = append(articles, model.Article{
			URL:         webURL,
			Title:       title,
			Heat:        heatDisplay,
			HeatValue:   heatValue,
			PublishedAt: time.Now(),
		})
	}

	return articles
}

// formatWeiboHeat 格式化热度数字为可读文本。
func formatWeiboHeat(v int64) string {
	if v >= 10000 {
		return fmt.Sprintf("%.0f万", float64(v)/10000)
	}
	return strconv.FormatInt(v, 10)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func (w *WeiboHot) setRandomHeaders(req *http.Request) {
	req.Header.Set("User-Agent", userAgents[rand.Intn(len(userAgents))])
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", acceptLanguages[rand.Intn(len(acceptLanguages))])
	req.Header.Set("Referer", "https://s.weibo.com/")
	req.Header.Set("Connection", "keep-alive")
}

// 编译期接口断言。
var _ crawler.Source = (*WeiboHot)(nil)
