package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/memory"
)

// compactionFilesKey keeps the files a compaction summary lists on the summary
// message, so the run's next compaction carries them forward.
const compactionFilesKey = "acorn_compaction_files"

// newCompactionMiddleware summarizes the run's messages once they exceed
// budget tokens. The summary has the sections memory.CompactionInstruction
// asks for, followed by the files the compacted tool calls read and changed.
func newCompactionMiddleware(ctx context.Context, model einomodel.AgenticModel, budget int, counter TokenCounter) (adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], error) {
	return summarization.NewTyped[*schema.AgenticMessage](ctx, &summarization.TypedConfig[*schema.AgenticMessage]{
		Model:           checkedCompactionModel{model},
		UserInstruction: memory.CompactionInstruction(),
		Trigger:         &summarization.TriggerCondition{ContextTokens: budget},
		TokenCounter: func(ctx context.Context, in *summarization.TypedTokenCounterInput[*schema.AgenticMessage]) (int, error) {
			return counter.CountMessages(ctx, in.Messages, in.Tools)
		},
		Finalize: finalizeCompaction,
		// A transient provider error, or a summary without its sections, would
		// otherwise fail the run.
		Retry: &summarization.TypedRetryConfig[*schema.AgenticMessage]{},
	})
}

// checkedCompactionModel fails a summary that does not have its sections, so
// the middleware asks again.
type checkedCompactionModel struct {
	einomodel.AgenticModel
}

func (m checkedCompactionModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...einomodel.Option) (*schema.AgenticMessage, error) {
	reply, err := m.AgenticModel.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	if err := memory.CheckSummary(summaryText(reply)); err != nil {
		return nil, fmt.Errorf("compaction summary: %w", err)
	}
	return reply, nil
}

// finalizeCompaction appends the files sections to the model's summary and
// replaces the compacted messages with it, as summarization.DefaultFinalize
// does.
func finalizeCompaction(ctx context.Context, original []*schema.AgenticMessage, summary *schema.AgenticMessage) ([]*schema.AgenticMessage, error) {
	files, err := compactedFiles(original)
	if err != nil {
		return nil, err
	}
	text := summaryText(summary) + "\n\n" + files.Sections()
	out, err := summarization.DefaultFinalize(ctx, original, &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{
		{Type: schema.ContentBlockTypeAssistantGenText, AssistantGenText: &schema.AssistantGenText{Text: text}},
	}})
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(files)
	if err != nil {
		return nil, err
	}
	out[len(out)-1].Extra[compactionFilesKey] = string(data)
	return out, nil
}

// summaryText joins a reply's text blocks the way the summarization
// middleware reads them.
func summaryText(msg *schema.AgenticMessage) string {
	var parts []string
	for _, block := range msg.ContentBlocks {
		if block.AssistantGenText != nil {
			parts = append(parts, block.AssistantGenText.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// compactedFiles lists the workstation files that messages read and changed:
// those an earlier compaction recorded, then the files and send_file calls in
// order, whatever their results, which the run may since have cleared.
func compactedFiles(messages []*schema.AgenticMessage) (memory.FileTouches, error) {
	var files memory.FileTouches
	for _, msg := range messages {
		if earlier, ok := msg.Extra[compactionFilesKey].(string); ok {
			var touches memory.FileTouches
			if err := json.Unmarshal([]byte(earlier), &touches); err != nil {
				return nil, fmt.Errorf("files of an earlier compaction: %w", err)
			}
			for _, touch := range touches {
				files = files.Touch(touch)
			}
		}
		for _, block := range msg.ContentBlocks {
			if call := block.FunctionToolCall; call != nil {
				if touch, ok := memory.TouchedFile(call.Name, call.Arguments); ok {
					files = files.Touch(touch)
				}
			}
		}
	}
	return files, nil
}
