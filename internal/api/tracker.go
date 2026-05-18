package api

import (
	"regexp"
	"sort"
	"strconv"
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

type trackerStorylineResp struct {
	Term          string              `json:"term"`
	Window        trackerWindow       `json:"window"`
	Summary       []string            `json:"summary"`
	Sources       []trackerSourceStat `json:"sources"`
	Items         []trackerArticleRef `json:"items"`
	Momentum      string              `json:"momentum"`
	ScoreDelta    int64               `json:"score_delta"`
	TotalArticles int                 `json:"total_articles"`
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
	// trackerTokenRegex 匹配候选 token:
	//   1. 中英混合词(如 "iPhone16""AI大模型""ChatGPT概念股"):至少含一个汉字或一个字母,2~16 字符
	//   2. 纯英文/数字标识(如 "#React""OpenAI"):1~31 字符
	//   3. 纯中文词:2~12 个汉字(覆盖"国家市场监督管理总局"等长实体)
	trackerTokenRegex = regexp.MustCompile(
		`[#A-Za-z0-9][#A-Za-z0-9+._-]{1,31}` + // 纯英文/数字/符号标识
			`|[\p{Han}A-Za-z0-9][\p{Han}A-Za-z0-9·.]{1,15}` + // 中英混合词(2~16 字符)
			`|[\p{Han}]{2,12}`, // 纯中文词(2~12 字)
	)

	// stopTokens 分层停用词:通用虚词 + 疑问代词 + 平台噪音 + 时间/数量 + 单位
	stopTokens = map[string]struct{}{
		// —— 通用虚词/代词 ——
		"这个": {}, "那个": {}, "一个": {}, "一次": {}, "一些": {}, "一种": {},
		"我们": {}, "你们": {}, "他们": {}, "大家": {}, "自己": {}, "别人": {},
		"什么": {}, "哪些": {}, "怎么": {}, "如何": {}, "为什么": {}, "为何": {},
		"是否": {}, "能否": {}, "可以": {}, "可能": {}, "应该": {}, "需要": {},
		"已经": {}, "正在": {}, "一直": {}, "终于": {}, "到底": {}, "竟然": {},
		"居然": {}, "其实": {}, "原来": {}, "只是": {}, "而已": {}, "之后": {},
		"之前": {}, "目前": {}, "当前": {}, "现在": {}, "以后": {}, "以前": {},
		"表示": {}, "认为": {}, "发现": {}, "出现": {}, "进行": {}, "开始": {},
		"继续": {}, "成为": {}, "属于": {}, "引发": {}, "导致": {}, "造成": {},
		"相关": {}, "有关": {}, "关于": {}, "对于": {}, "通过": {}, "根据": {},
		// —— 平台/媒体噪音 ——
		"知乎": {}, "热榜": {}, "热门": {}, "视频": {}, "话题": {}, "网友": {},
		"官方": {}, "回应": {}, "发布": {}, "新闻": {}, "记者": {}, "记者称": {},
		"万热度": {}, "万播放": {}, "播放": {}, "热度": {}, "全文": {}, "全文如下": {},
		"直播": {}, "网友称": {}, "详情": {}, "原文": {}, "账号": {}, "博主": {},
		"评论区": {}, "最新消息": {}, "哔哩哔哩": {}, "bilibili": {}, "B站": {},
		"关注": {}, "收藏": {}, "转发": {}, "评论": {}, "回答": {}, "问题": {},
		"曝光": {}, "爆料": {}, "独家": {}, "突发": {}, "重磅": {}, "实锤": {},
		// —— 时间/数量/程度 ——
		"今天": {}, "今日": {}, "昨天": {}, "昨日": {}, "明天": {}, "明日": {},
		"最新": {}, "最近": {}, "最大": {}, "最小": {}, "最高": {}, "最低": {},
		"非常": {}, "十分": {}, "极其": {}, "特别": {}, "真的": {}, "确实": {},
		"内容": {}, "方面": {}, "情况": {}, "事情": {}, "东西": {}, "地方": {},
	}

	// entitySuffixes 实体后缀词:带这些后缀的词大概率是命名实体。
	entitySuffixes = []string{
		"公司", "集团", "大学", "医院", "银行", "汽车", "平台", "手机", "芯片", "模型",
		"赛事", "政府", "委员会", "研究院", "实验室", "科技", "电子", "网络", "基金",
		"联盟", "协会", "机构", "中心", "工厂", "品牌", "航空", "铁路", "地铁",
	}

	// entityPrefixes 实体前缀词:以这些开头的词大概率是命名实体(地名/机构)。
	entityPrefixes = []string{
		"中国", "美国", "日本", "韩国", "俄罗斯", "北京", "上海", "深圳", "广州",
		"国家", "中央", "全国",
	}
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

	// 去重合并:如果 topic A 的 label 是 topic B 的子串,且 A 的 Score 不超过 B 的 2 倍,
	// 则 A 被 B "吸收"——只保留更长/更具体的 B,避免"普京""普京访华"同时出现。
	items = deduplicateTrackerTopics(items)

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
	token = strings.Trim(token, "#.,!?:;\uff0c\u3002\uff01\uff1f\uff1a\uff1b\uff08\uff09()[]\u3010\u3011\u300a\u300b\\\"'\u201c\u201d\u2018\u2019\u00b7-")
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
	// 过滤纯数字 token(年份、数量等无意义数字噪音)
	if isAllDigits(token) {
		return ""
	}
	return token
}

// isAllDigits 判断字符串是否全由 ASCII 数字组成。
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func looksLikeEntity(token string) bool {
	// 1. 带 # 前缀的话题标签
	if strings.HasPrefix(token, "#") {
		return true
	}
	// 2. 含大写字母的英文/混合词(如 "ChatGPT""iPhone""OpenAI")
	for _, r := range token {
		if r >= 'A' && r <= 'Z' {
			return true
		}
	}
	// 3. 带实体后缀的(如 "华为公司""北京大学")
	for _, suffix := range entitySuffixes {
		if strings.HasSuffix(token, suffix) {
			return true
		}
	}
	// 4. 带实体前缀的(如 "中国移动""美国总统")
	for _, prefix := range entityPrefixes {
		if strings.HasPrefix(token, prefix) && utf8.RuneCountInString(token) > utf8.RuneCountInString(prefix) {
			return true
		}
	}
	// 5. 较长的中文词(>=4 字)更可能是专有名词而非普通动词/形容词
	if utf8.RuneCountInString(token) >= 4 {
		return true
	}
	return false
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

func buildTrackerStoryline(term string, articles []model.Article, windowHours int) trackerStorylineResp {
	filtered := filterArticlesByTerm(term, articles)
	items := make([]trackerArticleRef, 0, len(filtered))
	sources := map[string]int{}
	var scoreDelta int64
	for _, article := range filtered {
		items = append(items, trackerArticleRef{
			ID:        article.ID,
			Title:     article.Title,
			SourceKey: article.SourceKey,
			Heat:      article.Heat,
			HeatValue: article.HeatValue,
		})
		sources[article.SourceKey]++
		scoreDelta += article.HeatValue - article.PrevHeatValue
	}

	summary := buildTrackerSummary(term, filtered, windowHours)
	momentum := "flat"
	if len(filtered) >= 2 {
		first := filtered[len(filtered)-1]
		last := filtered[0]
		momentum = detectMomentum(last.HeatValue-first.HeatValue, len(filtered)-1)
	}

	return trackerStorylineResp{
		Term:          term,
		Window:        trackerWindow{Hours: windowHours},
		Summary:       summary,
		Sources:       flattenTrackerSources(sources),
		Items:         items,
		Momentum:      momentum,
		ScoreDelta:    scoreDelta,
		TotalArticles: len(items),
	}
}

func filterArticlesByTerm(term string, articles []model.Article) []model.Article {
	term = strings.TrimSpace(strings.ToLower(term))
	if term == "" {
		return nil
	}

	type scored struct {
		article model.Article
		weight  int // title 命中权重 3,content 命中权重 1
	}

	var matches []scored
	for _, article := range articles {
		title := strings.ToLower(article.Title)
		content := strings.ToLower(article.Content)
		w := 0
		if strings.Contains(title, term) {
			w += 3
		}
		if strings.Contains(content, term) {
			w += 1
		}
		if w > 0 {
			matches = append(matches, scored{article: article, weight: w})
		}
	}

	// 排序:先按权重降序,再按发布时间降序,最后按热度降序
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].weight != matches[j].weight {
			return matches[i].weight > matches[j].weight
		}
		if !matches[i].article.PublishedAt.Equal(matches[j].article.PublishedAt) {
			return matches[i].article.PublishedAt.After(matches[j].article.PublishedAt)
		}
		return matches[i].article.HeatValue > matches[j].article.HeatValue
	})

	if len(matches) > 20 {
		matches = matches[:20]
	}
	out := make([]model.Article, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.article)
	}
	return out
}

func buildTrackerSummary(term string, articles []model.Article, windowHours int) []string {
	if len(articles) == 0 {
		return []string{"当前窗口内还没有足够的关于「" + term + "」的相关文章。"}
	}

	bullets := []string{}
	latest := articles[0]
	bullets = append(bullets,
		"最近 "+strconv.Itoa(windowHours)+" 小时内出现 "+strconv.Itoa(len(articles))+" 条关于「"+term+"」的相关文章，最新进展是《"+latest.Title+"》。")

	sourceCounts := map[string]int{}
	for _, article := range articles {
		sourceCounts[article.SourceKey]++
	}
	sourceStats := flattenTrackerSources(sourceCounts)
	if len(sourceStats) > 1 {
		bullets = append(bullets,
			"讨论已扩散到 "+strconv.Itoa(len(sourceStats))+" 个内容源，主来源是 "+sourceStats[0].SourceKey+"。")
	}

	hottest := latest
	for _, article := range articles[1:] {
		if article.HeatValue > hottest.HeatValue {
			hottest = article
		}
	}
	if hottest.HeatValue > 0 {
		bullets = append(bullets,
			"当前热度最高的相关内容是《"+hottest.Title+"》，热度约 "+hottest.Heat+"。")
	}

	if len(bullets) > 3 {
		bullets = bullets[:3]
	}
	return bullets
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// deduplicateTrackerTopics 去除话题列表中的子串重叠:
// 如果短 topic A 的 label 是长 topic B 的子串(如"普京"⊂"普京访华"),
// 且 A.Score <= B.Score*2,则将 A 标记为"被吸收",从结果中移除。
// 保留更长/更具体的话题,避免列表出现大量重叠条目。
//
// 输入已按权重排序,输出保持原序。复杂度 O(n²),n 通常 <50,可接受。
func deduplicateTrackerTopics(items []trackerTopic) []trackerTopic {
	if len(items) <= 1 {
		return items
	}

	absorbed := make([]bool, len(items))
	for i := range items {
		if absorbed[i] {
			continue
		}
		for j := range items {
			if i == j || absorbed[j] {
				continue
			}
			li := []rune(items[i].Label)
			lj := []rune(items[j].Label)

			// 只在长度严格不等时做子串判定(等长不互相吸收)
			if len(li) == len(lj) {
				continue
			}

			// 短的可能是长的子串
			short, long := i, j
			if len(li) > len(lj) {
				short, long = j, i
			}
			if !strings.Contains(items[long].Label, items[short].Label) {
				continue
			}
			// 短的被长的吸收条件:短的 score 不超过长的 2 倍
			// (如果短的 score 远高于长的,说明短词本身就是独立热点,不应被吸收)
			if items[short].Score <= items[long].Score*2 {
				absorbed[short] = true
			}
		}
	}

	out := make([]trackerTopic, 0, len(items))
	for i, item := range items {
		if !absorbed[i] {
			out = append(out, item)
		}
	}
	return out
}
