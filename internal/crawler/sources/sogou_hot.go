package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/wwf5067/newsfeed/internal/crawler"
	"github.com/wwf5067/newsfeed/internal/model"
)

// 搜狗热搜 JSON 接口。无需登录,直接返回 JSON。
const sogouHotAPI = "https://go.ie.sogou.com/hot_ranks"

// sogouHotResp 搜狗热搜 API 响应结构。
type sogouHotResp struct {
	Data []struct {
		Type       string `json:"type"`
		ID         string `json:"id"`
		Attributes struct {
			Title string `json:"title"`
			Flag  string `json:"flag"` // "热" | "新" | "荐" | ""
			Num   int64  `json:"num"`  // 热度值
			Rank  int    `json:"rank"` // 1-based 官方榜位
		} `json:"attributes"`
	} `json:"data"`
	Meta struct {
		Timestamp int64 `json:"timestamp"`
	} `json:"meta"`
}

// SogouHot 实现 crawler.Source。无需登录,直接请求公开 JSON 接口。
type SogouHot struct {
	schedule string
	client   *http.Client
}

// NewSogouHot 构造一个搜狗热搜源。
func NewSogouHot(schedule string) *SogouHot {
	return &SogouHot{
		schedule: schedule,
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (s *SogouHot) Key() string      { return "sogou_hot" }
func (s *SogouHot) Schedule() string { return s.schedule }

func (s *SogouHot) Fetch(ctx context.Context) ([]model.Article, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sogouHotAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgents[rand.Intn(len(userAgents))])
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", acceptLanguages[rand.Intn(len(acceptLanguages))])
	req.Header.Set("Referer", "https://ie.sogou.com/top/")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

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

	var parsed sogouHotResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	if len(parsed.Data) == 0 {
		return nil, fmt.Errorf("sogou hot list empty: %w", crawler.ErrEmptyData)
	}

	// API 返回的条目已按 rank 排列,保险起见按 rank 显式排一次,防极端乱序。
	sort.SliceStable(parsed.Data, func(i, j int) bool {
		return parsed.Data[i].Attributes.Rank < parsed.Data[j].Attributes.Rank
	})

	articles := make([]model.Article, 0, len(parsed.Data))
	now := time.Now()

	for i, item := range parsed.Data {
		title := item.Attributes.Title
		if title == "" {
			continue
		}

		// 构造稳定 URL:用标准化标题（去空白+小写）作为搜索关键词,
		// 与百度热搜保持一致 — 确保同一热词空格/大小写变体多次抓取 UPSERT 到同一行。
		articleURL := "https://www.sogou.com/web?query=" + url.QueryEscape(NormalizeBaiduWord(title)) + "&ie=utf8"

		articles = append(articles, model.Article{
			URL:         articleURL,
			Title:       title,
			Heat:        formatSogouHeat(item.Attributes.Num),
			HeatValue:   item.Attributes.Num,
			SourceRank:  i + 1, // 1-based,按排序后的位置,与百度热搜保持一致
			PublishedAt: now,
		})
	}

	if len(articles) == 0 {
		return nil, fmt.Errorf("no valid entries: %w", crawler.ErrEmptyData)
	}

	return articles, nil
}

// formatSogouHeat 格式化搜狗热度值为可读文本,风格与其它源保持一致。
//
//	6059529  → "606万"
//	234567890 → "2.3亿"
//	500       → "500"
func formatSogouHeat(v int64) string {
	switch {
	case v >= 1_0000_0000:
		fv := float64(v) / 1e8
		if fv >= 10 {
			return fmt.Sprintf("%.0f亿", fv)
		}
		return fmt.Sprintf("%.1f亿", fv)
	case v >= 1_0000:
		return fmt.Sprintf("%.0f万", float64(v)/10000)
	default:
		return fmt.Sprintf("%d", v)
	}
}

// 编译期接口断言。
var _ crawler.Source = (*SogouHot)(nil)
