package memory

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// MeterCompaction accounts for every Eino summarization attempt, including
// retries, in the current run's budget. The native protocol is preserved.
func (e *Engine) MeterCompaction(model einomodel.AgenticModel) einomodel.AgenticModel {
	return &compactionModel{AgenticModel: model, engine: e}
}

type compactionModel struct {
	einomodel.AgenticModel
	engine *Engine
}

func (m *compactionModel) Generate(ctx context.Context, messages []*schema.AgenticMessage, options ...einomodel.Option) (*schema.AgenticMessage, error) {
	data, err := json.Marshal(messages)
	if err != nil {
		return nil, err
	}
	n, err := m.engine.cfg.Count(ctx, string(data))
	if err != nil {
		return nil, err
	}
	const maxOutput = 4096
	usage, err := m.engine.reserve(ctx, "context_compaction", m.engine.cfg.ModelName, n, maxOutput)
	if err != nil {
		return nil, err
	}
	opts := append(append([]einomodel.Option(nil), options...), einomodel.WithMaxTokens(maxOutput))
	reply, callErr := m.AgenticModel.Generate(ctx, messages, opts...)
	if callErr == nil && reply != nil {
		if reply.ResponseMeta != nil && reply.ResponseMeta.TokenUsage != nil {
			u := reply.ResponseMeta.TokenUsage
			usage.InputTokens = u.PromptTokens
			usage.OutputTokens = u.CompletionTokens
			usage.Reported = true
		} else {
			var text strings.Builder
			for _, block := range reply.ContentBlocks {
				if block.AssistantGenText != nil {
					text.WriteString(block.AssistantGenText.Text)
				}
			}
			usage.OutputTokens, err = m.engine.cfg.Count(ctx, text.String())
			if err != nil {
				return nil, err
			}
		}
	}
	return reply, errors.Join(callErr, m.engine.cfg.Store.RecordMemoryUsage(context.WithoutCancel(ctx), usage))
}
