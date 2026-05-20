package extractor

import (
	"context"
	"errors"
)

// LLMExtractor 占位实现。等接入 LLM SDK 后再补完整逻辑——
// 接口和返回结构都已定义好,届时只需替换这里的 Extract 方法体。
type LLMExtractor struct {
	// APIKey、Endpoint 等字段后续按 SDK 需要再加
}

func NewLLMExtractor() *LLMExtractor { return &LLMExtractor{} }

func (LLMExtractor) Backend() string { return "llm" }

func (LLMExtractor) Extract(_ context.Context, _, _ string) (ExtractResult, error) {
	return ExtractResult{}, errors.New("llm extractor not implemented yet")
}
