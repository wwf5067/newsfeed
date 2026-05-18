package api

import (
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wwf5067/newsfeed/internal/model"
)

type trackerWindow struct {
	Hours int `json:"hours"`
}

type trackerSourceStat struct {
	SourceKey string `json:"source_key"`
	Count     int    `json:"count"`
}

type trackerArticleRef struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	SourceKey string `json:"source_key"`
	Heat      string `json:"heat"`
	HeatValue int64  `json:"heat_value"`
}

type trackerTopic struct {
	Label         string              `json:"label"`
	Kind          string              `json:"kind"`
	Score         int64               `json:"score"`
	PrevScore     int64               `json:"prev_score"`
	ScoreDelta    int64               `json:"score_delta"`
	Count         int                 `json:"count"`
	PrevCount     int                 `json:"prev_count"`
	CountDelta    int                 `json:"count_delta"`
	Momentum      string              `json:"momentum"`
	Sources       []trackerSourceStat `json:"sources"`
	RelatedTerms  []string            `json:"related_terms"`
	SampleArticle *trackerArticleRef  `json:"sample_article,omitempty"`
}

type trackerResp struct {
	Window trackerWindow  `json:"window"`
	Items  []trackerTopic `json:"items"`
}

type trackerAccumulator struct {
	Label         string
	Kind          string
	Score         int64
	Count         int
	Sources       map[string]int
	RelatedTerms  map[string]struct{}
	SampleArticle *trackerArticleRef
}

var (
	trackerTokenRegex = regexp.MustCompile(`[#A-Za-z0-9][#A-Za-z0-9+._-]{1,31}|[\p{Han}]{2,8}`)
	stopTokens        = map[string]struct{}{
		"知乎": {}, "热榜": {}, "热门": {}, "视频": {}, "话题": {}, "网友": {},
		"官方": {}, "今天": {}, "今日": {}, "最新": {}, "回应": {}, "发布": {},
		"表示": {}, "怎么": {}, "为什么": {}, "如何": {}, "这个": {}, "那个": {},
		"我们": {}, "你们": {}, "他们": {}, "已经": {}, "正在": {}, "一个": {},
		"一次": {}, "相关": {}, "内容": {}, "新闻": {}, "记者": {}, "记者称": {},
		"万热度": {}, "万播放": {}, "播放": {}, "热度": {}, "全文": {}, "全文如下": {},
		"直播": {}, "网友称": {}, "详情": {}, "原文": {}, "账号": {}, "博主": {},
		"评论区": {}, "最新消息": {}, "哔哩哔哩": {}, "bilibili": {}, "B站": {},
	}
	entitySuffixes = []string{"公司", "集团", "大学", "医院", "银行", "汽车", "平台", "手机", "芯片", "模型", "赛事"}
)

func buildTrackerTopics(articles []model.Article, now time.Time, windowHours, limit int) []trackerTopic {
	if windowHours <= 0 || windowHours > 168 {
		windowHours = 24
	}
	window := time.Duration(windowHours) * time.Hour
	recentCutoff := now.Add(-window)
	prevCutoff := now.Add(-2 * window)

	recentAccs := map[string]*trackerAccumulator{}
	prevAccs := map[string]*trackerAccumulator{}
	recentSeen := make(map[int64]map[string]struct{}, len(articles))
	prevSeen := make(map[int64]map[string]struct{}, len(articles))

	for _, article := range articles {
		switch {
		case !article.FetchedAt.Before(recentCutoff):
			accumulateTrackerTopics(recentAccs, recentSeen, article)
		case !article.FetchedAt.Before(prevCutoff):
			accumulateTrackerTopics(prevAccs, prevSeen, article)
		}
	}

	items := make([]trackerTopic, 0, len(recentAccs))
	for key, acc := range recentAccs {
		if acc.Count < 2 {
			continue
		}
		if acc.Score == 0 {
			continue
		}
		prev := prevAccs[key]
		prevScore := int64(0)
		prevCount := 0
		if prev != nil {
			prevScore = prev.Score
			prevCount = prev.Count
		}
		scoreDelta := acc.Score - prevScore
		countDelta := acc.Count - prevCount
		items = append(items, trackerTopic{
			Label:         acc.Label,
			Kind:          acc.Kind,
			Score:         acc.Score,
			PrevScore:     prevScore,
			ScoreDelta:    scoreDelta,
			Count:         acc.Count,
			PrevCount:     prevCount,
			CountDelta:    countDelta,
			Momentum:      detectMomentum(scoreDelta, countDelta),
			Sources:       flattenTrackerSources(acc.Sources),
			RelatedTerms:  flattenTrackerTerms(acc.RelatedTerms, 4),
			SampleArticle: acc.SampleArticle,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Momentum != items[j].Momentum {
			return trackerMomentumRank(items[i].Momentum) < trackerMomentumRank(items[j].Momentum)
		}
		if items[i].ScoreDelta != items[j].ScoreDelta {
			return items[i].ScoreDelta > items[j].ScoreDelta
		}
		if items[i].CountDelta != items[j].CountDelta {
			return items[i].CountDelta > items[j].CountDelta
		}
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return items[i].Label < items[j].Label
	})

	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func accumulateTrackerTopics(
	accs map[string]*trackerAccumulator,
	articleSeen map[int64]map[string]struct{},
	article model.Article,
) {
	candidates := extractTrackerCandidates(article)
	if len(candidates) == 0 {
		return
	}
	seen := articleSeen[article.ID]
	if seen == nil {
		seen = map[string]struct{}{}
		articleSeen[article.ID] = seen
	}

	for _, c := range candidates {
		key := c.Kind + ":" + c.Label
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		acc := accs[key]
		if acc == nil {
			acc = &trackerAccumulator{
				Label:        c.Label,
				Kind:         c.Kind,
				Sources:      map[string]int{},
				RelatedTerms: map[string]struct{}{},
			}
			accs[key] = acc
		}

		acc.Count++
		acc.Score += scoreArticle(article)
		acc.Sources[article.SourceKey]++
		for _, related := range c.RelatedTerms {
			if related != acc.Label {
				acc.RelatedTerms[related] = struct{}{}
			}
		}
		if acc.SampleArticle == nil || article.HeatValue > acc.SampleArticle.HeatValue {
			acc.SampleArticle = &trackerArticleRef{
				ID:        article.ID,
				Title:     article.Title,
				SourceKey: article.SourceKey,
				Heat:      article.Heat,
				HeatValue: article.HeatValue,
			}
		}
	}
}

type trackerCandidate struct {
	Label        string
	Kind         string
	RelatedTerms []string
}

func extractTrackerCandidates(article model.Article) []trackerCandidate {
	raw := trackerTokenRegex.FindAllString(article.Title+" "+article.Content, -1)
	if len(raw) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	out := make([]trackerCandidate, 0, len(raw))
	for _, token := range raw {
		normalized := normalizeTrackerToken(token)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		kind := "keyword"
		if looksLikeEntity(normalized) {
			kind = "entity"
		}
		related := []string{}
		if kind == "entity" {
			related = collectTrackerRelatedTerms(raw, normalized)
		}
		out = append(out, trackerCandidate{Label: normalized, Kind: kind, RelatedTerms: related})
	}

	return out
}

func normalizeTrackerToken(token string) string {
	token = strings.TrimSpace(token)
	token = strings.Trim(token, "#.,!?:;，。！？：；（）()[]【】《》\"'“”‘’·-")
	token = strings.TrimPrefix(token, "#")
	token = strings.TrimSuffix(token, "#")
	if token == "" {
		return ""
	}
	if _, blocked := stopTokens[token]; blocked {
		return ""
	}
	runeCount := utf8.RuneCountInString(token)
	if runeCount < 2 || runeCount > 16 {
		return ""
	}
	if strings.HasSuffix(token, "热度") || strings.HasSuffix(token, "播放") {
		return ""
	}
	if strings.Contains(token, "http") {
		return ""
	}
	return token
}

func looksLikeEntity(token string) bool {
	if strings.HasPrefix(token, "#") {
		return true
	}
	for _, suffix := range entitySuffixes {
		if strings.HasSuffix(token, suffix) {
			return true
		}
	}
	for _, r := range token {
		if r >= 'A' && r <= 'Z' {
			return true
		}
	}
	return utf8.RuneCountInString(token) >= 3
}

func collectTrackerRelatedTerms(tokens []string, label string) []string {
	terms := make([]string, 0, 4)
	seen := map[string]struct{}{}
	for _, token := range tokens {
		normalized := normalizeTrackerToken(token)
		if normalized == "" || normalized == label {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		terms = append(terms, normalized)
		if len(terms) == 4 {
			break
		}
	}
	return terms
}

func scoreArticle(article model.Article) int64 {
	if article.HeatValue > 0 {
		return article.HeatValue + maxInt64(article.HeatValue-article.PrevHeatValue, 0)
	}
	return 10_000
}

func detectMomentum(scoreDelta int64, countDelta int) string {
	if scoreDelta > 0 || countDelta > 0 {
		return "up"
	}
	if scoreDelta < 0 || countDelta < 0 {
		return "down"
	}
	return "flat"
}

func trackerMomentumRank(momentum string) int {
	switch momentum {
	case "up":
		return 0
	case "flat":
		return 1
	default:
		return 2
	}
}

func flattenTrackerSources(in map[string]int) []trackerSourceStat {
	out := make([]trackerSourceStat, 0, len(in))
	for source, count := range in {
		out = append(out, trackerSourceStat{SourceKey: source, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].SourceKey < out[j].SourceKey
	})
	return out
}

func flattenTrackerTerms(in map[string]struct{}, limit int) []string {
	out := make([]string, 0, len(in))
	for term := range in {
		out = append(out, term)
	}
	sort.Strings(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
