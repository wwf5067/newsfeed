package api

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wwf5067/newsfeed/internal/model"
)

func TestEventClustering(t *testing.T) {
	articles := []model.Article{
		// 普京访华事件(应合并为一组: 共享 普京+俄罗斯 或 俄罗斯+中国)
		{ID: 1, Title: "普京即将启程访华，俄罗斯远东地区要融入大远东经济圈", SourceKey: "zhihu_hot", HeatValue: 3050000},
		{ID: 2, Title: "普京携5位副总理8位部长访华", SourceKey: "baidu_hot", HeatValue: 7520000},
		{ID: 3, Title: "首都国际机场高速两侧挂起中俄国旗", SourceKey: "baidu_hot", HeatValue: 7040000},
		{ID: 4, Title: "中俄关系继续沿着正确轨道不断发展", SourceKey: "baidu_hot", HeatValue: 7900000},

		// 泰国免签事件(应合并: 共享 泰国+免签)
		{ID: 10, Title: "泰国内阁决定取消60天免签政策", SourceKey: "baidu_hot", HeatValue: 7420000},
		{ID: 11, Title: "泰国终止60天免签", SourceKey: "weibo_hot", HeatValue: 930000},
		{ID: 12, Title: "泰国取消免签后旅游业影响几何", SourceKey: "zhihu_hot", HeatValue: 450000},

		// 世界杯名单(应合并: 共享 世界杯+内马尔 或 世界杯+巴西)
		{ID: 20, Title: "巴西公布世界杯 26 人名单，内马尔回归", SourceKey: "zhihu_hot", HeatValue: 880000},
		{ID: 21, Title: "葡萄牙公布世界杯名单", SourceKey: "weibo_hot", HeatValue: 380000},

		// 孤立文章(不应归入任何事件组)
		{ID: 30, Title: "茅台宣布部分产品涨价", SourceKey: "zhihu_hot", HeatValue: 860000},
		{ID: 31, Title: "广西车辆坠河致10人遇难", SourceKey: "baidu_hot", HeatValue: 6470000},
		{ID: 32, Title: "浪姐歌手对打", SourceKey: "weibo_hot", HeatValue: 350000},
	}

	// 先计算 heatDiscovered(和 buildTrackerTopics 一样)
	heatDiscovered := collectHeatDiscoveredWords(articles)
	events := clusterTrackerEvents(articles, heatDiscovered, 10)

	fmt.Println("========== 事件聚类结果 ==========")
	fmt.Println()
	for i, e := range events {
		fmt.Printf("[%d] %s\n", i+1, e.Title)
		fmt.Printf("    实体: %s\n", strings.Join(e.Entities, ", "))
		if len(e.Keywords) > 0 {
			fmt.Printf("    关键词: %s\n", strings.Join(e.Keywords, ", "))
		}
		fmt.Printf("    文章数: %d | 总热度: %d | 来源: ", e.Count, e.Score)
		for _, s := range e.Sources {
			fmt.Printf("%s(%d) ", s.SourceKey, s.Count)
		}
		fmt.Println()
		fmt.Printf("    Top文章:\n")
		for _, a := range e.Articles {
			fmt.Printf("      - [%s] %s (%d)\n", a.SourceKey, a.Title, a.HeatValue)
		}
		fmt.Println()
	}

	// 验证
	if len(events) < 2 {
		t.Fatalf("期望至少2个事件组,实际 %d", len(events))
	}

	// 验证普京访华被合并
	found := false
	for _, e := range events {
		hasP := false
		hasR := false
		for _, ent := range e.Entities {
			if ent == "普京" {
				hasP = true
			}
			if ent == "俄罗斯" {
				hasR = true
			}
		}
		if hasP && hasR && e.Count >= 2 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("期望'普京'和'俄罗斯'被合并为一个事件组")
	}

	// 验证泰国免签被合并
	foundThai := false
	for _, e := range events {
		hasThai := false
		for _, ent := range e.Entities {
			if ent == "泰国" {
				hasThai = true
			}
		}
		if hasThai && e.Count >= 2 {
			foundThai = true
			break
		}
	}
	if !foundThai {
		t.Errorf("期望泰国相关文章被合并为一个事件组")
	}

	// 验证孤立文章不归组
	for _, e := range events {
		for _, a := range e.Articles {
			if a.Title == "浪姐歌手对打" && e.Count == 1 {
				t.Errorf("'浪姐歌手对打'不应单独成为事件组(count=1)")
			}
		}
	}
}
