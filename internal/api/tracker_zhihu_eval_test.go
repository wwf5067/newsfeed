package api

import (
	"fmt"
	"testing"

	"github.com/wwf5067/newsfeed/internal/model"
)

func TestTrackerExtraction_ZhihuHot(t *testing.T) {
	titles := []string{
		`武汉一村庄585人62人患癌，村民怀疑与工厂有关，举报4年无果，哪些信息值得关注？哪些相关方该负责？`,
		`新加坡总理要求民众「做好抗通胀准备」，请问2026年全球通胀趋势一旦确立，普通人能如何抗通胀呢？`,
		`逃犯3年用掉一千吨水，称听洗衣机转动声能让自己平静，整天在家不停洗衣服，从心理学角度如何解释这一行为？`,
		`如何评价小米 YU7 GT 刷新纽北 SUV 圈速纪录，任周灿成为首个获得纽北官方圈速认证的中国车手？`,
		`没考上大专，18岁在深圳做流水线工人，真的很迷茫，有没有过来人的真心建议？`,
		`2526赛季 NBA 西决G1，文班亚马 41 分 24 篮板，马刺 122-115 雷霆，如何评价？`,
		`当你吃着白馍夹着满满的肉时，是否会好奇为啥叫「肉夹馍」而不叫「馍夹肉」？为啥会有这种不合逻辑的说法？`,
		`普京即将启程访华，俄罗斯远东地区要真正融入「大远东」经济圈，最紧迫的突破口在哪里？`,
		`河南一幼儿园用依云矿泉水蒸饭引争议，工作人员回应餐标不是虚假的，自开园起便是如此，这种做法合理吗？`,
		`如何看待多家银行关停独立信用卡 App？这会带来哪些影响？`,
		`美以袭击伊朗进入第 81 天，当前局势如何？哪些信息值得关注？`,
		`为什么动物的嘴巴都是长在眼睛下面，包括我们人类也是这样，难道不能长在眼睛上面吗？这到底是什么原因啊？`,
		`「学术侦探」耿同学的论文打假之路为何走得如此孤独？`,
		`家长哭诉女儿遭校园欺凌，沈奕斐称是家长陷入「受害者思维」导致孩子认知扭曲，你认同吗？是家长过度焦虑吗？`,
		`俄罗斯当前面临着怎样的国内外局势？普京此次访华有哪些问题亟待解决？`,
		`《昨天今天明天》这个小品明显唱红，但是非常火，在今天这种唱红作品却没有市场，什么原因？`,
		`现在的患者为什么动不动就要投诉医生？`,
		`如何评价上海交大通报给予樊同学严重警告处分？`,
		`哪些届世界杯决赛，如果冠亚军对调的话，会诞生球王？`,
		`巴西公布世界杯 26 人名单，内马尔回归，7 大名将落选，你对该球队有哪些期待？这支球队能走多远？`,
		`全球升温已连续三年超过1.5℃，这意味着什么？这一临界点失守是否会导致变暖迎来质变？`,
		`如何看待内马尔压哨入选美加墨世界杯巴西队26人最终名单？并预测下这只巴西队能走多远？`,
		`近年来中国科学技术大学、上海财经大学等多所高校撤销外语专业，AI翻译普及后，外语专业未来发展出路在哪？`,
		`孙杨回应妈宝男标签称「谁见过事业这么成功的妈宝男」「谷爱凌等顶级运动员也有母亲陪着」，如何看待此回应？`,
		`茅台宣布部分产品涨价，马年珍享版单瓶涨幅 200 元，如何看待此次调价？能否成为行业涨价信号？`,
		`如何评价《崩坏星穹铁道》新发布的啊哈时刻|姬子·启行？`,
		`北方为何会「一秒入夏」，5月就迎来大范围高温？背后的气象原因是什么？`,
		`「三不明」中成药或将迎来医保目录「清退」，释放出怎样的信号？对患者用药又有多大影响？`,
		`人类有两个肺，为什么没有进化成一个肺一个鳃？`,
		`《三国志》系列里，你觉得数值给的最离谱的一个武将是谁，高的离谱或者低的离谱的？`,
	}

	// 人工标注的理想输出(精确匹配)
	type expected struct {
		entities []string // 精确匹配期望的 entity
		keywords []string // 精确匹配期望的 keyword
		noOutput bool     // true 表示理想情况下不应有任何候选
	}

	expectations := []expected{
		{entities: []string{"武汉"}, keywords: []string{"患癌"}},                                           // 01
		{entities: []string{"新加坡"}, keywords: []string{"通胀"}},                                           // 02
		{noOutput: true},                                                                              // 03 生活猎奇
		{entities: []string{"小米"}, keywords: nil},                                                       // 04
		{entities: []string{"深圳"}, keywords: nil},                                                       // 05 深圳已入词典
		{entities: []string{"NBA", "文班亚马", "马刺", "雷霆"}, keywords: nil},                                 // 06
		{noOutput: true},                                                                              // 07 冷知识
		{entities: []string{"普京", "俄罗斯", "中国"}, keywords: nil},                                         // 08 "访华"应触发中国
		{entities: []string{"依云", "河南"}},                                                             // 09 依云品牌+河南省，上游新增词典条目后已有候选
		{keywords: []string{"信用卡"}},                                                                    // 10
		{entities: []string{"美国", "以色列", "伊朗"}, keywords: nil},                                         // 11
		{noOutput: true},                                                                              // 12 科普
		{keywords: []string{"论文打假"}},                                                                   // 13
		{entities: []string{"沈奕斐"}, keywords: []string{"校园欺凌"}},                                        // 14 沈奕斐已入词典
		{entities: []string{"俄罗斯", "普京", "中国"}, keywords: nil},                                         // 15
		{entities: []string{"昨天今天明天"}, keywords: nil},                                                  // 16 《》作品名 ok
		{noOutput: true},                                                                              // 17 社会讨论
		{entities: []string{"上海交通大学"}, keywords: nil},                                                  // 18
		{entities: []string{"世界杯"}, keywords: nil},                                                     // 19
		{entities: []string{"内马尔", "世界杯", "巴西"}, keywords: nil},                                       // 20 巴西已入词典
		{noOutput: true},                                                                              // 21 气候科普
		{entities: []string{"内马尔", "世界杯", "巴西"}, keywords: nil},                                       // 22 巴西已入词典
		{entities: []string{"中国科学技术大学", "上海财经大学"}, keywords: nil},                                    // 23 上财已入词典
		{entities: []string{"孙杨", "谷爱凌"}, keywords: nil},                                              // 24 孙杨/谷爱凌已入词典
		{entities: []string{"茅台"}, keywords: []string{"涨价"}},                                          // 25 茅台已入词典
		{entities: []string{"崩坏：星穹铁道"}, keywords: nil},                                               // 26
		{keywords: []string{"高温"}},                                                                    // 27
		{keywords: []string{"医保"}},                                                                    // 28
		{noOutput: true},                                                                              // 29 科普
		{entities: []string{"三国志"}, keywords: nil},                                                    // 30
	}

	fmt.Println("")
	fmt.Println("========== 知乎热榜 Top30 实体/事件抽取评估 ==========")
	fmt.Println("")

	totalExpEntities := 0
	matchedEntities := 0
	totalExpKeywords := 0
	matchedKeywords := 0
	totalOutputEntities := 0
	correctOutputEntities := 0
	noOutputExpected := 0
	noOutputCorrect := 0
	falseEntityFromQuotes := 0 // 「」误触发计数

	for i, title := range titles {
		article := model.Article{ID: int64(i + 1), Title: title}
		candidates := extractTrackerCandidates(article)

		entities := []string{}
		keywords := []string{}
		for _, c := range candidates {
			if c.Kind == "entity" {
				entities = append(entities, c.Label)
			} else {
				keywords = append(keywords, c.Label)
			}
		}
		totalOutputEntities += len(entities)

		exp := expectations[i]

		// 召回: 期望的 entity 是否被输出
		for _, e := range exp.entities {
			totalExpEntities++
			found := false
			for _, out := range entities {
				if out == e {
					found = true
					break
				}
			}
			if found {
				matchedEntities++
			}
		}

		// 召回: 期望的 keyword 是否被输出(宽松:输出的 keyword 包含期望词即可)
		for _, k := range exp.keywords {
			totalExpKeywords++
			found := false
			for _, out := range keywords {
				if out == k {
					found = true
					break
				}
			}
			// 宽松匹配:keyword 输出里包含期望词也算
			if !found {
				for _, out := range keywords {
					if len(out) > len(k) && containsStr(out, k) {
						found = true
						break
					}
				}
			}
			if found {
				matchedKeywords++
			}
		}

		// 精确率: 输出的 entity 里有多少在 expected 列表中
		for _, out := range entities {
			for _, e := range exp.entities {
				if out == e {
					correctOutputEntities++
					break
				}
			}
		}

		// noOutput 检查
		if exp.noOutput {
			noOutputExpected++
			if len(candidates) == 0 {
				noOutputCorrect++
			}
		}

		// 输出详情
		fmt.Printf("[%02d] %s\n", i+1, title)
		if len(entities) > 0 {
			fmt.Printf("     实体: %v\n", entities)
		}
		if len(keywords) > 0 {
			fmt.Printf("     关键词: %v\n", keywords)
		}
		if len(entities) == 0 && len(keywords) == 0 {
			fmt.Printf("     (无候选)\n")
		}

		// 标注差异
		issues := []string{}
		for _, e := range exp.entities {
			found := false
			for _, out := range entities {
				if out == e {
					found = true
					break
				}
			}
			if !found {
				issues = append(issues, "漏entity:"+e)
			}
		}
		for _, k := range exp.keywords {
			found := false
			for _, out := range keywords {
				if out == k || containsStr(out, k) {
					found = true
					break
				}
			}
			if !found {
				issues = append(issues, "漏keyword:"+k)
			}
		}
		// 噪声 entity
		for _, out := range entities {
			isExpected := false
			for _, e := range exp.entities {
				if out == e {
					isExpected = true
					break
				}
			}
			if !isExpected && !exp.noOutput {
				issues = append(issues, "噪声entity:"+out)
			}
		}
		if exp.noOutput && len(candidates) > 0 {
			issues = append(issues, fmt.Sprintf("应无输出但有%d个候选", len(candidates)))
		}

		if len(issues) > 0 {
			fmt.Printf("     ⚠ %v\n", issues)
		}
		fmt.Println("")
	}

	// 统计「」误触发
	_ = falseEntityFromQuotes

	fmt.Println("========== 汇总指标 ==========")
	if totalExpEntities > 0 {
		fmt.Printf("实体召回率: %d/%d = %.1f%%\n", matchedEntities, totalExpEntities, pct(matchedEntities, totalExpEntities))
	}
	if totalExpKeywords > 0 {
		fmt.Printf("关键词召回率: %d/%d = %.1f%%\n", matchedKeywords, totalExpKeywords, pct(matchedKeywords, totalExpKeywords))
	}
	if totalOutputEntities > 0 {
		fmt.Printf("实体精确率: %d/%d = %.1f%%\n", correctOutputEntities, totalOutputEntities, pct(correctOutputEntities, totalOutputEntities))
	}
	if noOutputExpected > 0 {
		fmt.Printf("无效标题过滤率: %d/%d = %.1f%% (期望无输出的标题中正确过滤的)\n", noOutputCorrect, noOutputExpected, pct(noOutputCorrect, noOutputExpected))
	}
	fmt.Println("================================")
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) > 0 && findSubstr(s, sub))
}

func findSubstr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) * 100 / float64(b)
}
