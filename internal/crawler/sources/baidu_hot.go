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
	"strings"
	"time"
	"unicode"

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

	// 收集全部条目:topContent(置顶/广告) + content(正式热榜),按标准化词去重。
	// 标准化:去空白+小写,同时做模糊匹配(编辑距离≤1 / 互为子串)过滤同一热词微小变体。
	var items []baiduHotItem
	seenNorm := make([]string, 0, 64) // 已收录的标准化词列表(用于模糊匹配)
	isDup := func(word string) bool {
		norm := NormalizeBaiduWord(word)
		if norm == "" {
			return true
		}
		for _, s := range seenNorm {
			if IsSimilarWord(norm, s) {
				return true
			}
		}
		seenNorm = append(seenNorm, norm)
		return false
	}
	for _, card := range parsed.Data.Cards {
		for _, it := range card.TopContent {
			if it.Word != "" && !isDup(it.Word) {
				items = append(items, it)
			}
		}
		for _, it := range card.Content {
			if it.Word != "" && !isDup(it.Word) {
				items = append(items, it)
			}
		}
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("baidu hot list empty: %w", crawler.ErrEmptyData)
	}

	// 百度 API 返回的 items 已经按 Index 排好,保险起见再按 Index 排一次,防
	// 极端 API 乱序。Index 是 0-based 官方榜位,转 1-based 写到 SourceRank。
	// PublishedAt 用 now(同一批次共享时间戳),COALESCE 锁定后表示"该热搜
	// 首次被我们抓到的时间"— 对百度热搜没有"问题创建时间",这是最佳近似。
	// 各 tab 按 published_at DESC 排可呈现"最近上榜的在最前",首页 HotPanel
	// 单独按 source_rank ASC 排官方榜位。
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Index < items[j].Index
	})

	articles := make([]model.Article, 0, len(items))
	now := time.Now()
	for i, it := range items {
		score, _ := strconv.ParseInt(it.HotScore, 10, 64)

		// 用标准化后的关键词构造稳定 URL,确保同一热词(不论空格/大小写变体)
		// 多次抓取命中同一行(upsert 去重)。
		// 不直接用 rawUrl 是因为其中可能含时间戳参数导致 URL 每次不同。
		articleURL := "https://www.baidu.com/s?wd=" + url.QueryEscape(NormalizeBaiduWord(it.Word)) + "&sa=top_hot"

		articles = append(articles, model.Article{
			URL:         articleURL,
			Title:       it.Word,
			Content:     it.Desc,
			Heat:        formatBaiduHeat(score),
			HeatValue:   score,
			SourceRank:  i + 1, // 1-based 官方榜位
			PublishedAt: now,
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

// NormalizeBaiduWord 标准化热搜词用于去重 URL 构造:
//  1. 去除所有空白字符(包括全角空格)
//  2. 英文转小写
//
// 目的:百度 API 同一个热词有时带空格有时不带("小米 SU7" vs "小米SU7"),
// 标准化后生成稳定的 URL 去重 key。
func NormalizeBaiduWord(word string) string {
	var b strings.Builder
	b.Grow(len(word))
	for _, r := range word {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// IsSimilarWord 判断两个标准化后的词是否"几乎相同":
//   - 完全相同 → true
//   - 一个是另一个的子串(包含关系) → true
//   - 编辑距离 ≤ 1(只差一个字) → true
//
// 用于批内去重:同一次 API 返回或跨次拉取中,同一热词略有变形的情况。
func IsSimilarWord(a, b string) bool {
	if a == b {
		return true
	}
	// 包含关系(覆盖"杭州楼市新政" vs "杭州楼市新政策"类长串)
	if strings.Contains(a, b) || strings.Contains(b, a) {
		return true
	}
	// 编辑距离 ≤ 1:按 rune 比较
	ra := []rune(a)
	rb := []rune(b)
	la, lb := len(ra), len(rb)
	if abs(la-lb) > 1 {
		return false
	}
	if la == lb {
		// 同长度:最多一个位置不同
		diff := 0
		for i := 0; i < la; i++ {
			if ra[i] != rb[i] {
				diff++
				if diff > 1 {
					return false
				}
			}
		}
		return diff <= 1
	}
	// 长度差 1:检查是否只多/少一个字符
	if la < lb {
		ra, rb = rb, ra
		la, lb = lb, la
	}
	// ra 比 rb 多一个 rune
	diff := 0
	j := 0
	for i := 0; i < la && j < lb; i++ {
		if ra[i] != rb[j] {
			diff++
			if diff > 1 {
				return false
			}
			// 跳过 ra 中多出的那个字符,j 不动
			continue
		}
		j++
	}
	return true
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// 编译期接口断言。
var _ crawler.Source = (*BaiduHot)(nil)
