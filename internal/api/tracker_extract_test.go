package api

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wwf5067/newsfeed/internal/model"
)

// TestExtractTrackerCandidates 用真实标题批量测试实体识别和事件抽取效果。
// 每个 case 包含:
//   - title: 真实的新闻/热搜标题
//   - wantEntities: 期望识别出的实体(人名/品牌/地名/组织)
//   - wantKeywords: 期望识别出的关键词/事件短语
//   - wantAbsent: 不应该出现的噪声词
func TestExtractTrackerCandidates(t *testing.T) {
	cases := []struct {
		title        string
		wantEntities []string // 期望提取出的 entity(Kind="entity")
		wantKeywords []string // 期望提取出的 keyword(Kind="keyword")
		wantAbsent   []string // 不应该出现在结果中的噪声
	}{
		// === 科技新闻 ===
		{
			title:        "OpenAI发布GPT-5,性能全面超越Claude",
			wantEntities: []string{"OpenAI", "ChatGPT", "Claude"},
			wantKeywords: []string{},
			wantAbsent:   []string{"发布", "性能", "全面"},
		},
		{
			title:        "华为Mate70系列正式发布,搭载麒麟芯片",
			wantEntities: []string{"华为"},
			wantKeywords: []string{},
			wantAbsent:   []string{"正式", "搭载", "系列"},
		},
		{
			title:        "小米SU7交付量突破10万台",
			wantEntities: []string{"小米"},
			wantKeywords: []string{},
			wantAbsent:   []string{"突破", "万台"},
		},
		{
			title:        "特斯拉在中国市场降价3万元",
			wantEntities: []string{"特斯拉"},
			wantKeywords: []string{},
			wantAbsent:   []string{"中国市场", "万元"},
		},
		{
			title:        "DeepSeek发布新一代大模型,多项指标超GPT-4",
			wantEntities: []string{"DeepSeek", "ChatGPT"},
			wantKeywords: []string{},
			wantAbsent:   []string{"多项", "指标"},
		},

		// === 社会新闻 ===
		{
			title:        "武汉发生4.2级地震,暂无人员伤亡",
			wantEntities: []string{"武汉"},
			wantKeywords: []string{"地震"},
			wantAbsent:   []string{"暂无", "人员"},
		},
		{
			title:        "上海迪士尼乐园宣布涨价",
			wantEntities: []string{},
			wantKeywords: []string{"涨价"},
			wantAbsent:   []string{"宣布"},
		},
		{
			title:        "成都大熊猫基地一熊猫疑似生病",
			wantEntities: []string{"成都"},
			wantKeywords: []string{},
			wantAbsent:   []string{"疑似", "一"},
		},
		{
			title:        "杭州亚运会开幕式惊艳全球",
			wantEntities: []string{"杭州"},
			wantKeywords: []string{},
			wantAbsent:   []string{"全球", "惊艳"},
		},

		// === 国际新闻 ===
		{
			title:        "俄乌冲突持续,泽连斯基发表全国讲话",
			wantEntities: []string{"俄罗斯", "乌克兰"},
			wantKeywords: []string{},
			wantAbsent:   []string{"持续", "全国"},
		},
		{
			title:        "中美贸易谈判取得新进展",
			wantEntities: []string{"中国", "美国"},
			wantKeywords: []string{},
			wantAbsent:   []string{"取得", "新进展"},
		},
		{
			title:        "巴以冲突升级,以色列对加沙发动新一轮空袭",
			wantEntities: []string{"巴勒斯坦", "以色列"},
			wantKeywords: []string{},
			wantAbsent:   []string{"升级", "发动"},
		},
		{
			title:        "特朗普宣布竞选2028年总统",
			wantEntities: []string{"特朗普"},
			wantKeywords: []string{},
			wantAbsent:   []string{"宣布", "竞选"},
		},

		// === 娱乐新闻 ===
		{
			title:        "《哪吒2》票房突破100亿",
			wantEntities: []string{"哪吒"},
			wantKeywords: []string{},
			wantAbsent:   []string{"票房", "突破"},
		},
		{
			title:        "易烊千玺新电影《长空之王》定档",
			wantEntities: []string{"易烊千玺", "长空之王"},
			wantKeywords: []string{},
			wantAbsent:   []string{"新电影", "定档"},
		},
		{
			title:        "周杰伦演唱会门票秒光,黄牛价翻10倍",
			wantEntities: []string{"周杰伦"},
			wantKeywords: []string{"演唱会"},
			wantAbsent:   []string{"门票", "秒光", "黄牛"},
		},

		// === 体育新闻 ===
		{
			title:        "NBA季后赛:湖人4-2淘汰勇士晋级",
			wantEntities: []string{"NBA"},
			wantKeywords: []string{},
			wantAbsent:   []string{"淘汰"},
		},
		{
			title:        "梅西加盟迈阿密国际,年薪5000万美元",
			wantEntities: []string{"梅西"},
			wantKeywords: []string{},
			wantAbsent:   []string{"年薪", "美元"},
		},
		{
			title:        "中国女排3-1战胜日本队",
			wantEntities: []string{"中国女排"},
			wantKeywords: []string{},
			wantAbsent:   []string{},
		},

		// === 知乎讨论 / B站热门 ===
		{
			title:        "如何评价比亚迪海鸥降价到5万以下?",
			wantEntities: []string{"比亚迪"},
			wantKeywords: []string{"降价"},
			wantAbsent:   []string{"如何评价", "以下"},
		},
		{
			title:        "为什么现在的年轻人不愿意进工厂了?",
			wantEntities: []string{},
			wantKeywords: []string{},
			wantAbsent:   []string{"为什么", "年轻人", "不愿意", "工厂"},
		},
		{
			title:        "腾讯游戏宣布全面接入DeepSeek",
			wantEntities: []string{"腾讯", "DeepSeek"},
			wantKeywords: []string{},
			wantAbsent:   []string{"宣布", "全面"},
		},
		{
			title:        "B站UP主曝光某品牌虚假宣传",
			wantEntities: []string{"B站"},
			wantKeywords: []string{},
			wantAbsent:   []string{"UP主", "曝光", "某品牌"},
		},

		// === 财经新闻 ===
		{
			title:        "A股三大指数集体收涨,沪指重回3300点",
			wantEntities: []string{},
			wantKeywords: []string{},
			wantAbsent:   []string{"集体"},
		},
		{
			title:        "英伟达市值突破3万亿美元",
			wantEntities: []string{"英伟达"},
			wantKeywords: []string{},
			wantAbsent:   []string{"突破", "美元"},
		},
		{
			title:        "比特币价格突破10万美元创历史新高",
			wantEntities: []string{"比特币"},
			wantKeywords: []string{},
			wantAbsent:   []string{"突破", "美元", "历史"},
		},

		// === 事件类(期望抽取事件关键词) ===
		{
			title:        "广州地铁11号线发生脱轨事故",
			wantEntities: []string{"广州"},
			wantKeywords: []string{"事故"},
			wantAbsent:   []string{"发生"},
		},
		{
			title:        "某知名主播被曝偷税漏税",
			wantEntities: []string{},
			wantKeywords: []string{"偷税漏税"},
			wantAbsent:   []string{"某知名", "被曝"},
		},
		{
			title:        "山东化工厂爆炸,已致3人死亡",
			wantEntities: []string{},
			wantKeywords: []string{"爆炸"},
			wantAbsent:   []string{"已致"},
		},

		// === 边界 case ===
		{
			title:        "",
			wantEntities: []string{},
			wantKeywords: []string{},
			wantAbsent:   []string{},
		},
		{
			title:        "哔哩哔哩",
			wantEntities: []string{},
			wantKeywords: []string{},
			wantAbsent:   []string{"哔哩哔哩"},
		},
		{
			title:        "最新消息:详情来了",
			wantEntities: []string{},
			wantKeywords: []string{},
			wantAbsent:   []string{"最新消息", "详情", "来了"},
		},
	}

	// 统计
	totalEntities := 0
	foundEntities := 0
	totalKeywords := 0
	foundKeywords := 0
	noiseLeaked := 0
	totalNoise := 0

	for _, tc := range cases {
		article := model.Article{Title: tc.title}
		candidates := extractTrackerCandidates(article, nil)

		// 收集实际结果
		gotEntities := map[string]struct{}{}
		gotKeywords := map[string]struct{}{}
		allLabels := map[string]struct{}{}
		for _, c := range candidates {
			allLabels[c.Label] = struct{}{}
			if c.Kind == "entity" {
				gotEntities[c.Label] = struct{}{}
			} else {
				gotKeywords[c.Label] = struct{}{}
			}
		}

		// 检查期望的 entity
		for _, want := range tc.wantEntities {
			totalEntities++
			if _, ok := gotEntities[want]; ok {
				foundEntities++
			} else {
				t.Errorf("MISS entity | title=%q | want=%q | got=%v",
					tc.title, want, candidateLabels(candidates))
			}
		}

		// 检查期望的 keyword
		for _, want := range tc.wantKeywords {
			totalKeywords++
			// keyword 可能被以 entity 识别也行,或是作为 allLabels 中的一项
			if _, ok := allLabels[want]; ok {
				foundKeywords++
			} else {
				// 容许部分匹配:如果期望的 keyword 是实际 label 的子串或反之
				matched := false
				for label := range allLabels {
					if strings.Contains(label, want) || strings.Contains(want, label) {
						matched = true
						break
					}
				}
				if matched {
					foundKeywords++
				} else {
					t.Errorf("MISS keyword | title=%q | want=%q | got=%v",
						tc.title, want, candidateLabels(candidates))
				}
			}
		}

		// 检查噪声
		for _, absent := range tc.wantAbsent {
			totalNoise++
			if _, ok := allLabels[absent]; ok {
				noiseLeaked++
				t.Errorf("NOISE leaked | title=%q | noise=%q | got=%v",
					tc.title, absent, candidateLabels(candidates))
			}
		}
	}

	// 输出汇总
	fmt.Println("\n========== 实体识别与事件抽取效果评估 ==========")
	fmt.Printf("实体召回率: %d/%d = %.1f%%\n", foundEntities, totalEntities,
		safePercent(foundEntities, totalEntities))
	fmt.Printf("关键词召回率: %d/%d = %.1f%%\n", foundKeywords, totalKeywords,
		safePercent(foundKeywords, totalKeywords))
	fmt.Printf("噪声过滤率: %d/%d = %.1f%% (越高越好)\n",
		totalNoise-noiseLeaked, totalNoise,
		safePercent(totalNoise-noiseLeaked, totalNoise))
	fmt.Printf("噪声泄漏数: %d/%d\n", noiseLeaked, totalNoise)
	fmt.Println("==============================================")
}

// TestExtractTrackerCandidatesDetail 逐条打印每个标题的完整抽取结果,方便人工审查。
func TestExtractTrackerCandidatesDetail(t *testing.T) {
	titles := []string{
		// 科技
		"OpenAI发布GPT-5,性能全面超越Claude",
		"华为Mate70系列正式发布,搭载麒麟芯片",
		"小米SU7交付量突破10万台",
		"DeepSeek发布新一代大模型,多项指标超GPT-4",
		"腾讯游戏宣布全面接入DeepSeek",
		"苹果iPhone 16被曝存在电池鼓包问题",
		"英伟达市值突破3万亿美元,成全球第一",

		// 国际
		"俄乌冲突持续,泽连斯基发表全国讲话",
		"中美贸易谈判取得新进展",
		"巴以冲突升级,以色列对加沙发动新一轮空袭",
		"特朗普宣布竞选2028年总统",
		"朝韩局势紧张,朝鲜试射弹道导弹",

		// 社会
		"武汉发生4.2级地震,暂无人员伤亡",
		"杭州亚运会开幕式惊艳全球",
		"广州地铁11号线发生脱轨事故",
		"山东化工厂爆炸,已致3人死亡",
		"成都大熊猫基地一熊猫疑似生病",

		// 娱乐
		"《哪吒2》票房突破100亿",
		"易烊千玺新电影《长空之王》定档",
		"周杰伦演唱会门票秒光,黄牛价翻10倍",
		"王一博代言奢侈品牌引争议",

		// 体育
		"NBA季后赛:湖人4-2淘汰勇士晋级",
		"梅西加盟迈阿密国际,年薪5000万美元",
		"中国女排3-1战胜日本队",

		// 知乎/B站
		"如何评价比亚迪海鸥降价到5万以下?",
		"腾讯游戏宣布全面接入DeepSeek",
		"B站UP主曝光某品牌虚假宣传",
		"为什么现在的年轻人不愿意进工厂了?",

		// 财经
		"A股三大指数集体收涨,沪指重回3300点",
		"比特币价格突破10万美元创历史新高",

		// 噪声标题
		"哔哩哔哩",
		"最新消息:详情来了",
		"如何看待此事?对此你怎么看",
		"",
	}

	fmt.Println("\n========== 标题实体/事件抽取详细结果 ==========")
	fmt.Println()
	for _, title := range titles {
		if title == "" {
			fmt.Println("标题: (空)")
			fmt.Println("  结果: (无)")
			fmt.Println()
			continue
		}
		article := model.Article{Title: title}
		candidates := extractTrackerCandidates(article, nil)
		fmt.Printf("标题: %s\n", title)
		if len(candidates) == 0 {
			fmt.Println("  结果: (无候选)")
		} else {
			for i, c := range candidates {
				related := ""
				if len(c.RelatedTerms) > 0 {
					related = " related=" + strings.Join(c.RelatedTerms, ",")
				}
				fmt.Printf("  [%d] %-8s %s%s\n", i+1, c.Kind, c.Label, related)
			}
		}
		fmt.Println()
	}
	fmt.Println("================================================")
}

func candidateLabels(candidates []trackerCandidate) []string {
	labels := make([]string, 0, len(candidates))
	for _, c := range candidates {
		labels = append(labels, fmt.Sprintf("%s(%s)", c.Label, c.Kind))
	}
	return labels
}

func safePercent(num, denom int) float64 {
	if denom == 0 {
		return 100.0
	}
	return float64(num) * 100.0 / float64(denom)
}
