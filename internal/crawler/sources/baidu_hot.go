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
	"strconv"
	"time"

	"github.com/wwf5067/newsfeed/internal/crawler"
	"github.com/wwf5067/newsfeed/internal/model"
)

// 百度热搜实时榜 JSON 接口。无需登录,直接返回 JSON。
const baiduHotAPI = "https://top.baidu.com/api/board?tab=realtime&platform=pc&version=1"

// 百度接口返回的最小字段子集。
type baiduHotResp struct {
	Success bool `json:"success"`
	Error   struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Data struct {
		Cards []struct {
			UpdateTime string         `json:"updateTime"`
			Content    []baiduHotItem `json:"content"`
			TopContent []baiduHotItem `json:"topContent"`
		} `json:"cards"`
	} `json:"data"`
}

type baiduHotItem struct {
	Index    int    `json:"index"`    // 0-based 排名
	Word     string `json:"word"`     // 热搜词
	HotScore string `json:"hotScore"` // 热度指数,数字字符串如 "7809483"
	Desc     string `json:"desc"`     // 摘要,热词解释(常为空)
	RawURL   string `json:"rawUrl"`   // 对应百度搜索结果页
}

// BaiduHot 实现 crawler.Source。
type BaiduHot struct {
	schedule string
	client   *http.Client
}

// NewBaiduHot 构造一个百度热搜源。无需凭据。
func NewBaiduHot(schedule string) *BaiduHot {
	return &BaiduHot{
		schedule: schedule,
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (b *BaiduHot) Key() string      { return "baidu_hot" }
func (b *BaiduHot) Schedule() string { return b.schedule }

func (b *BaiduHot) Fetch(ctx context.Context) ([]model.Article, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baiduHotAPI, nil)
	if err != nil {
		return nil, err
	}
	// 复用公共 UA / Accept-Language 池,降低同源指纹。
	req.Header.Set("User-Agent", userAgents[rand.Intn(len(userAgents))])
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", acceptLanguages[rand.Intn(len(acceptLanguages))])
	req.Header.Set("Referer", "https://top.baidu.com/board?tab=realtime")

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

	var parsed baiduHotResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	if !parsed.Success || parsed.Error.Code != 0 {
		return nil, fmt.Errorf("baidu api error %d: %s", parsed.Error.Code, parsed.Error.Message)
	}

	// 收集全部条目:topContent(置顶/广告) + content(正式热榜),按 word 去重。
	// 理论上只有一个 card,多 card 情况做兜底合并。
	var items []baiduHotItem
	seen := make(map[string]bool)
	for _, card := range parsed.Data.Cards {
		for _, it := range card.TopContent {
			if it.Word != "" && !seen[it.Word] {
				items = append(items, it)
				seen[it.Word] = true
			}
		}
		for _, it := range card.Content {
			if it.Word != "" && !seen[it.Word] {
				items = append(items, it)
				seen[it.Word] = true
			}
		}
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("baidu hot list empty: %w", crawler.ErrEmptyData)
	}

	// 先按 hot_score 倒序排,确保下面用 rank 偏移给 PublishedAt 时,热度高的在前。
	// 排序的是 items 切片本身,不影响下游,但保证后面循环按热度顺序构造 article。
	sort.SliceStable(items, func(i, j int) bool {
		si, _ := strconv.ParseInt(items[i].HotScore, 10, 64)
		sj, _ := strconv.ParseInt(items[j].HotScore, 10, 64)
		return si > sj
	})

	articles := make([]model.Article, 0, len(items))
	now := time.Now()
	for rank, it := range items {
		score, _ := strconv.ParseInt(it.HotScore, 10, 64)

		// 用关键词构造稳定 URL,确保同一热词多次抓取命中同一行(upsert 去重)。
		// 不直接用 rawUrl 是因为其中可能含时间戳参数导致 URL 每次不同。
		articleURL := "https://www.baidu.com/s?wd=" + url.QueryEscape(it.Word) + "&sa=top_hot"

		// PublishedAt 按热度倒序赋值:rank 0(最热)= now,rank 1 = now-1s,以此类推。
		// 配合 crawler/repository.go 的 published_at COALESCE 锁定语义,首次入库时间
		// 永久保留,前端按 published_at DESC 排序就等价于按热度 DESC。
		// 1 秒间隔够稀疏,避免同源同批次时间冲突;且总跨度 ~50 秒以内,不影响"最近"
		// 时间过滤。
		articles = append(articles, model.Article{
			URL:         articleURL,
			Title:       it.Word,
			Content:     it.Desc,
			Heat:        formatBaiduHeat(score),
			HeatValue:   score,
			PublishedAt: now.Add(-time.Duration(rank) * time.Second),
		})
	}
	return articles, nil
}

// formatBaiduHeat 把百度热搜指数格式化为中文短文本,风格与其它源保持一致。
//
//	7809483   -> "781 万热搜"
//	234567890 -> "2.3 亿热搜"
//	500       -> "500 热搜"
func formatBaiduHeat(score int64) string {
	switch {
	case score >= 1_0000_0000:
		v := float64(score) / 1e8
		if v >= 10 {
			return fmt.Sprintf("%.0f 亿热搜", v)
		}
		return fmt.Sprintf("%.1f 亿热搜", v)
	case score >= 1_0000:
		return fmt.Sprintf("%d 万热搜", score/1_0000)
	default:
		return fmt.Sprintf("%d 热搜", score)
	}
}

// 编译期接口断言。
var _ crawler.Source = (*BaiduHot)(nil)
