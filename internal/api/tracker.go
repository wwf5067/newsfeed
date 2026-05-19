package api

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
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
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	SourceKey   string    `json:"source_key"`
	Heat        string    `json:"heat"`
	HeatValue   int64     `json:"heat_value"`
	PublishedAt time.Time `json:"published_at"` // 前端按时间分组用
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
	ScoreDelta    int64               `json:"score_delta"` // 窗口内真实热度净增(基于 snapshot,跟 window 对齐)
	NewCount      int                 `json:"new_count"`   // 窗口内新出现的文章数(baseline snapshot 不存在)
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

type trackerCandidate struct {
	Label        string
	Kind         string
	RelatedTerms []string
}

type trackerLexiconEntry struct {
	Label    string
	Aliases  []string
	Category string // 元数据,前端可据此分类筛选(company/person/ip/event/place);空字符串表示未归类
}

type trackerLexiconAlias struct {
	Label            string
	Needle           string
	Lower            string
	RequiresBoundary bool
}

var (
	trackerTokenRegex = regexp.MustCompile(
		`[#A-Za-z0-9][#A-Za-z0-9+._-]{1,31}` +
			`|[\p{Han}A-Za-z0-9][\p{Han}A-Za-z0-9·.]{1,15}` +
			`|[\p{Han}]{2,12}`,
	)
	trackerTitleSplitRegex = regexp.MustCompile(`[|｜/:：,，。！？!?()（）\[\]【】<>《》"“”‘’·]+`)
	stopTokens             = map[string]struct{}{
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
		"知乎": {}, "热榜": {}, "热门": {}, "视频": {}, "话题": {}, "网友": {},
		"官方": {}, "回应": {}, "发布": {}, "新闻": {}, "记者": {}, "记者称": {},
		"万热度": {}, "万播放": {}, "播放": {}, "热度": {}, "全文": {}, "全文如下": {},
		"直播": {}, "网友称": {}, "详情": {}, "原文": {}, "账号": {}, "博主": {},
		"评论区": {}, "最新消息": {}, "哔哩哔哩": {}, "bilibili": {}, "B站": {},
		"关注": {}, "收藏": {}, "转发": {}, "评论": {}, "回答": {}, "问题": {},
		"曝光": {}, "爆料": {}, "独家": {}, "突发": {}, "重磅": {}, "实锤": {},
		"今天": {}, "今日": {}, "昨天": {}, "昨日": {}, "明天": {}, "明日": {},
		"最新": {}, "最近": {}, "最大": {}, "最小": {}, "最高": {}, "最低": {},
		"非常": {}, "十分": {}, "极其": {}, "特别": {}, "真的": {}, "确实": {},
		"内容": {}, "方面": {}, "情况": {}, "事情": {}, "东西": {}, "地方": {},
		"村民": {}, "村庄": {}, "工厂": {}, "公司": {}, "企业": {}, "政府": {},
		"警方": {}, "医院": {}, "学校": {}, "大学": {}, "法院": {}, "检方": {},
		"相关方": {}, "负责": {}, "值得关注": {},
		"警察": {}, "平民": {}, "中国人": {}, "中国游客": {}, "游客": {}, "多人": {},
	}
	entitySuffixes = []string{
		"公司", "集团", "大学", "医院", "银行", "汽车", "平台", "手机", "芯片", "模型",
		"赛事", "政府", "委员会", "研究院", "实验室", "科技", "电子", "网络", "基金",
		"联盟", "协会", "机构", "中心", "工厂", "品牌", "航空", "铁路", "地铁",
	}
	entityPrefixes = []string{
		"中国", "美国", "日本", "韩国", "俄罗斯", "北京", "上海", "深圳", "广州",
		"国家", "中央", "全国",
	}
	// strongGeoNames 高频地名:2字但信息量极高,不应被 weak 过滤。
	// 只收"经常出现在热搜标题里且构成话题核心"的地名,不追求穷举。
	strongGeoNames = map[string]struct{}{
		"武汉": {}, "成都": {}, "重庆": {}, "杭州": {}, "南京": {}, "西安": {},
		"长沙": {}, "天津": {}, "青岛": {}, "厦门": {}, "郑州": {}, "合肥": {},
		"苏州": {}, "东莞": {}, "佛山": {}, "昆明": {}, "贵阳": {}, "福州": {},
		"济南": {}, "沈阳": {}, "大连": {}, "哈尔滨": {},
		"台湾": {}, "香港": {}, "澳门": {}, "台北": {},
		"泰国": {}, "印度": {}, "越南": {}, "菲律宾": {}, "缅甸": {}, "朝鲜": {},
		"英国": {}, "法国": {}, "德国": {}, "乌克兰": {}, "以色列": {}, "伊朗": {},
		"巴西": {}, "澳洲": {}, "加拿大": {},
		"美国": {}, "中国": {}, "日本": {}, "韩国": {}, "俄罗斯": {},
		"欧盟": {}, "巴勒斯坦": {}, "巴基斯坦": {},
	}
	// strongVerbs 高信息量动词:2字但对新闻话题识别至关重要。
	// 这些词出现在标题里时几乎一定是核心事件描述,不应被 weak 过滤。
	strongVerbs = map[string]struct{}{
		"绑架": {}, "勒索": {}, "杀害": {}, "枪杀": {}, "失踪": {}, "逮捕": {},
		"判刑": {}, "起诉": {}, "举报": {}, "维权": {}, "罢工": {}, "暴动": {},
		"患癌": {}, "确诊": {}, "感染": {}, "死亡": {}, "中毒": {}, "猝死": {},
		"裁员": {}, "破产": {}, "暴雷": {}, "跑路": {}, "爆炸": {}, "坍塌": {},
		"坠毁": {}, "翻车": {}, "泄漏": {}, "污染": {}, "造假": {}, "贪腐": {},
		"霸凌": {}, "性侵": {}, "家暴": {}, "虐待": {}, "诈骗": {}, "洗钱": {},
		"降价": {}, "涨价": {}, "崩盘": {}, "熔断": {}, "退市": {}, "暴跌": {},
		"持枪": {},
	}
	topicSuffixes = []string{
		"事件", "计划", "政策", "比赛", "决赛", "演唱会", "电影", "电视剧", "综艺", "事故",
		"台风", "地震", "暴雨", "洪水", "发布会", "裁员", "融资", "停运", "停播", "罢工",
		"选举", "高考", "考研", "春晚", "奥运", "世界杯", "季后赛",
	}
	trackerTrimPrefixes = []string{"关于", "有关", "对于", "因为", "因", "就", "将", "把", "被", "让", "请问", "如何看待", "如何评价", "为什么", "怎么评价", "怎么看", "最新", "突发", "热议", "围观"}
	trackerTrimSuffixes = []string{"怎么回事", "是真的吗", "意味着什么", "说了什么", "最新回应", "回应", "发布", "表示", "来了", "出炉", "曝光", "完整版", "完整版视频", "视频", "全文", "详情", "后续", "什么情况", "哪些信息值得关注", "当前局势如何", "值得关注", "如何防范此类事情"}

	// compoundGeoAbbrevs 2字合称→两个实体的拆解。
	// 热搜标题常用"美以""中美""俄乌"等缩写指代两个国家/地区。
	compoundGeoAbbrevs = map[string][2]string{
		"美以": {"美国", "以色列"},
		"中美": {"中国", "美国"},
		"中日": {"中国", "日本"},
		"中韩": {"中国", "韩国"},
		"中俄": {"中国", "俄罗斯"},
		"俄乌": {"俄罗斯", "乌克兰"},
		"巴以": {"巴勒斯坦", "以色列"},
		"美俄": {"美国", "俄罗斯"},
		"美欧": {"美国", "欧盟"},
		"中欧": {"中国", "欧盟"},
		"朝韩": {"朝鲜", "韩国"},
		"印巴": {"印度", "巴基斯坦"},
		"两岸": {"大陆", "台湾"},
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

func extractTrackerCandidates(article model.Article) []trackerCandidate {
	title := strings.TrimSpace(article.Title)
	if title == "" {
		return nil
	}

	ordered := make([]string, 0, 8)
	poolSeen := map[string]struct{}{}

	// 0. 合称拆解:检测标题中的2字合称(美以/中美/俄乌等),展开为两个独立实体。
	for abbrev, pair := range compoundGeoAbbrevs {
		if strings.Contains(title, abbrev) {
			appendTrackerPool(&ordered, poolSeen, pair[0])
			appendTrackerPool(&ordered, poolSeen, pair[1])
		}
	}

	// 1. 词典扫描:整段标题不区分大小写匹配 lexicon 别名,优先级最高。
	collectTrackerLexiconMatches(title, &ordered, poolSeen)

	// 2. 中文分词:用 gse 把标题切成词序列。
	for _, tok := range segmentTitle(title) {
		normalized := normalizeTrackerToken(tok)
		if normalized == "" {
			continue
		}
		appendTrackerPool(&ordered, poolSeen, normalized)
	}

	// 3. 兜底:按标点切分 + 正则扫描
	segments := trackerTitleSplitRegex.Split(title, -1)
	for _, segment := range segments {
		normalized := normalizeTrackerToken(segment)
		if normalized != "" {
			appendTrackerPool(&ordered, poolSeen, normalized)
		}
		for _, token := range trackerTokenRegex.FindAllString(segment, -1) {
			normalized = normalizeTrackerToken(token)
			if normalized == "" {
				continue
			}
			appendTrackerPool(&ordered, poolSeen, normalized)
		}
	}
	if len(ordered) == 0 {
		for _, token := range trackerTokenRegex.FindAllString(title, -1) {
			normalized := normalizeTrackerToken(token)
			if normalized == "" {
				continue
			}
			appendTrackerPool(&ordered, poolSeen, normalized)
		}
	}
	if len(ordered) == 0 {
		return nil
	}

	out := make([]trackerCandidate, 0, 6)
	for _, label := range ordered {
		if !shouldKeepTrackerToken(label) {
			continue
		}
		kind := "keyword"
		if looksLikeEntity(label) {
			kind = "entity"
		}
		related := []string{}
		if kind == "entity" {
			related = collectTrackerRelatedTerms(ordered, label)
		}
		out = append(out, trackerCandidate{Label: label, Kind: kind, RelatedTerms: related})
		if len(out) >= 8 { // 多取一些,dedup 后保留 6 个
			break
		}
	}

	// 子串去重:如果短 candidate 是另一个更长 candidate 的子串,去掉短的。
	// 保留更完整、信息量更大的版本(如保留"美以袭击伊朗进入第 81 天",去掉"美以袭击伊朗进入第")。
	out = deduplicateCandidateSubstrings(out)
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

// deduplicateCandidateSubstrings 去除候选列表中的子串冗余。
// 如果 A.Label 是 B.Label 的子串且 A 不是 entity,则移除 A。
// entity 类型的短词永远保留(如"伊朗"不会被"美以袭击伊朗..."吸收)。
func deduplicateCandidateSubstrings(candidates []trackerCandidate) []trackerCandidate {
	if len(candidates) <= 1 {
		return candidates
	}
	absorbed := make([]bool, len(candidates))
	for i := range candidates {
		if absorbed[i] || candidates[i].Kind == "entity" {
			continue // entity 不被吸收
		}
		for j := range candidates {
			if i == j || absorbed[j] {
				continue
			}
			// i 是 j 的子串 → 移除 i
			if len(candidates[i].Label) < len(candidates[j].Label) &&
				strings.Contains(candidates[j].Label, candidates[i].Label) {
				absorbed[i] = true
				break
			}
		}
	}
	out := make([]trackerCandidate, 0, len(candidates))
	for i, c := range candidates {
		if !absorbed[i] {
			out = append(out, c)
		}
	}
	return out
}

func appendTrackerPool(pool *[]string, seen map[string]struct{}, label string) {
	label = canonicalizeTrackerToken(label)
	if label == "" {
		return
	}
	if _, ok := seen[label]; ok {
		return
	}
	seen[label] = struct{}{}
	*pool = append(*pool, label)
}

func canonicalizeTrackerToken(token string) string {
	if token == "" {
		return ""
	}
	if label, ok := trackerEntityAliasToLabel[token]; ok {
		return label
	}
	// gse 分词把英文/混合 token 全转小写(openai / gpt-5),走 lower-case 索引兜底
	if label, ok := trackerEntityAliasToLabelLower[strings.ToLower(token)]; ok {
		return label
	}
	return token
}

// normalizeLexiconAlias 给 lexicon 别名索引专用的轻量 normalizer。
// 只 trim 前后空白和包裹符号,不做内容过滤。这样纯数字别名(315/618)、
// 短词(B站)等都能进索引。区别于 normalizeTrackerToken 用于"用户输入或抽出 token
// 的过滤"——那里要过滤纯数字防止"500 万热度"里的"500"被认成实体。
func normalizeLexiconAlias(alias string) string {
	alias = strings.TrimSpace(alias)
	alias = strings.Trim(alias, "#.,!?:;，。！？：；（）()[]【】《》\"'“”‘’·-")
	return strings.TrimSpace(alias)
}

func buildTrackerEntityAliasIndex(entries []trackerLexiconEntry) []trackerLexiconAlias {
	out := make([]trackerLexiconAlias, 0, len(entries)*3)
	seen := map[string]struct{}{}
	for _, entry := range entries {
		// 用轻量 normalizer:lexicon 数据是人工维护的规范形式,
		// 不能走 normalizeTrackerToken 的"纯数字过滤/最小长度"等输入侧规则,
		// 否则像 315/618/B站 这种合法 alias 会被吃掉
		label := normalizeLexiconAlias(entry.Label)
		if label == "" {
			continue
		}
		aliases := append([]string{label}, entry.Aliases...)
		for _, alias := range aliases {
			needle := normalizeLexiconAlias(alias)
			if needle == "" {
				continue
			}
			if strings.ContainsAny(needle, " -") {
				for _, part := range splitTrackerAliasParts(needle) {
					partKey := label + "\x00" + strings.ToLower(part)
					if _, ok := seen[partKey]; ok {
						continue
					}
					seen[partKey] = struct{}{}
					out = append(out, trackerLexiconAlias{
						Label:            label,
						Needle:           part,
						Lower:            strings.ToLower(part),
						RequiresBoundary: trackerAliasRequiresBoundary(part),
					})
				}
			}
			if len(needle) >= 6 && strings.ContainsRune(needle, ' ') {
				compact := strings.ReplaceAll(needle, " ", "")
				compactKey := label + "\x00" + strings.ToLower(compact)
				if compact != "" {
					if _, ok := seen[compactKey]; !ok {
						seen[compactKey] = struct{}{}
						out = append(out, trackerLexiconAlias{
							Label:            label,
							Needle:           compact,
							Lower:            strings.ToLower(compact),
							RequiresBoundary: trackerAliasRequiresBoundary(compact),
						})
					}
				}
			}
			key := label + "\x00" + strings.ToLower(needle)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, trackerLexiconAlias{
				Label:            label,
				Needle:           needle,
				Lower:            strings.ToLower(needle),
				RequiresBoundary: trackerAliasRequiresBoundary(needle),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if utf8.RuneCountInString(out[i].Needle) != utf8.RuneCountInString(out[j].Needle) {
			return utf8.RuneCountInString(out[i].Needle) > utf8.RuneCountInString(out[j].Needle)
		}
		if out[i].Label != out[j].Label {
			return out[i].Label < out[j].Label
		}
		return out[i].Needle < out[j].Needle
	})
	return out
}

func collectTrackerLexiconMatches(title string, pool *[]string, seen map[string]struct{}) {
	lowerTitle := strings.ToLower(title)
	if acMatcher == nil {
		return
	}
	// Aho-Corasick 一次扫描找到所有命中的 alias 索引
	hits := acMatcher.MatchThreadSafe([]byte(lowerTitle))
	for _, idx := range hits {
		if idx < 0 || idx >= len(acPatternLabels) {
			continue
		}
		alias := acPatternPatterns[idx]
		if alias == "" {
			continue
		}
		// 短 alias 需要 boundary check(防止 "o1" 匹配 "go123")
		if acPatternNeedBoundary[idx] {
			if !containsTrackerAliasWithBoundary(lowerTitle, alias) {
				continue
			}
		}
		appendTrackerPool(pool, seen, acPatternLabels[idx])
	}
}

func splitTrackerAliasParts(alias string) []string {
	parts := strings.FieldsFunc(alias, func(r rune) bool {
		return r == ' ' || r == '-'
	})
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = normalizeTrackerToken(part)
		if part == "" || utf8.RuneCountInString(part) < 2 {
			continue
		}
		if trackerAliasRequiresBoundary(part) && utf8.RuneCountInString(part) < 5 {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

func trackerAliasRequiresBoundary(alias string) bool {
	for _, r := range alias {
		if r > unicode.MaxASCII {
			return false
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func containsTrackerAliasWithBoundary(text, alias string) bool {
	if alias == "" {
		return false
	}
	for start := 0; start < len(text); {
		idx := strings.Index(text[start:], alias)
		if idx < 0 {
			return false
		}
		idx += start
		end := idx + len(alias)
		if hasTrackerAliasBoundary(text, idx, end) {
			return true
		}
		start = idx + len(alias)
	}
	return false
}

func hasTrackerAliasBoundary(text string, start, end int) bool {
	if start > 0 {
		prev, _ := utf8.DecodeLastRuneInString(text[:start])
		if isTrackerASCIIWordRune(prev) {
			return false
		}
	}
	if end < len(text) {
		next, _ := utf8.DecodeRuneInString(text[end:])
		if isTrackerASCIIWordRune(next) {
			return false
		}
	}
	return true
}

func isTrackerASCIIWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

func normalizeTrackerToken(token string) string {
	token = strings.TrimSpace(token)
	token = strings.Trim(token, "#.,!?:;，。！？：；（）()[]【】《》\"'“”‘’·-")
	token = strings.TrimPrefix(token, "#")
	token = strings.TrimSuffix(token, "#")
	token = compactTrackerSpaces(token)
	if token == "" {
		return ""
	}

	for changed := true; changed; {
		changed = false
		for _, prefix := range trackerTrimPrefixes {
			if strings.HasPrefix(token, prefix) && utf8.RuneCountInString(token) > utf8.RuneCountInString(prefix)+1 {
				token = strings.TrimSpace(strings.TrimPrefix(token, prefix))
				changed = true
			}
		}
		for _, suffix := range trackerTrimSuffixes {
			if strings.HasSuffix(token, suffix) && utf8.RuneCountInString(token) > utf8.RuneCountInString(suffix)+1 {
				token = strings.TrimSpace(strings.TrimSuffix(token, suffix))
				changed = true
			}
		}
		token = strings.Trim(token, "#.,!?:;，。！？：；（）()[]【】《》\"'“”‘’·-")
	}

	if token == "" {
		return ""
	}
	if _, blocked := stopTokens[token]; blocked {
		return ""
	}
	runeCount := utf8.RuneCountInString(token)
	if runeCount < 2 || runeCount > 20 {
		return ""
	}
	if strings.HasSuffix(token, "热度") || strings.HasSuffix(token, "播放") {
		return ""
	}
	if strings.Contains(strings.ToLower(token), "http") {
		return ""
	}
	if isAllDigits(token) {
		return ""
	}
	if looksLikeNumericMeasure(token) {
		return ""
	}
	if !containsHanOrLetter(token) {
		return ""
	}
	if isWeakChineseFragment(token) {
		return ""
	}
	return token
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func looksLikeNumericMeasure(token string) bool {
	if token == "" {
		return false
	}
	runes := []rune(token)
	if len(runes) < 2 {
		return false
	}
	digits := 0
	for _, r := range runes {
		if unicode.IsDigit(r) {
			digits++
		}
	}
	if digits == 0 {
		return false
	}
	if digits*2 < len(runes) {
		return false
	}
	for _, suffix := range []string{"人", "名", "例", "岁", "年", "天", "家", "次", "%", "万", "亿", "元", "斤", "公里", "小时"} {
		if strings.HasSuffix(token, suffix) {
			return true
		}
	}
	return false
}

func containsHanOrLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func compactTrackerSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func hasUpperASCII(token string) bool {
	for _, r := range token {
		if r >= 'A' && r <= 'Z' {
			return true
		}
	}
	return false
}

func hanRuneCount(token string) int {
	count := 0
	for _, r := range token {
		if unicode.Is(unicode.Han, r) {
			count++
		}
	}
	return count
}

func isWeakChineseFragment(token string) bool {
	if hasUpperASCII(token) || strings.Contains(token, "·") {
		return false
	}
	if looksLikeEntity(token) || looksLikeTopicPhrase(token) {
		return false
	}
	// 强地名和强动词:2字但信息量极高,不视为弱碎片
	if _, ok := strongGeoNames[token]; ok {
		return false
	}
	if _, ok := strongVerbs[token]; ok {
		return false
	}
	return hanRuneCount(token) <= 3
}

func looksLikeEntity(token string) bool {
	if _, ok := trackerEntityLabelSet[token]; ok {
		return true
	}
	if _, ok := strongGeoNames[token]; ok {
		return true
	}
	if isGenericRoleToken(token) {
		return false
	}
	if strings.HasPrefix(token, "#") {
		return true
	}
	if hasUpperASCII(token) {
		return true
	}
	if strings.Contains(token, "·") {
		return true
	}
	for _, suffix := range entitySuffixes {
		if strings.HasSuffix(token, suffix) {
			return true
		}
	}
	for _, prefix := range entityPrefixes {
		if strings.HasPrefix(token, prefix) && utf8.RuneCountInString(token) > utf8.RuneCountInString(prefix) {
			return true
		}
	}
	return false
}

func looksLikeTopicPhrase(token string) bool {
	if token == "" || looksLikeEntity(token) {
		return false
	}
	if _, ok := strongVerbs[token]; ok {
		return true
	}
	for _, suffix := range topicSuffixes {
		if strings.HasSuffix(token, suffix) {
			return true
		}
	}
	hanCount := hanRuneCount(token)
	return hanCount >= 4 && hanCount <= 12
}

func shouldKeepTrackerToken(token string) bool {
	if isGenericRoleToken(token) {
		return false
	}
	if looksLikeEntity(token) {
		return true
	}
	return looksLikeTopicPhrase(token)
}

func isGenericRoleToken(token string) bool {
	_, ok := stopTokens[token]
	return ok
}

func collectTrackerRelatedTerms(tokens []string, label string) []string {
	terms := make([]string, 0, 4)
	seen := map[string]struct{}{}
	for _, token := range tokens {
		token = canonicalizeTrackerToken(token)
		if token == "" || token == label {
			continue
		}
		if !shouldKeepTrackerToken(token) {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		terms = append(terms, token)
		if len(terms) == 4 {
			break
		}
	}
	if len(terms) > 0 {
		return terms
	}
	for _, alias := range trackerEntityTermsByLabel[label] {
		normalized := normalizeTrackerToken(alias)
		if normalized == "" || strings.EqualFold(normalized, label) {
			continue
		}
		if !shouldKeepTrackerToken(normalized) && !trackerAliasRequiresBoundary(strings.ToLower(normalized)) {
			continue
		}
		if canonical := canonicalizeTrackerToken(normalized); canonical != label && canonical != "" {
			continue
		}
		terms = append(terms, normalized)
		if len(terms) == 2 {
			break
		}
	}
	return terms
}

func trackerTermMatchesArticle(term string, article model.Article) bool {
	term = normalizeTrackerToken(term)
	if term == "" {
		return false
	}
	canonical := canonicalizeTrackerToken(term)
	if canonical != "" && canonical != term {
		for _, alias := range trackerEntityTermsByLabel[canonical] {
			if trackerTermMatchesArticle(alias, article) {
				return true
			}
		}
		return false
	}
	title := strings.ToLower(article.Title)
	content := strings.ToLower(article.Content)
	needle := strings.ToLower(term)
	if trackerAliasRequiresBoundary(needle) {
		return containsTrackerAliasWithBoundary(title, needle) || containsTrackerAliasWithBoundary(content, needle)
	}
	return strings.Contains(title, needle) || strings.Contains(content, needle)
}

func scoreTrackerTermMatch(term string, article model.Article) int {
	term = normalizeTrackerToken(term)
	if term == "" {
		return 0
	}
	title := strings.ToLower(article.Title)
	content := strings.ToLower(article.Content)
	needle := strings.ToLower(term)
	weight := 0
	if trackerAliasRequiresBoundary(needle) {
		if containsTrackerAliasWithBoundary(title, needle) {
			weight += 3
		}
		if containsTrackerAliasWithBoundary(content, needle) {
			weight += 1
		}
	} else {
		if strings.Contains(title, needle) {
			weight += 3
		}
		if strings.Contains(content, needle) {
			weight += 1
		}
	}
	return weight
}

func scoreArticle(article model.Article) int64 {
	if article.HeatValue > 0 {
		return article.HeatValue + maxInt64(article.HeatValue-article.PrevHeatValue, 0)
	}
	return 10_000
}

// detectMomentum 严格判断:
// - up:热度有净增 AND 窗口内有新增文章(两条都满足才能判"升温")
// - down:热度净降(不要求 newCount,跌就是跌)
// - flat:其余(纯新增没热度上升,或纯热度上升没新增)
//
// 历史:旧实现用 OR — score_delta>0 || count_delta>0 都判 up,导致几乎不可能 down。
// 配合"acc.Score 是绝对值累加"的 bug,长窗口下永远升温。改 AND 后语义严格,
// 配合 GetWindowDeltas 的真实增量才有意义。
func detectMomentum(scoreDelta int64, newCount int) string {
	if scoreDelta > 0 && newCount > 0 {
		return "up"
	}
	if scoreDelta < 0 {
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

// buildTrackerStoryline 组装实体页响应。
//
// articles 由调用方过滤为"已包含 term"(SQL 直查)。
// deltas 是窗口内真实热度增量(每篇文章 captured_at 之前最近 snapshot 与 current 的差),
// 用于精确算 score_delta / momentum / new_count;调用方负责调 GetWindowDeltas 提供。
//
// deltas == nil 时降级:scoreDelta=0、newCount=0、momentum=flat,前端 chip 不显示。
// 这样 snapshot 表查询失败不会让整个 storyline 接口报错。
func buildTrackerStoryline(
	term string,
	articles []model.Article,
	deltas []WindowDelta,
	windowHours int,
) trackerStorylineResp {
	items := make([]trackerArticleRef, 0, len(articles))
	sources := map[string]int{}
	for _, article := range articles {
		items = append(items, trackerArticleRef{
			ID:          article.ID,
			Title:       article.Title,
			SourceKey:   article.SourceKey,
			Heat:        article.Heat,
			HeatValue:   article.HeatValue,
			PublishedAt: article.PublishedAt,
		})
		sources[article.SourceKey]++
	}

	// 累加窗口内真实热度增量 + 数窗口内新文章数。
	// deltas 跟 articles 是同一个 id 集合(handler 一次性传入),用 map 也行,
	// 这里直接遍历两次累加,N <= 200 性能完全无所谓。
	var scoreDelta int64
	newCount := 0
	for _, d := range deltas {
		scoreDelta += d.Delta
		if d.IsNewInWindow {
			newCount++
		}
	}

	summary := buildTrackerSummary(term, articles, windowHours)
	momentum := detectMomentum(scoreDelta, newCount)

	return trackerStorylineResp{
		Term:          term,
		Window:        trackerWindow{Hours: windowHours},
		Summary:       summary,
		Sources:       flattenTrackerSources(sources),
		Items:         items,
		Momentum:      momentum,
		ScoreDelta:    scoreDelta,
		NewCount:      newCount,
		TotalArticles: len(items),
	}
}

func buildRelatedTrackers(term string, articles []model.Article, limit int) []trackerTopic {
	filtered := filterArticlesByTerm(term, articles)
	if len(filtered) == 0 {
		return nil
	}

	related := buildTrackerTopics(filtered, time.Now(), 24, limit+1)
	out := make([]trackerTopic, 0, len(related))
	needle := strings.ToLower(strings.TrimSpace(term))
	for _, item := range related {
		if strings.ToLower(item.Label) == needle {
			continue
		}
		out = append(out, item)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func filterArticlesByTerm(term string, articles []model.Article) []model.Article {
	term = normalizeTrackerToken(term)
	if term == "" {
		return nil
	}

	canonical := canonicalizeTrackerToken(term)
	terms := []string{term}
	if canonical != "" {
		if aliases := trackerEntityTermsByLabel[canonical]; len(aliases) > 0 {
			terms = aliases
		}
	}

	type scored struct {
		article model.Article
		weight  int
	}

	matches := make([]scored, 0, len(articles))
	for _, article := range articles {
		weight := 0
		for _, candidate := range terms {
			weight += scoreTrackerTermMatch(candidate, article)
		}
		if weight > 0 {
			matches = append(matches, scored{article: article, weight: weight})
		}
	}

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
			if len(li) == len(lj) {
				continue
			}
			short, long := i, j
			if len(li) > len(lj) {
				short, long = j, i
			}
			if !strings.Contains(items[long].Label, items[short].Label) {
				continue
			}
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
