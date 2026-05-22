package api

import (
	"strings"
	"testing"

	"github.com/wwf5067/newsfeed/internal/model"
)

// TestHeatDiscoveryParametrizedSameAsDefault 验证参数化版本传 default
// 跟原 collectHeatDiscoveredWords 行为一致(回归保护)。
func TestHeatDiscoveryParametrizedSameAsDefault(t *testing.T) {
	articles := []model.Article{
		{ID: 1, Title: "段永平再谈半导体", SourceKey: "zhihu_hot"},
		{ID: 2, Title: "段永平投资逻辑分享", SourceKey: "weibo_hot"},
		{ID: 3, Title: "段永平公益基金成立", SourceKey: "baidu_hot"},
		{ID: 4, Title: "智商税到底是什么", SourceKey: "zhihu_hot"},
		{ID: 5, Title: "网友质疑这是智商税", SourceKey: "weibo_hot"},
	}
	a := collectHeatDiscoveredWords(articles)
	b := collectHeatDiscoveredWordsWithParams(articles, DefaultHeatDiscoveryParams())
	if len(a) != len(b) {
		t.Fatalf("default params 行为不一致: orig %d vs params %d", len(a), len(b))
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			t.Errorf("orig 有 %q 但 params 没", k)
		}
	}
}

// TestEvaluateHeatDiscovery 评估流程能正常生成报告。
func TestEvaluateHeatDiscovery(t *testing.T) {
	articles := []model.Article{
		{ID: 1, Title: "段永平再谈半导体", SourceKey: "zhihu_hot"},
		{ID: 2, Title: "段永平投资逻辑分享", SourceKey: "weibo_hot"},
		{ID: 3, Title: "段永平公益基金成立", SourceKey: "baidu_hot"},
		{ID: 4, Title: "智商税到底是什么", SourceKey: "zhihu_hot"},
		{ID: 5, Title: "网友质疑这是智商税", SourceKey: "weibo_hot"},
	}
	blacklist := []string{"智商税"} // 假设用户把"智商税"加进了黑名单

	report := EvaluateHeatDiscovery(articles, blacklist)

	if report.ArticlesCount != 5 {
		t.Errorf("articles_count: got %d, want 5", report.ArticlesCount)
	}
	if report.BlacklistCount != 1 {
		t.Errorf("blacklist_count: got %d, want 1", report.BlacklistCount)
	}
	if report.Baseline.DiscoveredCount == 0 {
		t.Error("baseline 应该至少发现一些词")
	}
	// candidates 数量应该是 8(3x3 - baseline 自身)
	if len(report.Candidates) != 8 {
		t.Errorf("candidates 数: got %d, want 8", len(report.Candidates))
	}
	// best_variant 永远非空
	if report.BestVariant == "" {
		t.Error("best_variant 应该有值")
	}
	t.Logf("baseline: count=%d hit=%d precision=%.3f score=%.3f",
		report.Baseline.DiscoveredCount, report.Baseline.BlacklistHitCount,
		report.Baseline.Precision, report.Baseline.Score)
	t.Logf("best_variant: %s", report.BestVariant)
	if report.Suggestion != "" {
		t.Logf("suggestion: %s", report.Suggestion)
	}
}

// TestEncodeHeatEvalRows JSON 编码不丢字段(简单 round-trip)。
func TestEncodeHeatEvalRows(t *testing.T) {
	rows := []HeatEvalRow{
		{Params: DefaultHeatDiscoveryParams(), DiscoveredCount: 10, Score: 0.8},
	}
	s, err := EncodeHeatEvalRows(rows)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(s, "discovered_count") || !strings.Contains(s, "params") {
		t.Errorf("JSON 缺字段: %s", s)
	}
}
