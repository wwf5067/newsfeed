package api

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wwf5067/newsfeed/internal/model"
)

// TestExtractZhihuHotTitles 用知乎热榜前 50 真实标题测试抽取效果。
func TestExtractZhihuHotTitles(t *testing.T) {
	type testCase struct {
		title        string
		wantEntities []string // 期望的 entity（至少应该出现）
		wantKeywords []string // 期望的 keyword（至少应该出现）
		wantAbsent   []string // 不应出现的噪声
	}

	cases := []testCase{
		{
			title:        "武汉一村庄585人62人患癌，村民怀疑与工厂有关，举报4年无果，哪些信息值得关注？哪些相关方该负责？",
			wantEntities: []string{"武汉"},
			wantKeywords: []string{"患癌"},
			wantAbsent:   []string{"村民", "工厂", "哪些信息", "哪些相关方", "该负责", "值得关注"},
		},
		{
			title:        "新加坡总理要求民众「做好抗通胀准备」，请问2026年全球通胀趋势一旦确立，普通人能如何抗通胀呢？",
			wantEntities: []string{"新加坡"},
			wantKeywords: []string{},
			wantAbsent:   []string{"普通人", "如何", "请问"},
		},
		{
			title:        "逃犯3年用掉一千吨水，称听洗衣机转动声能让自己平静，整天在家不停洗衣服，从心理学角度如何解释这一行为？",
			wantEntities: []string{},
			wantKeywords: []string{},
			wantAbsent:   []string{"如何", "这一行为"},
		},
		{
			title:        "普京即将启程访华，俄罗斯远东地区要真正融入「大远东」经济圈，最紧迫的突破口在哪里？",
			wantEntities: []string{"普京", "俄罗斯"},
			wantKeywords: []string{},
			wantAbsent:   []string{"最紧迫", "在哪里"},
		},
		{
			title:        "当你吃着白馍夹着满满的肉时，是否会好奇为啥叫「肉夹馍」而不叫「馍夹肉」？为啥会有这种不合逻辑的说法？",
			wantEntities: []string{},
			wantKeywords: []string{},
			wantAbsent:   []string{"是否", "为啥"},
		},
		{
			title:        "如何评价小米 YU7 GT 刷新纽北 SUV 圈速纪录，任周灿成为首个获得纽北官方圈速认证的中国车手？",
			wantEntities: []string{"小米"},
			wantKeywords: []string{},
			wantAbsent:   []string{"如何评价", "首个", "获得"},
		},
		{
			title:        "河南一幼儿园用依云矿泉水蒸饭引争议，工作人员回应餐标不是虚假的，自开园起便是如此，这种做法合理吗？",
			wantEntities: []string{},
			wantKeywords: []string{},
			wantAbsent:   []string{"工作人员", "回应", "合理吗"},
		},
		{
			title:        "没考上大专，18岁在深圳做流水线工人，真的很迷茫，有没有过来人的真心建议？",
			wantEntities: []string{"深圳"},
			wantKeywords: []string{},
			wantAbsent:   []string{"真的", "真心", "建议"},
		},
		{
			title:        "2526赛季 NBA 西决G1，文班亚马 41 分 24 篮板，马刺 122-115 雷霆，如何评价？",
			wantEntities: []string{"NBA"},
			wantKeywords: []string{},
			wantAbsent:   []string{"如何评价", "2526赛季"},
		},
		{
			title:        "家长哭诉女儿遭校园欺凌，沈奕斐称是家长陷入「受害者思维」导致孩子认知扭曲，你认同吗？是家长过度焦虑吗？",
			wantEntities: []string{},
			wantKeywords: []string{"校园欺凌"},
			wantAbsent:   []string{"你认同吗", "过度焦虑"},
		},
		{
			title:        "美以袭击伊朗进入第 81 天，当前局势如何？哪些信息值得关注？",
			wantEntities: []string{"美国", "以色列", "伊朗"},
			wantKeywords: []string{},
			wantAbsent:   []string{"当前", "局势如何", "哪些信息", "值得关注"},
		},
		{
			title:        "现在的患者为什么动不动就要投诉医生？",
			wantEntities: []string{},
			wantKeywords: []string{},
			wantAbsent:   []string{"为什么", "动不动"},
		},
		{
			title:        "俄罗斯当前面临着怎样的国内外局势？普京此次访华有哪些问题亟待解决？",
			wantEntities: []string{"俄罗斯", "普京"},
			wantKeywords: []string{},
			wantAbsent:   []string{"当前", "怎样", "亟待解决"},
		},
		{
			title:        "如何看待多家银行关停独立信用卡 App？这会带来哪些影响？",
			wantEntities: []string{},
			wantKeywords: []string{"信用卡"},
			wantAbsent:   []string{"如何看待", "哪些影响"},
		},
		{
			title:        "“学术侦探″耿同学的论文打假之路为何走得如此孤独？",
			wantEntities: []string{},
			wantKeywords: []string{"论文打假"},
			wantAbsent:   []string{"为何", "如此"},
		},
		{
			title:        "《昨天今天明天》这个小品明显唱红，但是非常火，在今天这种唱红作品却没有市场，什么原因？",
			wantEntities: []string{"昨天今天明天"},
			wantKeywords: []string{},
			wantAbsent:   []string{"非常", "什么原因"},
		},
		{
			title:        "为什么动物的嘴巴都是长在眼睛下面，包括我们人类也是这样，难道不能长在眼睛上面吗？这到底是什么原因啊？",
			wantEntities: []string{},
			wantKeywords: []string{},
			wantAbsent:   []string{"为什么", "我们", "到底"},
		},
		{
			title:        "如何评价上海交大通报给予樊同学严重警告处分？",
			wantEntities: []string{},
			wantKeywords: []string{},
			wantAbsent:   []string{"如何评价", "给予"},
		},
		{
			title:        "哪些届世界杯决赛，如果冠亚军对调的话，会诞生球王？",
			wantEntities: []string{},
			wantKeywords: []string{"世界杯"},
			wantAbsent:   []string{"哪些", "如果"},
		},
		{
			title:        "全球升温已连续三年超过1.5℃，这意味着什么？这一临界点失守是否会导致变暖迎来质变？",
			wantEntities: []string{},
			wantKeywords: []string{},
			wantAbsent:   []string{"这意味着什么", "是否", "导致"},
		},
		{
			title:        "如何看待内马尔压哨入选美加墨世界杯巴西队26人最终名单？并预测下这只巴西队能走多远？",
			wantEntities: []string{"内马尔"},
			wantKeywords: []string{"世界杯"},
			wantAbsent:   []string{"如何看待", "预测"},
		},
		{
			title:        "近年来中国科学技术大学、上海财经大学等多所高校撤销外语专业，AI翻译普及后，外语专业未来发展出路在哪？",
			wantEntities: []string{},
			wantKeywords: []string{},
			wantAbsent:   []string{"近年来", "多所", "在哪"},
		},
		{
			title:        "孙杨回应妈宝男标签称「谁见过事业这么成功的妈宝男」「谷爱凌等顶级运动员也有母亲陪着」，如何看待此回应？",
			wantEntities: []string{"孙杨", "谷爱凌"},
			wantKeywords: []string{},
			wantAbsent:   []string{"如何看待", "回应"},
		},
		{
			title:        "巴西公布世界杯 26 人名单，内马尔回归，7 大名将落选，你对该球队有哪些期待？这支球队能走多远？",
			wantEntities: []string{"内马尔"},
			wantKeywords: []string{"世界杯"},
			wantAbsent:   []string{"哪些期待", "走多远"},
		},
		{
			title:        "茅台宣布部分产品涨价，马年珍享版单瓶涨幅 200 元，如何看待此次调价？能否成为行业涨价信号？",
			wantEntities: []string{"茅台"},
			wantKeywords: []string{"涨价"},
			wantAbsent:   []string{"如何看待", "能否", "宣布"},
		},
		{
			title:        "5 名中国人在泰国遭 4 名警察 1 平民绑架、持枪勒索，什么情况？前往泰国应如何防范此类事情？",
			wantEntities: []string{"泰国"},
			wantKeywords: []string{"绑架"},
			wantAbsent:   []string{"什么情况", "如何防范", "平民", "警察"},
		},
		{
			title:        "如何评价《崩坏星穹铁道》新发布的啊哈时刻|姬子·启行？",
			wantEntities: []string{"崩坏：星穹铁道"},
			wantKeywords: []string{},
			wantAbsent:   []string{"如何评价", "发布"},
		},
		{
			title:        "北方为何会「一秒入夏」，5月就迎来大范围高温？背后的气象原因是什么？",
			wantEntities: []string{},
			wantKeywords: []string{"高温"},
			wantAbsent:   []string{"为何", "背后", "原因是什么"},
		},
		{
			title:        "人类有两个肺，为什么没有进化成一个肺一个鳃？",
			wantEntities: []string{},
			wantKeywords: []string{},
			wantAbsent:   []string{"为什么", "一个"},
		},
		{
			title:        "「三不明」中成药或将迎来医保目录「清退」，释放出怎样的信号？对患者用药又有多大影响？",
			wantEntities: []string{},
			wantKeywords: []string{},
			wantAbsent:   []string{"怎样", "多大"},
		},
	}

	// 统计
	totalEntities := 0
	foundEntities := 0
	totalKeywords := 0
	foundKeywords := 0
	noiseLeaked := 0
	totalNoise := 0

	fmt.Println("")
	fmt.Println("========== 知乎热榜标题抽取详细结果 ==========")
	fmt.Println("")

	for i, tc := range cases {
		article := model.Article{Title: tc.title}
		candidates := extractTrackerCandidates(article)

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

		// 打印详情
		fmt.Printf("[%02d] %s\n", i+1, tc.title)
		if len(candidates) == 0 {
			fmt.Println("     → (无候选)")
		} else {
			for j, c := range candidates {
				fmt.Printf("     [%d] %-8s %s\n", j+1, c.Kind, c.Label)
			}
		}

		// 检查期望的 entity
		for _, want := range tc.wantEntities {
			totalEntities++
			if _, ok := gotEntities[want]; ok {
				foundEntities++
			} else {
				// 检查是否以子串形式存在
				found := false
				for label := range gotEntities {
					if strings.Contains(label, want) || strings.Contains(want, label) {
						found = true
						break
					}
				}
				if found {
					foundEntities++
				} else {
					fmt.Printf("     ❌ MISS entity: %q\n", want)
					t.Errorf("MISS entity | title=%q | want=%q | got=%v",
						tc.title, want, candidateLabelsZh(candidates))
				}
			}
		}

		// 检查期望的 keyword
		for _, want := range tc.wantKeywords {
			totalKeywords++
			if _, ok := allLabels[want]; ok {
				foundKeywords++
			} else {
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
					fmt.Printf("     ❌ MISS keyword: %q\n", want)
					t.Errorf("MISS keyword | title=%q | want=%q | got=%v",
						tc.title, want, candidateLabelsZh(candidates))
				}
			}
		}

		// 检查噪声
		for _, absent := range tc.wantAbsent {
			totalNoise++
			if _, ok := allLabels[absent]; ok {
				noiseLeaked++
				fmt.Printf("     ⚠️  NOISE: %q\n", absent)
				t.Errorf("NOISE leaked | title=%q | noise=%q", tc.title, absent)
			}
		}
		fmt.Println()
	}

	// 输出汇总
	fmt.Println("========== 知乎热榜抽取效果汇总 ==========")
	fmt.Printf("实体召回率: %d/%d = %.1f%%\n", foundEntities, totalEntities,
		safePercentZh(foundEntities, totalEntities))
	fmt.Printf("关键词召回率: %d/%d = %.1f%%\n", foundKeywords, totalKeywords,
		safePercentZh(foundKeywords, totalKeywords))
	fmt.Printf("噪声过滤率: %d/%d = %.1f%% (越高越好)\n",
		totalNoise-noiseLeaked, totalNoise,
		safePercentZh(totalNoise-noiseLeaked, totalNoise))
	fmt.Printf("噪声泄漏数: %d/%d\n", noiseLeaked, totalNoise)
	fmt.Println("=============================================")
}

func candidateLabelsZh(candidates []trackerCandidate) []string {
	labels := make([]string, 0, len(candidates))
	for _, c := range candidates {
		labels = append(labels, fmt.Sprintf("%s(%s)", c.Label, c.Kind))
	}
	return labels
}

func safePercentZh(num, denom int) float64 {
	if denom == 0 {
		return 100.0
	}
	return float64(num) * 100.0 / float64(denom)
}
