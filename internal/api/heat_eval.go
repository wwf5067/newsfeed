package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/wwf5067/newsfeed/internal/model"
)

// 热词发现算法评估框架。
//
// 设计目标:
// 跑定时 job(每小时),用最近 24h 的文章 + 当前 heat_blacklist,
// 把"当前阈值"和"邻近候选阈值"分别跑一遍 collectHeatDiscoveredWords,
// 比较 precision(发现的词不在黑名单的比例)和 candidate_count(召回信号)。
// 把对比报告写到 heat_eval_reports 表,供运维参考是否调整代码常数。
//
// 不自动改阈值 — 自动化调参容易跑偏(垃圾词涌入或召回归零),需要人工把关。
// 这里只输出"建议",作为决策依据。

// HeatEvalRow 单组阈值的评估结果。
type HeatEvalRow struct {
	Params            HeatDiscoveryParams `json:"params"`
	DiscoveredCount   int                 `json:"discovered_count"`
	BlacklistHitCount int                 `json:"blacklist_hit_count"`
	Precision         float64             `json:"precision"` // = 1 - hit/discovered;NaN 时设 0
	Score             float64             `json:"score"`     // 综合分数 = precision * log(count+1)
}

// HeatEvalReport 一次完整评估的报告。
type HeatEvalReport struct {
	EvaluatedAt    time.Time     `json:"evaluated_at"`
	WindowHours    int           `json:"window_hours"`
	ArticlesCount  int           `json:"articles_count"`
	BlacklistCount int           `json:"blacklist_count"`
	Baseline       HeatEvalRow   `json:"baseline"`
	Candidates     []HeatEvalRow `json:"candidates"`
	BestVariant    string        `json:"best_variant"` // 描述,如 "minArticles=3,minSources=2"
	Suggestion     string        `json:"suggestion"`   // 建议文本,空 = 无须调整
}

// EvaluateHeatDiscovery 跑一次评估并返回报告。
//
// 输入:
//   - articles: 评估窗口内的文章(通常是最近 24h)
//   - blacklist: 当前 heat_blacklist 表的快照
//
// 算法:
//  1. 用 baseline params 跑 collectHeatDiscoveredWordsWithParams,得到 discovered set
//  2. 命中率:discovered 中有多少词在 blacklist
//  3. precision = 1 - hit / discovered_count
//  4. score = precision * log(count + 1)
//  5. 用 baseline ± 1 步长生成 8 个邻近候选,各跑一遍
//  6. 取 score 最高者:如果 score > baseline.score * 1.05(显著优于,5% 以上),写 suggestion
func EvaluateHeatDiscovery(articles []model.Article, blacklist []string) HeatEvalReport {
	blacklistSet := make(map[string]struct{}, len(blacklist))
	for _, w := range blacklist {
		blacklistSet[w] = struct{}{}
	}

	baseline := DefaultHeatDiscoveryParams()
	baselineRow := evalParams(articles, baseline, blacklistSet)

	candidates := generateCandidateParams(baseline)
	rows := make([]HeatEvalRow, 0, len(candidates))
	bestRow := baselineRow
	bestKey := paramsKey(baseline)
	for _, c := range candidates {
		row := evalParams(articles, c, blacklistSet)
		rows = append(rows, row)
		if row.Score > bestRow.Score {
			bestRow = row
			bestKey = paramsKey(c)
		}
	}

	suggestion := ""
	if bestKey != paramsKey(baseline) && bestRow.Score > baselineRow.Score*1.05 {
		suggestion = fmt.Sprintf(
			"候选 %s 综合分 %.4f 显著优于当前 baseline %.4f(precision %.3f→%.3f,候选词 %d→%d),建议人工评估调整。",
			bestKey, bestRow.Score, baselineRow.Score,
			baselineRow.Precision, bestRow.Precision,
			baselineRow.DiscoveredCount, bestRow.DiscoveredCount,
		)
	}

	return HeatEvalReport{
		EvaluatedAt:    time.Now(),
		WindowHours:    24, // 调用方传 24h 窗口的 articles 时此值描述输入
		ArticlesCount:  len(articles),
		BlacklistCount: len(blacklist),
		Baseline:       baselineRow,
		Candidates:     rows,
		BestVariant:    bestKey,
		Suggestion:     suggestion,
	}
}

// evalParams 单组阈值跑一遍 collectHeatDiscoveredWordsWithParams 并算指标。
func evalParams(articles []model.Article, p HeatDiscoveryParams, blacklistSet map[string]struct{}) HeatEvalRow {
	discovered := collectHeatDiscoveredWordsWithParams(articles, p)
	hits := 0
	for w := range discovered {
		if _, ok := blacklistSet[w]; ok {
			hits++
		}
	}
	count := len(discovered)
	precision := 0.0
	if count > 0 {
		precision = 1.0 - float64(hits)/float64(count)
	}
	// score = precision * log(count + 1):既考虑准(precision)也考虑全(count)
	// 防 count=0 时 log(1)=0 让 score 直接 0
	score := precision * math.Log1p(float64(count))
	return HeatEvalRow{
		Params:            p,
		DiscoveredCount:   count,
		BlacklistHitCount: hits,
		Precision:         precision,
		Score:             score,
	}
}

// generateCandidateParams 围绕 baseline 生成邻近候选。
//
// 只调 minArticles 和 minSources(其它常数 minHanLen/maxHanLen/maxBigramHanLen
// 调整影响过大,作为常量保留)。
//
// 8 组合 = (minArticles ∈ {b-1, b, b+1}) × (minSources ∈ {b-1, b, b+1}) - 1 (去重 baseline)
// 太少的下限被 evalParams 内 max(_, 1) 自动归正。
func generateCandidateParams(baseline HeatDiscoveryParams) []HeatDiscoveryParams {
	var out []HeatDiscoveryParams
	for da := -1; da <= 1; da++ {
		for ds := -1; ds <= 1; ds++ {
			if da == 0 && ds == 0 {
				continue // 跳过 baseline 自身
			}
			c := baseline
			c.MinArticles = baseline.MinArticles + da
			c.MinSources = baseline.MinSources + ds
			if c.MinArticles < 1 {
				c.MinArticles = 1
			}
			if c.MinSources < 1 {
				c.MinSources = 1
			}
			out = append(out, c)
		}
	}
	// 排序保证顺序稳定,日志/报告可读性更好
	sort.Slice(out, func(i, j int) bool {
		if out[i].MinArticles != out[j].MinArticles {
			return out[i].MinArticles < out[j].MinArticles
		}
		return out[i].MinSources < out[j].MinSources
	})
	return out
}

// paramsKey 一个参数组合的简短 key,用于报告里描述哪组最优。
func paramsKey(p HeatDiscoveryParams) string {
	return fmt.Sprintf("minArticles=%d,minSources=%d", p.MinArticles, p.MinSources)
}

// SaveHeatEvalReport 把评估报告写入 heat_eval_reports 表。
// 跟 EvaluateHeatDiscovery 分离是为了让前者可被纯 in-memory 测试。
type HeatEvalRepo interface {
	InsertHeatEvalReport(ctx context.Context, report HeatEvalReport) (int64, error)
}

// MarshalJSON helpers — 让 HeatDiscoveryParams 在 JSONB 里展开成对象而非 struct 标签默认形式。
// HeatDiscoveryParams 自身用 json.Marshal 默认行为已经够用,这里不重写,保持简单。

// EncodeHeatEvalRows 用于 SQL 写入 — 把 candidates 数组序列化成 JSON 字符串。
func EncodeHeatEvalRows(rows []HeatEvalRow) (string, error) {
	b, err := json.Marshal(rows)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// EncodeHeatEvalRow 单行序列化(给 baseline 用)。
func EncodeHeatEvalRow(row HeatEvalRow) (string, error) {
	b, err := json.Marshal(row)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
