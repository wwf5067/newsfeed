package api

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wwf5067/newsfeed/internal/model"
)

// TestHeatDiscovery 验证热度反馈式实体发现:跨文章高频词自动被发现。
func TestHeatDiscovery(t *testing.T) {
	// 模拟场景: "520红包" 在百度和微博的多条标题中出现
	articles := []model.Article{
		{ID: 1, Title: "微信再度开放520元大额红包 限时1天", SourceKey: "baidu_hot"},
		{ID: 2, Title: "520红包", SourceKey: "weibo_hot"},
		{ID: 3, Title: "520红包怎么发?最全攻略来了", SourceKey: "zhihu_hot"},
		{ID: 4, Title: "今年520红包金额上限提高到520元", SourceKey: "baidu_hot"},
		{ID: 5, Title: "特斯拉降价3万元引发抢购", SourceKey: "baidu_hot"},
		{ID: 6, Title: "OpenAI发布GPT-5", SourceKey: "zhihu_hot"},
	}

	// collectHeatDiscoveredWords 应该发现"红包"(出现在4篇不同文章中)
	discovered := collectHeatDiscoveredWords(articles)

	fmt.Println("=== 热度反馈发现的词 ===")
	for word := range discovered {
		fmt.Printf("  发现: %q\n", word)
	}
	fmt.Println()

	// 验证"红包"被发现
	if _, ok := discovered["红包"]; !ok {
		t.Errorf("期望发现'红包'(出现在4篇文章中),但未发现。discovered=%v", discovered)
	}

	// 验证"降价"不会被误发现(只出现1次)
	if _, ok := discovered["降价"]; ok {
		t.Errorf("'降价'只出现1次,不应被发现")
	}

	// 完整流程验证:buildTrackerTopics 是否正确使用 heatDiscovered
	fmt.Println("=== 完整流程测试 ===")
	topics := buildTrackerTopics(articles, nil, 24, 20)
	foundRedBao := false
	for _, topic := range topics {
		fmt.Printf("  [%s] %s (count=%d)\n", topic.Kind, topic.Label, topic.Count)
		if topic.Label == "红包" {
			foundRedBao = true
		}
	}
	if !foundRedBao {
		t.Errorf("期望'红包'出现在 topics 中(4篇文章命中),但未找到")
	}
	fmt.Println()

	// 模拟"洁丽雅"场景:3篇不同文章提到
	articles2 := []model.Article{
		{ID: 10, Title: "洁丽雅晒报案回执，辟谣谣言", SourceKey: "zhihu_hot"},
		{ID: 11, Title: "洁丽雅毛巾品牌被曝质量问题", SourceKey: "weibo_hot"},
		{ID: 12, Title: "洁丽雅回应网友质疑", SourceKey: "baidu_hot"},
		{ID: 13, Title: "华为发布新款手机", SourceKey: "baidu_hot"},
	}
	discovered2 := collectHeatDiscoveredWords(articles2)
	fmt.Println("=== 洁丽雅场景 ===")
	for word := range discovered2 {
		fmt.Printf("  发现: %q\n", word)
	}
	// 注意:洁丽雅已经在词典中了,所以 collectHeatDiscoveredWords 会排除它(词典已覆盖)
	// 但如果我们移除词典中的洁丽雅,它应该能被发现
	fmt.Println("  (洁丽雅已在词典中,被 collectHeatDiscoveredWords 排除 — 符合设计)")

	// 测试一个不在词典里的3字词: "油柑"场景
	articles3 := []model.Article{
		{ID: 20, Title: "李显龙用中文点赞广西油柑", SourceKey: "weibo_hot"},
		{ID: 21, Title: "广西油柑成为东盟热门水果", SourceKey: "baidu_hot"},
		{ID: 22, Title: "油柑价格暴涨,果农笑开花", SourceKey: "zhihu_hot"},
	}
	discovered3 := collectHeatDiscoveredWords(articles3)
	fmt.Println("\n=== 油柑场景(不在词典) ===")
	for word := range discovered3 {
		fmt.Printf("  发现: %q\n", word)
	}
	if _, ok := discovered3["油柑"]; !ok {
		t.Errorf("期望发现'油柑'(出现在3篇文章中且不在词典),但未发现")
	}

	// 验证: buildTrackerTopics 使用后,"油柑"能出现在 topics 中
	topics3 := buildTrackerTopics(articles3, nil, 24, 20)
	fmt.Println("\n=== 油柑 topics ===")
	foundYouGan := false
	for _, topic := range topics3 {
		fmt.Printf("  [%s] %s (count=%d)\n", topic.Kind, topic.Label, topic.Count)
		if strings.Contains(topic.Label, "油柑") {
			foundYouGan = true
		}
	}
	if !foundYouGan {
		t.Errorf("期望'油柑'出现在 topics 中,但未找到")
	}
}

// TestHeatDiscoveryBigram 验证 bigram 合并能正确发现被 gse 切碎的复合词。
// 典型场景:
//   - "段永平" → gse 切为 ["段", "永平"],bigram 合并后发现 "段永平"
//   - "智商税" → gse 切为 ["智商", "税"],bigram 合并后发现 "智商税"
func TestHeatDiscoveryBigram(t *testing.T) {
	// 场景1: "段永平" 被 gse 切碎为 "段"+"永平"
	articles := []model.Article{
		{ID: 1, Title: "段永平回应价值投资理念", SourceKey: "baidu_hot"},
		{ID: 2, Title: "段永平捐款浙大引热议", SourceKey: "weibo_hot"},
		{ID: 3, Title: "段永平谈苹果股票持仓", SourceKey: "zhihu_hot"},
		{ID: 4, Title: "特斯拉降价促销", SourceKey: "baidu_hot"},
	}

	discovered := collectHeatDiscoveredWords(articles)
	fmt.Println("=== 段永平 bigram 测试 ===")
	for word := range discovered {
		fmt.Printf("  发现: %q\n", word)
	}

	// 期望:"段永平"被 bigram 合并发现
	if _, ok := discovered["段永平"]; !ok {
		t.Errorf("期望发现'段永平'(bigram 合并: 段+永平),但未发现。discovered=%v", mapKeys(discovered))
	}
	// "永平"作为碎片不应单独出现(被 bigram 替代)
	if _, ok := discovered["永平"]; ok {
		t.Errorf("'永平'应被 bigram '段永平' 替代,不应单独出现")
	}

	// 场景2: "智商税" 被 gse 切碎为 "智商"+"税"
	articles2 := []model.Article{
		{ID: 10, Title: "智商税骗局被曝光", SourceKey: "baidu_hot"},
		{ID: 11, Title: "这些保健品都是智商税", SourceKey: "weibo_hot"},
		{ID: 12, Title: "揭秘智商税营销套路", SourceKey: "zhihu_hot"},
		{ID: 13, Title: "华为发布新品", SourceKey: "baidu_hot"},
	}

	discovered2 := collectHeatDiscoveredWords(articles2)
	fmt.Println("\n=== 智商税 bigram 测试 ===")
	for word := range discovered2 {
		fmt.Printf("  发现: %q\n", word)
	}

	// 期望:"智商税"被 bigram 合并发现
	if _, ok := discovered2["智商税"]; !ok {
		t.Errorf("期望发现'智商税'(bigram 合并: 智商+税),但未发现。discovered=%v", mapKeys(discovered2))
	}
	// "智商"作为碎片:如果频次不高于"智商税",应被替代
	if _, ok := discovered2["智商"]; ok {
		// 在这些标题中"智商"和"智商税"频次相同,碎片应被替代
		t.Errorf("'智商'应被 bigram '智商税' 替代,不应单独出现")
	}
}

func mapKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
