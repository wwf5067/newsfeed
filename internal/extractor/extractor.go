// Package extractor 从文章标题/正文中抽取实体与事件。
//
// 这一层只关心"抽出来什么",不关心存储、调度、并发。Runner 会拿着 Extractor
// 跑批次,Repository 负责落库。第一版用规则实现(rule.go),后续可换 LLM
// 实现而不动 Runner/Repository。
package extractor

import "context"

// 实体类型常量。type 字段约束在 schema 里只是 TEXT,这里用常量保持代码侧一致。
const (
	EntityPerson   = "person"
	EntityOrg      = "org"
	EntityLocation = "location"
	EntityWork     = "work"
	EntityOther    = "other"
)

// ExtractedEntity 是 extractor 输出的实体单元。
// 不包含 ID/时间戳——这些字段由 Repository 在 Upsert 时确定。
type ExtractedEntity struct {
	Name string
	Type string
}

// ExtractedEvent 是 extractor 输出的事件单元。
// Fingerprint 用于跨文章归并:同一指纹视为同一事件。
type ExtractedEvent struct {
	Title       string
	Fingerprint string
	Summary     string
}

// ExtractResult 一次抽取的全部产物。允许任一切片为空。
type ExtractResult struct {
	Entities []ExtractedEntity
	Events   []ExtractedEvent
}

// Extractor 是抽取实现的统一接口。
//
// Backend() 返回实现标识(rule|llm),用于日志和指标。
// Extract 应当是无副作用的纯函数,不持有外部资源。
type Extractor interface {
	Backend() string
	Extract(ctx context.Context, title, content string) (ExtractResult, error)
}
