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

// 公开"热门视频"接口,无需登录。
// ps=分页大小(最大 50);pn=页码(从 1 起)。一次取 50 条已足够首页展示。
const bilibiliPopularAPI = "https://api.bilibili.com/x/web-interface/popular?ps=50&pn=1"

// B 站接口返回(只取我们需要的字段子集)。
type bilibiliPopularResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		List []struct {
			Bvid    string `json:"bvid"`
			Title   string `json:"title"`
			Desc    string `json:"desc"`
			Pubdate int64  `json:"pubdate"`
			Owner   struct {
				Name string `json:"name"`
			} `json:"owner"`
			Stat struct {
				View int64 `json:"view"`
			} `json:"stat"`
		} `json:"list"`
	} `json:"data"`
}

// Bilibili 实现 crawler.Source。
type Bilibili struct {
	schedule string
	client   *http.Client
}

// NewBilibili 构造一个 B 站热门源。无需 cookie。
func NewBilibili(schedule string) *Bilibili {
	return &Bilibili{
		schedule: schedule,
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (b *Bilibili) Key() string      { return "bilibili_popular" }
func (b *Bilibili) Schedule() string { return b.schedule }

func (b *Bilibili) Fetch(ctx context.Context) ([]model.Article, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, bilibiliPopularAPI, nil)
	if err != nil {
		return nil, err
	}
	// 复用 zhihu_hot 的 UA / 语言池,降低同源指纹。Referer 用 b 站首页。
	req.Header.Set("User-Agent", userAgents[rand.Intn(len(userAgents))])
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", acceptLanguages[rand.Intn(len(acceptLanguages))])
	req.Header.Set("Referer", "https://www.bilibili.com/")

	resp, err := b.client.Do(req)
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

	var parsed bilibiliPopularResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	// B 站业务码:0 表示成功;-412 是常见的风控提示(频率/特征异常)。
	switch parsed.Code {
	case 0:
		// fallthrough
	case -412:
		return nil, fmt.Errorf("bilibili code -412 %q: %w", parsed.Message, crawler.ErrRateLimited)
	default:
		return nil, fmt.Errorf("bilibili code %d: %s", parsed.Code, parsed.Message)
	}

	if len(parsed.Data.List) == 0 {
		return nil, fmt.Errorf("bilibili list empty: %w", crawler.ErrEmptyData)
	}

	articles := make([]model.Article, 0, len(parsed.Data.List))
	for _, item := range parsed.Data.List {
		if item.Title == "" || item.Bvid == "" {
			continue
		}
		published := time.Now()
		if item.Pubdate > 0 {
			published = time.Unix(item.Pubdate, 0)
		}
		articles = append(articles, model.Article{
			URL:         "https://www.bilibili.com/video/" + item.Bvid,
			Title:       item.Title,
			Content:     item.Desc,
			Author:      item.Owner.Name,
			Heat:        formatViewHeat(item.Stat.View),
			HeatValue:   item.Stat.View, // 用播放量做趋势比较
			PublishedAt: published,
		})
	}
	return articles, nil
}

// formatViewHeat 把播放量数值格式化为中文短文本,与知乎"xxx 万热度"风格保持一致。
//
//	834425   -> "83 万播放"
//	1200000000 -> "12 亿播放"
//	500      -> "500 播放"
func formatViewHeat(view int64) string {
	switch {
	case view >= 1_0000_0000:
		// 亿:>= 10 亿用整数,否则保留 1 位小数
		v := float64(view) / 1e8
		if v >= 10 {
			return fmt.Sprintf("%.0f 亿播放", v)
		}
		return fmt.Sprintf("%.1f 亿播放", v)
	case view >= 1_0000:
		return fmt.Sprintf("%d 万播放", view/1_0000)
	default:
		return fmt.Sprintf("%d 播放", view)
	}
}

// 编译期接口断言。
var _ crawler.Source = (*Bilibili)(nil)
