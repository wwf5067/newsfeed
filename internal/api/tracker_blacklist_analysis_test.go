package api

// tracker_blacklist_analysis_test.go — 黑名单缺口分析 & 好词召回测试
//
// 目标:
//  1. TestBlacklistGapAnalysis:  扫描约40个"候选漏网词",打印过滤流水线分析表格
//  2. TestGoodNewWordsRecall:    验证真实新词仍被 collectHeatDiscoveredWords 正确发现
//
// 方法论:
//   - 对每个目标词构造 10 篇假文章 (5 baidu_hot + 5 weibo_hot),
//     保证即使 heatMinArticles 返回最高值 8 也能被满足。
//   - 直接访问包级变量 stopTokens 检测词是否已被过滤。
//   - 调用 collectHeatDiscoveredWords 观察词是否进入发现结果。

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wwf5067/newsfeed/internal/model"
)

// makeBlacklistTestArticles 为目标词构造 10 篇假文章(5 baidu_hot + 5 weibo_hot)。
//
// 使用 10 篇确保 heatMinArticles(word) 的最高阈值(8)也能被满足,
// 让"词是否出现在发现结果中"完全由 isExcludedHeatWord 决定,而非文章数不足。
func makeBlacklistTestArticles(word string, baseID int64) []model.Article {
	now := time.Now()
	type titleAndSource struct {
		title  string
		source string
	}
	entries := []titleAndSource{
		// baidu_hot × 5 ─ 自然新闻场景,目标词处于不同句法位置
		{word + "最新进展引发广泛关注", "baidu_hot"},
		{"警方通报" + word + "案详细经过", "baidu_hot"},
		{word + "事件持续发酵多方回应", "baidu_hot"},
		{"深度调查" + word + "背后原因", "baidu_hot"},
		{"专家解读" + word + "来龙去脉", "baidu_hot"},
		// weibo_hot × 5 ─ 微博热搜短标题风格
		{"热议" + word + "当前状况", "weibo_hot"},
		{word + "到底怎么了全网关注", "weibo_hot"},
		{"突发" + word + "最新官方回应", "weibo_hot"},
		{word + "事件最终结果出炉", "weibo_hot"},
		{"全面梳理" + word + "来龙去脉", "weibo_hot"},
	}
	arts := make([]model.Article, len(entries))
	for i, e := range entries {
		arts[i] = model.Article{
			ID:          baseID + int64(i),
			Title:       e.title,
			SourceKey:   e.source,
			HeatValue:   1_000_000,
			PublishedAt: now,
		}
	}
	return arts
}

// TestBlacklistGapAnalysis 对约40个候选词运行过滤流水线,输出分析表格。
//
// 表格列:
//
//	词 | 分组 | 在stopTokens中 | 进入heat发现 | 结论
//
// 结论说明:
//
//	✅ 已过滤   — 在 stopTokens 中,heat 发现正确拦截
//	❌ 漏网!   — 不在 stopTokens 且进入 heat 发现 → 需加入 stopTokens
//	⚠️ 仅CSS   — 不在 stopTokens 但加入了 clusterSimilarityStopwords(设计如此)
//	ℹ️ 未入heat  — 不在 stopTokens 且未进入 heat 发现(可能另有其他过滤机制)
func TestBlacklistGapAnalysis(t *testing.T) {
	type wordCase struct {
		word  string
		group string
		// cssOnly: 该词被有意只加入 clusterSimilarityStopwords 而不加 stopTokens
		cssOnly bool
	}

	cases := []wordCase{
		// ── 媒体元词 ──────────────────────────────────────────────────────────
		{"热搜", "媒体元词", false},
		{"词条", "媒体元词", false},
		{"消息", "媒体元词", false}, // 在 clusterSimilarityStopwords 但不在 stopTokens
		{"热词", "媒体元词", false},
		{"话题", "媒体元词(基线:已在stop)", false}, // 验证基线
		// ── 人物角色泛称 ───────────────────────────────────────────────────────
		{"当事人", "人物角色", false},
		{"受害者", "人物角色", false},
		{"嫌疑人", "人物角色", false},
		{"遇难者", "人物角色", false},
		{"市民", "人物角色", false},
		{"居民", "人物角色", false},
		{"网友", "人物角色(基线:已在stop)", false}, // 验证基线
		// ── 位置/时间修饰词 ────────────────────────────────────────────────────
		{"当地", "位置修饰词(仅CSS)", true}, // 有意只加入 clusterSimilarityStopwords
		{"当天", "时间修饰词", false},
		{"当日", "时间修饰词", false},
		{"当晚", "时间修饰词", false},
		{"近日", "时间修饰词", false},
		{"今天", "时间修饰词(基线:已在stop)", false}, // 验证基线
		// ── 数量修饰词 ────────────────────────────────────────────────────────
		{"多名", "数量修饰词", false},
		{"一名", "数量修饰词", false},
		{"数名", "数量修饰词", false},
		{"多人", "数量修饰词(基线:已在stop)", false}, // 验证基线
		// ── 事件类型泛称 ───────────────────────────────────────────────────────
		{"案件", "事件类型", false},
		{"事故", "事件类型", false},
		{"纠纷", "事件类型", false},
		{"风波", "事件类型", false},
		// ── 通用基线验证(已在 stopTokens,应全部拦截) ──────────────────────────
		{"相关", "基线(已在stop)", false},
		{"官方", "基线(已在stop)", false},
		{"视频", "基线(已在stop)", false},
		{"今天", "基线(已在stop)", false},
	}

	// 消重 — 上面 "今天" 出现两次,测试去重后结果一致即可
	seen := make(map[string]bool)
	var deduped []wordCase
	for _, c := range cases {
		if !seen[c.word] {
			seen[c.word] = true
			deduped = append(deduped, c)
		}
	}
	cases = deduped

	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║               TestBlacklistGapAnalysis — 黑名单缺口分析报告                        ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════════════════════════╝")
	fmt.Printf("\n%-10s %-26s %-14s %-14s %s\n",
		"词", "分组", "在stopTokens", "进入heat发现", "结论")
	fmt.Println(strings.Repeat("─", 88))

	var gaps []string        // 漏网词(不在 stopTokens 且进入 heat 发现)
	var cssOnlyLeaks []string // 仅在 clusterSimilarityStopwords 的词进入了 heat 发现

	for i, c := range cases {
		_, inStop := stopTokens[c.word]
		arts := makeBlacklistTestArticles(c.word, int64(i*20+5000))
		discovered := collectHeatDiscoveredWords(arts)
		_, inHeat := discovered[c.word]

		var conclusion string
		switch {
		case inStop:
			conclusion = "✅ 已过滤"
		case c.cssOnly && inHeat:
			conclusion = "⚠️  仅CSS(设计如此)"
			cssOnlyLeaks = append(cssOnlyLeaks, c.word)
		case !inStop && inHeat:
			conclusion = "❌ 漏网!需加入stopTokens"
			gaps = append(gaps, c.word)
		default:
			conclusion = "ℹ️  未进入heat发现"
		}

		fmt.Printf("%-10s %-26s %-14v %-14v %s\n",
			c.word, c.group, inStop, inHeat, conclusion)
	}

	fmt.Println(strings.Repeat("─", 88))
	fmt.Printf("\n📊 汇总:\n")
	fmt.Printf("   ❌ 漏网词 (%d 个): %v\n", len(gaps), gaps)
	if len(cssOnlyLeaks) > 0 {
		fmt.Printf("   ⚠️  CSS漏网词 (%d 个,设计如此): %v\n", len(cssOnlyLeaks), cssOnlyLeaks)
	}
	fmt.Println()

	if len(gaps) > 0 {
		t.Errorf("发现 %d 个词绕过了 heat 发现过滤层(需加入 stopTokens): %v",
			len(gaps), gaps)
	}
}

// makeRecallTestArticles 为召回测试构造 10 篇文章(5 baidu_hot + 5 weibo_hot)。
//
// 设计原则(避免 bigram/trigram 替换误伤):
//   - 目标词始终在标题开头,消除"左邻重复"问题
//   - 目标词的直接右邻在 10 篇文章中各不相同,确保每个"word+右邻"组合只出现
//     在 1 篇文章里(不满足 minArticles=2) → 不进入 bigramArticles 结果
//     → 不触发 dedup 将目标词替换掉
func makeRecallTestArticles(word string, baseID int64) []model.Article {
	now := time.Now()
	type ts struct {
		title  string
		source string
	}
	// 10 篇标题:目标词置于句首,直接右邻词全部不同
	// 右邻词: 获得 | 为何 | 震惊 | 预计 | 引起 | 登顶 | 缘何 | 促使 | 令 | 助推
	entries := []ts{
		{word + "获得广泛媒体报道", "baidu_hot"},       // 右邻:获得(独有)
		{word + "为何引发持续讨论", "baidu_hot"},       // 右邻:为何(独有)
		{word + "震惊业界专业人士", "baidu_hot"},       // 右邻:震惊(独有)
		{word + "预计持续成为舆论焦点", "baidu_hot"},   // 右邻:预计(独有)
		{word + "引起各方广泛瞩目", "baidu_hot"},       // 右邻:引起(独有)
		{word + "登顶热搜话题榜单", "weibo_hot"},       // 右邻:登顶(独有)
		{word + "缘何持续引发热议", "weibo_hot"},       // 右邻:缘何(独有)
		{word + "促使行业开始深刻反思", "weibo_hot"},   // 右邻:促使(独有)
		{word + "令全国网民深感震撼", "weibo_hot"},     // 右邻:令(独有)
		{word + "助推舆论广泛传播", "weibo_hot"},       // 右邻:助推(独有)
	}
	arts := make([]model.Article, len(entries))
	for i, e := range entries {
		arts[i] = model.Article{
			ID:          baseID + int64(i),
			Title:       e.title,
			SourceKey:   e.source,
			HeatValue:   1_000_000,
			PublishedAt: now,
		}
	}
	return arts
}

// TestGoodNewWordsRecall 验证"好的新词"能被 collectHeatDiscoveredWords 正确发现。
//
// 这些词是实体型新词,不应被 stopTokens 过滤,应通过 heat discovery 自动发现。
// 使用 10 篇文章 (5 baidu_hot + 5 weibo_hot) 确保满足最高 heatMinArticles 阈值。
//
// 注意事项:
//   - 纯 ASCII 词(如 DeepSeek):hanRuneCount=0,不走 unigram 统计路径,
//     依赖 extractTrackerCandidates 实体提取;测试记录但不强断言。
//   - 4汉字外国人名(如文班亚马):当 GSE 将其切为 4 个单字时,
//     当前代码最多支持 trigram(3字),4字合并需要 GSE 至少切成 2+2,
//     若字典未收录则无法完整还原;测试记录但不强断言。
func TestGoodNewWordsRecall(t *testing.T) {
	type recallCase struct {
		word        string
		note        string
		mustFind    bool // 是否强制断言必须被发现
		softExpect  bool // 软期望:预计能找到但不做强断言(记录实际结果)
	}

	cases := []recallCase{
		// 强断言:这些词通过 bigram/trigram 合并应该能被发现
		{"脑机接口", "科技概念(4汉字,GSE应识别)", true, false},
		{"武契奇", "国际政要(3汉字,trigram合并)", true, false},
		{"智商税", "网络热词(3汉字,bigram合并)", true, false},
		{"段永平", "商业人物(3汉字,bigram合并)", true, false},
		// 软期望:依赖 GSE 字典覆盖,若字典未收录则有局限
		{"文班亚马", "体育明星(4汉字,依赖GSE知道文班/亚马)", false, true},
		// 以下词已在 lexicon 中,isExcludedHeatWord 正确拦截,无需发现
		{"内马尔", "体育明星(已在词典/无需发现)", false, false},
		{"小米SU7", "汽车产品(SU7是小米别名/无需发现)", false, false},
		// 纯英文:不走 Han 统计路径,依赖实体提取
		{"DeepSeek", "AI公司(纯英文,不走heat路径)", false, false},
	}

	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║              TestGoodNewWordsRecall — 好新词召回测试                        ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════════════════╝")
	fmt.Printf("\n%-14s %-36s %-28s %s\n", "词", "备注", "发现词列表(诊断)", "结果")
	fmt.Println(strings.Repeat("─", 92))

	allPass := true
	for i, c := range cases {
		arts := makeRecallTestArticles(c.word, int64(i*20+9000))
		discovered := collectHeatDiscoveredWords(arts)
		_, found := discovered[c.word]

		// 找出发现的词(用于诊断切词效果)
		foundWords := make([]string, 0, len(discovered))
		for w := range discovered {
			foundWords = append(foundWords, w)
		}
		foundSummary := strings.Join(foundWords, " ")
		if len([]rune(foundSummary)) > 24 {
			runes := []rune(foundSummary)
			foundSummary = string(runes[:24]) + "…"
		}

		var status string
		switch {
		case found:
			status = "✅ 已发现"
		case c.softExpect:
			status = "⚠️  未发现(GSE字典限制)"
		case !c.mustFind && !c.softExpect:
			status = "ℹ️  不走heat路径(预期)"
		default:
			status = "❌ 未发现"
			allPass = false
			t.Errorf("期望发现 %q 但未发现; discovered=%v", c.word, mapKeys(discovered))
		}

		fmt.Printf("%-14s %-36s %-28s %s\n", c.word, c.note, foundSummary, status)
	}

	fmt.Println(strings.Repeat("─", 92))
	if allPass {
		fmt.Println("✅ 所有强断言的好新词均已被发现")
	} else {
		fmt.Println("❌ 部分好新词未被召回,请检查 isExcludedHeatWord / stopTokens")
	}
	fmt.Println()
}
