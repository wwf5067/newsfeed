package api

import (
	"log/slog"
	"sync"

	"github.com/go-ego/gse"
)

// 中文分词单例。gse 默认词典 ~5MB,内嵌进 binary,首次切词时一次加载,之后零开销。
//
// 接入 extractTrackerCandidates 的目的是替代旧的"按标点切" — 中文标题里
// 几乎没有空格/标点,旧策略容易把整段标题当成一个 token 处理。
//
// 失败时 fallback 到旧策略(标点切分),保证 segmenter 加载不影响服务可用性。
var (
	trackerSegOnce sync.Once
	trackerSeg     gse.Segmenter
	trackerSegErr  error
)

func loadTrackerSegmenter() {
	if err := trackerSeg.LoadDictEmbed(); err != nil {
		trackerSegErr = err
		slog.Warn("gse load embed dict failed", "err", err)
		return
	}
	// 把 lexicon 主标签注入 user dict,让 gse 把"东方甄选/与辉同行/小米SU7" 整体切出。
	// 只注入 Label,不注入别名 — 别名变体太多会污染分词器,反而切错。
	for _, e := range trackerEntityLexicon {
		_ = trackerSeg.AddToken(e.Label, 100, "n")
	}
	// 重建 DAG 索引:AddToken 只是把词加进字典,不触发路由计算,
	// 必须调 CalcToken 让新词在后续 Cut 时生效。
	trackerSeg.CalcToken()
}

// segmentTitle 把标题切成词序列。
//
// 失败时(gse 加载失败或字典损坏)返回 nil,调用方按需 fallback。
// 注意 gse 默认会把英文/混合 token 转小写("OpenAI"→"openai"),
// 调用方需用 canonicalizeTrackerToken 还原大小写到 Label 形式。
func segmentTitle(title string) []string {
	trackerSegOnce.Do(loadTrackerSegmenter)
	if trackerSegErr != nil {
		return nil
	}
	return trackerSeg.Cut(title, true)
}
