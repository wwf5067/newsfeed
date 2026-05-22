package api

// TestHeatWordFreqAnalysis — 频率分布分析测试
//
// 目标:模拟"从接口拿到黑名单词 vs 已转正非黑名单词"的双方对比,
// 通过 gse 词频分布找到能在兼顾召回率的同时提升准确率的最优过滤阈值。
//
// 方法:
//  1. badWords  = 代表性的"黑名单词"(被发现后手动拉黑的低质量词)
//  2. goodWords = 代表性的"好词"(被发现并转正,有真实追踪价值的词)
//  3. 对每个词计算 gse freq / 是否通过当前各层过滤 / 2-char 与 3+-char 分布
//  4. 扫描不同阈值,找最优:TP (正确过滤 bad) 最高 + FP (错误过滤 good) = 0
//
// 运行: go test -v -run TestHeatWordFreqAnalysis ./internal/api/
// 运行全部: go test -v -run "TestHeatWord" ./internal/api/

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// ------------------------------------------------------------
// 样本数据
// ------------------------------------------------------------

type heatWordSample struct {
	word    string
	comment string
	group   string // "bad" | "good"
}

var heatWordSamples = []heatWordSample{
	// ── BAD: 已知会进黑名单的低质量发现词 ──────────────────────────────
	// 媒体元词
	{"热搜", "媒体元词(已加stopTokens)", "bad"},
	{"词条", "媒体元词(已加stopTokens)", "bad"},
	{"消息", "媒体元词(已加stopTokens)", "bad"},
	{"通报", "官方发布行为", "bad"},
	{"声明", "官方声明泛称", "bad"},
	{"报道", "媒体行为泛称", "bad"},
	{"发布", "泛动作", "bad"},
	// 角色泛称
	{"当事人", "角色泛称(已加stopTokens)", "bad"},
	{"受害者", "角色泛称(已加stopTokens)", "bad"},
	{"嫌疑人", "角色泛称(已加stopTokens)", "bad"},
	{"遇难者", "角色泛称(已加stopTokens)", "bad"},
	{"市民", "角色泛称(已加stopTokens)", "bad"},
	{"居民", "角色泛称(已加stopTokens)", "bad"},
	{"网友", "角色泛称", "bad"},
	{"专家", "角色泛称", "bad"},
	{"工作人员", "角色泛称", "bad"},
	// 时间/场景修饰词
	{"当天", "时间修饰(已加stopTokens)", "bad"},
	{"近日", "时间修饰(已加stopTokens)", "bad"},
	{"现场", "场景泛词", "bad"},
	{"当地", "场景泛词", "bad"},
	{"事发", "场景泛词", "bad"},
	// 数量修饰词
	{"多名", "数量修饰(已加stopTokens)", "bad"},
	{"一名", "数量修饰(已加stopTokens)", "bad"},
	{"数名", "数量修饰(已加stopTokens)", "bad"},
	{"多人", "数量修饰(已在stopTokens基线)", "bad"},
	// 事件类型泛称
	{"案件", "事件类型(已加stopTokens)", "bad"},
	{"事故", "事件类型(已加stopTokens)", "bad"},
	{"纠纷", "事件类型(已加stopTokens)", "bad"},
	{"风波", "事件类型(已加stopTokens)", "bad"},
	{"事件", "事件类型(已在stopTokens基线)", "bad"},
	// 机构/平台泛称
	{"平台", "机构泛称", "bad"},
	{"企业", "机构泛称", "bad"},
	{"官方", "机构泛称(已在stopTokens基线)", "bad"},
	{"部门", "机构泛称", "bad"},
	{"相关部门", "机构泛称(已在stopTokens基线)", "bad"},
	// 动作泛称
	{"调查", "调查行为泛称", "bad"},
	{"救援", "救援行为泛称", "bad"},
	{"处置", "处置行为泛称", "bad"},
	{"回应", "回应行为泛称", "bad"},
	{"质疑", "质疑行为泛称", "bad"},
	{"关注", "关注行为泛称", "bad"},

	// ── GOOD: 应被发现并转正的高质量新词 ─────────────────────────────
	// 科技/AI
	{"脑机接口", "科技新概念(4字)", "good"},
	{"鸿蒙", "华为OS品牌名(2字)", "good"},
	{"ChatGPT", "AI工具(纯英文)", "good"},
	// 国际政要
	{"武契奇", "塞尔维亚总统(3字)", "good"},
	{"泽连斯基", "乌克兰总统(4字)", "good"},
	{"马克龙", "法国总统(3字)", "good"},
	// 网络热词/现象
	{"智商税", "网络热词(3字)", "good"},
	{"内卷", "社会现象词(2字)", "good"},
	{"躺平", "社会现象词(2字)", "good"},
	{"摆烂", "网络用语(2字)", "good"},
	// 商业人物
	{"段永平", "商业人物(3字)", "good"},
	{"马斯克", "企业家(3字)", "good"},
	// 体育明星
	{"文班亚马", "NBA球员(4字)", "good"},
	{"姆巴佩", "足球运动员(3字)", "good"},
	// 产品/事件
	{"星舰", "航天产品(2字)", "good"},
	{"天问", "探测器名(2字)", "good"},
	{"问界", "汽车品牌(2字)", "good"},
	{"蔚来", "汽车品牌(2字)", "good"},
	{"裁员", "经济事件词(2字,有时效性)", "good"},
}

// ------------------------------------------------------------
// TestHeatWordFreqAnalysis — 主分析测试
// ------------------------------------------------------------

func TestHeatWordFreqAnalysis(t *testing.T) {
	trackerSegOnce.Do(loadTrackerSegmenter)

	type result struct {
		sample          heatWordSample
		gseFreq         float64
		inGseDict       bool
		chars           int
		passedStopToken bool // 不在 stopTokens 中(true = 没被拦截)
		passedExcluded  bool // 不在 isExcludedHeatWord(true = 可进入热词发现)
		passedTopicFreq bool // 不被 isGenericTopicByFreq 拦截(true = 可展示)
		minArts         int  // heatMinArticles 要求
	}

	results := make([]result, 0, len(heatWordSamples))
	for _, s := range heatWordSamples {
		r := result{sample: s}
		r.chars = len([]rune(s.word))

		if trackerSegErr == nil {
			freq, _, ok := trackerSeg.Find(s.word)
			r.gseFreq = freq
			r.inGseDict = ok
		}
		_, inStop := stopTokens[s.word]
		r.passedStopToken = !inStop
		r.passedExcluded = !isExcludedHeatWord(s.word)
		r.passedTopicFreq = !isGenericTopicByFreq(s.word)
		r.minArts = heatMinArticles(s.word)

		results = append(results, r)
	}

	// ── 分组打印 ──────────────────────────────────────────────────────
	printGroup := func(group string, title string) {
		fmt.Printf("\n%s\n", strings.Repeat("─", 100))
		fmt.Printf("  %s\n", title)
		fmt.Printf("%s\n", strings.Repeat("─", 100))
		fmt.Printf("%-14s %-8s %-6s %-8s %-10s %-10s %-10s %-8s  %s\n",
			"词", "gseFreq", "字数", "在字典", "过stopTok", "过excluded", "过topicFreq", "minArts", "备注")
		fmt.Printf("%s\n", strings.Repeat("─", 100))
		for _, r := range results {
			if r.sample.group != group {
				continue
			}
			stopMark := "✅已拦截"
			if r.passedStopToken {
				stopMark = "❌可通过"
			}
			excMark := "✅已拦截"
			if r.passedExcluded {
				excMark = "❌可通过"
			}
			topicMark := "✅已拦截"
			if r.passedTopicFreq {
				topicMark = "❌可通过"
			}
			dictMark := "否"
			if r.inGseDict {
				dictMark = "是"
			}
			fmt.Printf("%-14s %-8.0f %-6d %-8s %-10s %-10s %-10s %-8d  %s\n",
				r.sample.word, r.gseFreq, r.chars, dictMark,
				stopMark, excMark, topicMark,
				r.minArts, r.sample.comment)
		}
	}

	printGroup("bad", "🔴 BAD 词(黑名单代表样本) — 期望被过滤")
	printGroup("good", "🟢 GOOD 词(转正非黑名单代表样本) — 期望被保留")

	// ── 频率边界分析 ──────────────────────────────────────────────────
	fmt.Printf("\n%s\n", strings.Repeat("═", 80))
	fmt.Printf("  📊 频率分布分析\n")
	fmt.Printf("%s\n", strings.Repeat("═", 80))

	var badFreqs, goodFreqs []float64
	var badLeaking []string

	for _, r := range results {
		if r.sample.group == "bad" {
			badFreqs = append(badFreqs, r.gseFreq)
			if r.passedExcluded {
				badLeaking = append(badLeaking, fmt.Sprintf("%s(%.0f)", r.sample.word, r.gseFreq))
			}
		} else {
			goodFreqs = append(goodFreqs, r.gseFreq)
			if !r.passedExcluded {
				// 好词被过滤了 — 误伤
				// (理论上只有在白名单里的才会被isExcludedHeatWord拦截,所以应该不多)
			}
		}
	}

	printFreqStats := func(label string, freqs []float64) {
		if len(freqs) == 0 {
			return
		}
		sorted := make([]float64, len(freqs))
		copy(sorted, freqs)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

		var sum float64
		for _, f := range sorted {
			sum += f
		}
		avg := sum / float64(len(sorted))
		p50 := sorted[len(sorted)/2]
		p75 := sorted[len(sorted)*3/4]
		p90 := sorted[min90(len(sorted))]

		fmt.Printf("  %s: n=%d  avg=%.0f  p50=%.0f  p75=%.0f  p90=%.0f  range=[%.0f, %.0f]\n",
			label, len(sorted), avg, p50, p75, p90, sorted[0], sorted[len(sorted)-1])
	}

	printFreqStats("BAD词 gseFreq", badFreqs)
	printFreqStats("GOOD词 gseFreq", goodFreqs)

	if len(badLeaking) > 0 {
		fmt.Printf("\n  ⚠️  当前 isExcludedHeatWord 漏网的 BAD词 (%d): %v\n",
			len(badLeaking), badLeaking)
	} else {
		fmt.Printf("\n  ✅ 当前 isExcludedHeatWord 已拦截全部 BAD词样本\n")
	}

	// ── 阈值扫描:找出最优的 2-char 频率截断 ─────────────────────────
	fmt.Printf("\n%s\n", strings.Repeat("═", 80))
	fmt.Printf("  🔬 2-char 词频率阈值扫描 (新增规则: 2字 + freq>T + 不在白名单 → 排除)\n")
	fmt.Printf("%s\n", strings.Repeat("═", 80))
	fmt.Printf("  %-10s %-20s %-20s %-20s\n",
		"阈值T", "BAD 2字词过滤率", "GOOD 2字词误伤率", "净收益")
	fmt.Printf("  %s\n", strings.Repeat("─", 70))

	thresholds := []float64{100, 200, 300, 400, 500, 600, 800, 1000, 1500, 2000}

	for _, T := range thresholds {
		var bad2Total, bad2Filtered, good2Total, good2FalseDrop int
		for _, r := range results {
			if r.chars != 2 {
				continue
			}
			// 检查是否在白名单(白名单词不会被新规则拦截)
			inWhitelist := isInHeatWhitelist(r.sample.word)
			if r.sample.group == "bad" {
				bad2Total++
				// 新规则: 2字 + freq>T + 不在白名单 + 不已被拦截 → 过滤
				if r.passedExcluded && r.gseFreq > T && !inWhitelist {
					bad2Filtered++
				}
			} else {
				good2Total++
				// 新规则误伤: 2字 + freq>T + 不在白名单 + 当前可通过
				if r.passedExcluded && r.gseFreq > T && !inWhitelist {
					good2FalseDrop++
				}
			}
		}

		filterRate := ""
		if bad2Total > 0 {
			filterRate = fmt.Sprintf("%d/%d (%.0f%%)", bad2Filtered, bad2Total,
				float32(bad2Filtered)/float32(bad2Total)*100)
		}
		falseDropRate := ""
		if good2Total > 0 {
			falseDropRate = fmt.Sprintf("%d/%d (%.0f%%)", good2FalseDrop, good2Total,
				float32(good2FalseDrop)/float32(good2Total)*100)
		}
		net := bad2Filtered - good2FalseDrop*3 // 误伤代价是漏网的3倍
		marker := ""
		if good2FalseDrop == 0 && bad2Filtered > 0 {
			marker = "  ← 无误伤✨"
		}

		fmt.Printf("  T=%-8.0f %-20s %-20s +%d-%d×3=%d%s\n",
			T, filterRate, falseDropRate, bad2Filtered, good2FalseDrop, net, marker)
	}

	// ── heatMinArticles 阈值分析 ────────────────────────────────────
	fmt.Printf("\n%s\n", strings.Repeat("═", 80))
	fmt.Printf("  🎯 heatMinArticles 当前阈值对两组词的要求\n")
	fmt.Printf("%s\n", strings.Repeat("═", 80))
	fmt.Printf("  %-14s %-8s %-8s %-6s\n", "词", "gseFreq", "minArts", "分组")
	fmt.Printf("  %s\n", strings.Repeat("─", 40))
	for _, r := range results {
		if !r.passedExcluded {
			continue // 已被拦截,不参与此分析
		}
		fmt.Printf("  %-14s %-8.0f %-8d %s\n",
			r.sample.word, r.gseFreq, r.minArts, r.sample.group)
	}

	// ── 建议总结 ────────────────────────────────────────────────────
	fmt.Printf("\n%s\n", strings.Repeat("═", 80))
	fmt.Printf("  💡 优化建议\n")
	fmt.Printf("%s\n", strings.Repeat("═", 80))
}

// isInHeatWhitelist 检查词是否在白名单中(被任何保护集覆盖)。
func isInHeatWhitelist(word string) bool {
	if _, ok := trackerEntityLabelSet[word]; ok {
		return true
	}
	if _, ok := strongGeoNames[word]; ok {
		return true
	}
	if _, ok := strongVerbs[word]; ok {
		return true
	}
	if _, ok := strongTopicNouns[word]; ok {
		return true
	}
	return false
}

func min90(n int) int {
	idx := n * 9 / 10
	if idx >= n {
		return n - 1
	}
	return idx
}

// ------------------------------------------------------------
// TestHeatWordFreqThresholdImpl — 验证具体改进方案的效果
// ------------------------------------------------------------
// 此测试用于在实现新规则后,验证改进前后的对比。

func TestHeatWordFreqThresholdImpl(t *testing.T) {
	trackerSegOnce.Do(loadTrackerSegmenter)

	fmt.Printf("\n%s\n", strings.Repeat("═", 80))
	fmt.Printf("  📐 改进方案验证: isExcludedHeatWord 增加 2-char 词频率门槛\n")
	fmt.Printf("%s\n", strings.Repeat("═", 80))

	// 当前规则 vs 改进后规则的对比
	type verdict struct {
		word     string
		group    string
		gseFreq  float64
		chars    int
		curBlock bool // 当前是否被拦截
		newBlock bool // 改进后是否被拦截(newIsExcluded)
		change   string
	}

	var verdicts []verdict
	for _, s := range heatWordSamples {
		freq := float64(0)
		if trackerSegErr == nil {
			f, _, _ := trackerSeg.Find(s.word)
			freq = f
		}
		chars := len([]rune(s.word))
		curBlock := isExcludedHeatWord(s.word)
		newBlock := newIsExcludedHeatWord(s.word, freq, chars)

		change := "—"
		if !curBlock && newBlock {
			change = "🔧 新增过滤"
		} else if curBlock && !newBlock {
			change = "⚠️ 误解除"
		}

		verdicts = append(verdicts, verdict{
			word: s.word, group: s.group, gseFreq: freq,
			chars: chars, curBlock: curBlock, newBlock: newBlock, change: change,
		})
	}

	newFilteredBad := 0
	newFalseDropGood := 0

	fmt.Printf("\n  %-14s %-6s %-8s %-6s %-10s %-10s %s\n",
		"词", "分组", "gseFreq", "字数", "当前", "改进后", "变化")
	fmt.Printf("  %s\n", strings.Repeat("─", 70))
	for _, v := range verdicts {
		curStr := "✅已拦"
		if !v.curBlock {
			curStr = "❌通过"
		}
		newStr := "✅已拦"
		if !v.newBlock {
			newStr = "❌通过"
		}
		if v.change != "—" {
			fmt.Printf("  %-14s %-6s %-8.0f %-6d %-10s %-10s %s\n",
				v.word, v.group, v.gseFreq, v.chars, curStr, newStr, v.change)
			if v.change == "🔧 新增过滤" && v.group == "bad" {
				newFilteredBad++
			} else if v.change == "🔧 新增过滤" && v.group == "good" {
				newFalseDropGood++
			}
		}
	}

	fmt.Printf("\n  📊 改进效果:\n")
	fmt.Printf("     新增正确过滤 BAD 词: %d\n", newFilteredBad)
	fmt.Printf("     新增误伤 GOOD 词:    %d\n", newFalseDropGood)

	if newFalseDropGood > 0 {
		t.Errorf("改进方案误伤了 %d 个 GOOD 词,需要调整阈值", newFalseDropGood)
	}
	if newFilteredBad == 0 {
		t.Log("改进方案未新增过滤(可能样本已全被当前规则覆盖)")
	}
}

// newIsExcludedHeatWord 模拟改进后的 isExcludedHeatWord。
// 改进:对 2 字词增加词典频率门槛 — 频率 > twoCharHeatFreqThreshold
//
//	且不在任何白名单中 → 直接排除(不走 heatMinArticles 动态门槛)。
//
// 原理:
//   - 2字高频词(通报/声明/调查/网友/专家...)在新闻语料中极为常见,
//     一旦某话题爆发会随之在标题中高频出现,误触热词发现门槛。
//   - 真正有价值的 2字新词要么不在 gse 词典(新造词),要么频率极低(专有名词)。
//   - 合法的 2字高频词(美国/日本/涨价/裁员)已在白名单(strongGeoNames/strongTopicNouns)
//     中保护,不受此规则影响。
const twoCharHeatFreqThreshold = float64(400)

func newIsExcludedHeatWord(word string, freq float64, chars int) bool {
	// 已有规则不变
	if isExcludedHeatWord(word) {
		return true
	}
	// 新规则: 2字词 + 高gse频率 + 不在白名单 → 排除
	if chars == 2 && freq > twoCharHeatFreqThreshold {
		if !isInHeatWhitelist(word) {
			return true
		}
	}
	return false
}
