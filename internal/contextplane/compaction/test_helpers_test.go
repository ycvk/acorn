package compaction

import (
	"context"
	"sync"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/localit-io/tiktoken-go"
	tiktokenloader "github.com/pkoukk/tiktoken-go-loader"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
)

type testCompactionEngine struct {
	called  bool
	calls   int
	request CompactRequest
	results []*CompactionResult
	result  *CompactionResult
	err     error
}

func (e *testCompactionEngine) Compact(_ context.Context, req CompactRequest) (*CompactionResult, error) {
	e.called = true
	e.calls++
	e.request = req
	if e.err != nil {
		return nil, e.err
	}
	if len(e.results) > 0 {
		index := e.calls - 1
		if index >= len(e.results) {
			index = len(e.results) - 1
		}
		return e.results[index], nil
	}
	return e.result, nil
}

type testBudgetGovernor struct {
	pressure contextplane.BudgetPressure
	dynamic  bool
}

func (g testBudgetGovernor) Evaluate(_ context.Context, req contextplane.BudgetEvaluateRequest) (contextplane.BudgetPressure, error) {
	if g.dynamic && len(req.Messages) <= 3 {
		p := g.pressure
		p.State = contextplane.PressureOK
		return p, nil
	}
	return g.pressure, nil
}

func testPressure(state contextplane.BudgetPressureState) contextplane.BudgetPressure {
	return contextplane.BudgetPressure{
		EffectiveWindowTokens: 1000,
		State:                 state,
	}
}

func testTokenCounter(t *testing.T) *contextplane.CompressionTokenCounter {
	t.Helper()
	counter, err := contextplane.NewCompressionTokenCounter(config.ContextConfig{TokenEncoding: "o200k_base"})
	if err != nil {
		t.Fatalf("NewCompressionTokenCounter: %v", err)
	}
	return counter
}

func testContextSessionProfile() contextplane.ModelProfile {
	return contextplane.ModelProfile{
		ContextWindowTokens:         200000,
		ReservedOutputTokens:        4096,
		ReservedSummaryOutputTokens: 2048,
		StaticOverheadTokens:        4096,
		WarningBufferTokens:         20000,
		AutoCompactBufferTokens:     13000,
	}
}

var testCompressionTokenLoaderOnce sync.Once

func ensureCompressionTokenLoader() error {
	testCompressionTokenLoaderOnce.Do(func() {
		tiktoken.SetBpeLoader(tiktokenloader.NewOfflineLoader())
	})
	return nil
}

func normalizeCompressionMessage(msg adk.Message) *schema.Message {
	if msg == nil {
		return &schema.Message{}
	}
	return &schema.Message{
		Role:                     msg.Role,
		Content:                  msg.Content,
		UserInputMultiContent:    append([]schema.MessageInputPart(nil), msg.UserInputMultiContent...),
		AssistantGenMultiContent: append([]schema.MessageOutputPart(nil), msg.AssistantGenMultiContent...),
		Name:                     msg.Name,
		ToolCalls:                append([]schema.ToolCall(nil), msg.ToolCalls...),
		ToolCallID:               msg.ToolCallID,
		ToolName:                 msg.ToolName,
		ReasoningContent:         msg.ReasoningContent,
	}
}

func normalizeCompressionTool(tool *schema.ToolInfo) *schema.ToolInfo {
	if tool == nil {
		return &schema.ToolInfo{}
	}
	clone := *tool
	clone.Extra = nil
	return &clone
}
