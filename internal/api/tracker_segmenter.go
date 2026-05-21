package api

import (
	"log/slog"
	"strings"
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

// posSegmentTitle 带词性的分词。返回 (词, 词性) 对。
// 词性标记遵循 gse/jieba 标准: n=名词, v=动词, nr=人名, ns=地名, nt=机构名 等。
func posSegmentTitle(title string) []gse.SegPos {
	trackerSegOnce.Do(loadTrackerSegmenter)
	if trackerSegErr != nil {
		return nil
	}
	return trackerSeg.Pos(title, true)
}

// inferWordKind 根据 gse 词性标注推断热词应归类为 entity 还是 keyword。
// 规则:
//   - n/nr/ns/nt/nz/eng/nrt → "entity"(名词性→实体)
//   - v/vn/vd/vg → "keyword"(动词性→事件关键词)
//   - 其他/未知 → "keyword"(默认保守)
func inferWordKind(word string) string {
	segments := posSegmentTitle(word)
	if len(segments) == 0 {
		return "keyword"
	}
	// 取第一个片段的词性(短词通常只有一个片段)
	pos := ""
	for _, seg := range segments {
		text := seg.Text
		if strings.TrimSpace(text) == strings.TrimSpace(word) {
			pos = seg.Pos
			break
		}
	}
	if pos == "" && len(segments) > 0 {
		pos = segments[0].Pos
	}

	switch {
	case strings.HasPrefix(pos, "n"): // n, nr, ns, nt, nz, nrt...
		return "entity"
	case pos == "eng": // 英文
		return "entity"
	default:
		return "keyword"
	}
}

// InjectPromotedWords 运行时注入转正的候选词到 gse 和 trackerEntityLabelSet。
// 调用时机: 服务启动时 + 每次有新转正时。
// 线程安全: gse.AddToken 本身是线程安全的,CalcToken 需要在注入完成后调一次。
// promotedWordSet 记录已转正的热词(参与 gse 分词)。
// 用于前端区分"发现中"和"已转正"状态。
var promotedWordSet = map[string]struct{}{}

// IsPromotedWord 检查词是否已转正。
func IsPromotedWord(word string) bool {
	_, ok := promotedWordSet[word]
	return ok
}

func InjectPromotedWords(candidates []HeatCandidate) {
	trackerSegOnce.Do(loadTrackerSegmenter)
	if trackerSegErr != nil {
		return
	}
	for _, c := range candidates {
		// 注入 gse 词典让分词器整体切出
		_ = trackerSeg.AddToken(c.Word, 100, "n")
		// entity 类型额外注入 labelSet,让 shouldKeepTrackerToken 优先保留
		if c.Kind == "entity" {
			trackerEntityLabelSet[c.Word] = struct{}{}
		}
		// 记录转正状态
		promotedWordSet[c.Word] = struct{}{}
	}
	if len(candidates) > 0 {
		trackerSeg.CalcToken()
		slog.Info("injected promoted heat candidates into gse",
			"count", len(candidates))
	}
}

// RemovePromotedWord 从运行时词典中移除已转正的热词,用于黑名单删除时立即生效。
// 注意: gse 不支持动态删词,分词器可能仍会切出该词;但移除 trackerEntityLabelSet
// 后 shouldKeepTrackerToken 不再保留它,移除 promotedWordSet 后 IsPromotedWord
// 返回 false,不再参与权重加成。重启后 AddHeatBlacklist 已清除 promoted_at,
// ListPromotedCandidates 不会重新加载该词。
func RemovePromotedWord(word string) {
	delete(promotedWordSet, word)
	delete(trackerEntityLabelSet, word)
}
