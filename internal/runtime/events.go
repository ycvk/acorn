package runtime

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/core"
)

// agentEventProjector converts adk events into persisted StreamItems. It owns
// the per-run assistant message counter used for delta message ids.
type agentEventProjector struct {
	// messagePrefix is unique per projector so a resumed run's assistant
	// messages never reuse the ids of the messages before the interrupt.
	messagePrefix  string
	provider       string
	failedCalls    *failedToolCalls
	assistantCount int
	// streamErr is the first assistant stream read failure. Eino follows a
	// failed stream with an error event; streamErr fails the run if it does not.
	streamErr   error
	usageWarned bool
}

func newAgentEventProjector(runID string, chatModel einomodel.AgenticModel, failedCalls *failedToolCalls) *agentEventProjector {
	return &agentEventProjector{
		messagePrefix: fmt.Sprintf("%s:assistant:%d", runID, time.Now().UnixNano()),
		provider:      activeProviderName(chatModel),
		failedCalls:   failedCalls,
	}
}

// streamReadError marks a failure while reading an assistant stream, which
// the projector records instead of aborting event consumption.
type streamReadError struct{ err error }

func (e *streamReadError) Error() string { return "read assistant stream: " + e.err.Error() }

func (e *streamReadError) Unwrap() error { return e.err }

func (p *agentEventProjector) project(event *adk.TypedAgentEvent[*schema.AgenticMessage], emit func(core.StreamItem) error) error {
	now := time.Now().UTC()
	if event.Err != nil {
		return emit(core.StreamItem{Kind: core.StreamKindRunFailed, CreatedAt: now, Payload: map[string]any{"error": event.Err.Error()}})
	}
	if event.Output != nil && event.Output.MessageOutput != nil {
		mo := event.Output.MessageOutput
		switch mo.AgenticRole {
		case schema.AgenticRoleTypeAssistant:
			if err := p.projectAssistant(mo, emit); err != nil {
				return err
			}
		case schema.AgenticRoleTypeUser:
			msg, err := mo.GetMessage()
			if err != nil {
				return fmt.Errorf("read tool result message: %w", err)
			}
			for _, block := range msg.ContentBlocks {
				if item, ok := p.toolResultItem(block, now); ok {
					if err := emit(item); err != nil {
						return err
					}
				}
			}
		}
	}
	if event.Action != nil && event.Action.Interrupted != nil {
		return emit(core.StreamItem{Kind: core.StreamKindRunInterrupted, CreatedAt: now, Payload: map[string]any{
			"interrupt": streamInterruptFromInfo(event.Action.Interrupted),
		}})
	}
	return nil
}

func (p *agentEventProjector) toolResultItem(block *schema.ContentBlock, now time.Time) (core.StreamItem, bool) {
	var id, name, output string
	switch block.Type {
	case schema.ContentBlockTypeFunctionToolResult:
		result := block.FunctionToolResult
		id, name = result.CallID, result.Name
		for _, part := range result.Content {
			if part.Text != nil {
				output += part.Text.Text
			}
		}
	case schema.ContentBlockTypeToolSearchResult:
		result := block.ToolSearchFunctionToolResult
		id, name, output = result.CallID, result.Name, result.String()
	default:
		return core.StreamItem{}, false
	}
	if errText, failed := p.failedCalls.take(id); failed {
		return core.StreamItem{Kind: core.StreamKindToolCallFailed, CreatedAt: now, Payload: map[string]any{
			"tool_call_id": id, "tool_name": name, "error": errText,
		}}, true
	}
	return core.StreamItem{Kind: core.StreamKindToolCallSucceeded, CreatedAt: now, Payload: map[string]any{
		"tool_call_id": id, "tool_name": name, "output": output,
	}}, true
}

func (p *agentEventProjector) projectAssistant(mo *adk.TypedMessageVariant[*schema.AgenticMessage], emit func(core.StreamItem) error) error {
	p.assistantCount++
	messageID := fmt.Sprintf("%s:%d", p.messagePrefix, p.assistantCount)
	final := mo.Message
	if mo.IsStreaming {
		concat, err := p.projectAssistantStream(mo.MessageStream, messageID, emit)
		var retrying *adk.WillRetryError
		if errors.As(err, &retrying) {
			return nil // the retry's assistant event carries the message
		}
		var readErr *streamReadError
		if errors.As(err, &readErr) {
			if p.streamErr == nil {
				p.streamErr = readErr
			}
			return nil
		}
		if err != nil {
			return err
		}
		final = concat
	}
	if final == nil {
		return errors.New("assistant event carried no message")
	}
	if err := emit(core.StreamItem{Kind: core.StreamKindAssistantMessage, CreatedAt: time.Now().UTC(), Payload: map[string]any{
		"message": StreamMessageFromSchema(final, p.provider),
	}}); err != nil {
		return err
	}
	payload := map[string]any{"reported": false}
	if final.ResponseMeta != nil && final.ResponseMeta.TokenUsage != nil {
		u := final.ResponseMeta.TokenUsage
		payload = map[string]any{"reported": true, "prompt_tokens": u.PromptTokens, "completion_tokens": u.CompletionTokens, "total_tokens": u.TotalTokens}
	} else if !p.usageWarned {
		slog.Warn("model did not report token usage", "run", p.messagePrefix)
		p.usageWarned = true
	}
	return emit(core.StreamItem{Kind: core.StreamItemKind(core.EventModelUsage), CreatedAt: time.Now().UTC(), Payload: payload})
}

func (p *agentEventProjector) projectAssistantStream(stream *schema.StreamReader[*schema.AgenticMessage], messageID string, emit func(core.StreamItem) error) (*schema.AgenticMessage, error) {
	defer stream.Close()
	frames := make([]*schema.AgenticMessage, 0, 16)
	seq := 0
	for {
		frame, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, &streamReadError{err: err}
		}
		frames = append(frames, frame)
		public := StreamMessageFromSchema(frame, p.provider)
		if public.Content == "" && public.Reasoning == "" && len(public.ToolCalls) == 0 {
			continue
		}
		seq++
		if err := emit(core.StreamItem{Kind: core.StreamKindAssistantDelta, CreatedAt: time.Now().UTC(), Payload: map[string]any{
			"assistant_delta": &core.StreamAssistantDelta{
				Role:      string(schema.Assistant),
				Delta:     public.Content,
				Reasoning: public.Reasoning,
				Sequence:  seq,
				MessageID: messageID,
				ToolCalls: public.ToolCalls,
				Meta:      streamMessageMeta(frame),
			},
		}}); err != nil {
			return nil, err
		}
	}
	if len(frames) == 0 {
		return nil, errors.New("assistant stream returned no frames")
	}
	concat, err := schema.ConcatAgenticMessages(frames)
	if err != nil {
		return nil, fmt.Errorf("concat assistant stream: %w", err)
	}
	return concat, nil
}

func activeProviderName(chatModel einomodel.AgenticModel) string {
	if ap, ok := chatModel.(interface{ ActiveProvider() string }); ok {
		return ap.ActiveProvider()
	}
	return ""
}

func streamInterruptFromInfo(info *adk.InterruptInfo) *core.StreamInterrupt {
	if info == nil {
		return nil
	}
	interrupt := &core.StreamInterrupt{ContextCount: len(info.InterruptContexts), Contexts: make([]core.StreamInterruptContext, 0, len(info.InterruptContexts))}
	for _, item := range info.InterruptContexts {
		interrupt.Contexts = append(interrupt.Contexts, core.StreamInterruptContext{
			ID:          item.ID,
			Address:     fmt.Sprint(item.Address),
			Info:        core.CompactInterruptInfo(item.Info),
			IsRootCause: item.IsRootCause,
		})
	}
	return interrupt
}
