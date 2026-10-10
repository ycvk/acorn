package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/memory"
)

type memoryModel struct {
	model einomodel.AgenticModel
	cfg   *config.Config
	mu    sync.Mutex
}

func NewMemoryModel(ctx context.Context, cfg *config.Config) (memory.Model, error) {
	if cfg == nil {
		return nil, errors.New("memory model requires config")
	}
	return &memoryModel{cfg: cfg}, nil
}

func (m *memoryModel) GenerateMemory(ctx context.Context, instruction, input string, maxOutput int) (memory.Generation, error) {
	if err := m.cfg.ValidateExecutionReady(); err != nil {
		return memory.Generation{}, err
	}
	m.mu.Lock()
	if m.model == nil {
		model, err := newChatModel(ctx, m.cfg)
		if err != nil {
			m.mu.Unlock()
			return memory.Generation{}, err
		}
		m.model = model
	}
	model := m.model
	m.mu.Unlock()
	reply, err := model.Generate(ctx, []*schema.AgenticMessage{schema.SystemAgenticMessage(instruction), schema.UserAgenticMessage(input)}, einomodel.WithMaxTokens(maxOutput))
	if err != nil {
		return memory.Generation{}, err
	}
	if reply == nil {
		return memory.Generation{}, errors.New("memory model returned no message")
	}
	var text strings.Builder
	for _, block := range reply.ContentBlocks {
		if block.Type == schema.ContentBlockTypeAssistantGenText {
			text.WriteString(block.AssistantGenText.Text)
		}
	}
	result := memory.Generation{Text: text.String()}
	if reply.ResponseMeta != nil && reply.ResponseMeta.TokenUsage != nil {
		usage := reply.ResponseMeta.TokenUsage
		result.InputTokens = usage.PromptTokens
		result.OutputTokens = usage.CompletionTokens
		result.Reported = true
	}
	return result, nil
}
