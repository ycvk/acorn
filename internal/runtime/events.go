package runtime

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/core"
)

// agentEventProjector converts adk events into persisted StreamItems. It owns
// the per-run assistant message counter used for delta message ids.
type agentEventProjector struct {
	runID          string
	provider       string
	assistantCount int
}

func newAgentEventProjector(runID string, chatModel einomodel.BaseChatModel) *agentEventProjector {
	return &agentEventProjector{runID: runID, provider: activeProviderName(chatModel)}
}

func (p *agentEventProjector) project(event *adk.AgentEvent, emit func(core.StreamItem) error) error {
	now := time.Now().UTC()
	if event.Err != nil {
		return emit(core.StreamItem{Kind: core.StreamKindRunFailed, CreatedAt: now, Payload: map[string]any{"error": event.Err.Error()}})
	}
	if event.Output != nil && event.Output.MessageOutput != nil {
		mo := event.Output.MessageOutput
		switch mo.Role {
		case schema.Assistant:
			if err := p.projectAssistant(mo, emit); err != nil {
				return err
			}
		case schema.Tool:
			msg, err := mo.GetMessage()
			if err != nil {
				return fmt.Errorf("read tool result message: %w", err)
			}
			if err := emit(core.StreamItem{Kind: core.StreamKindToolCallSucceeded, CreatedAt: now, Payload: map[string]any{
				"tool_call_id": msg.ToolCallID,
				"tool_name":    msg.ToolName,
				"output":       msg.Content,
			}}); err != nil {
				return err
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

func (p *agentEventProjector) projectAssistant(mo *adk.MessageVariant, emit func(core.StreamItem) error) error {
	p.assistantCount++
	messageID := fmt.Sprintf("%s:assistant:%d", p.runID, p.assistantCount)
	final := mo.Message
	if mo.IsStreaming {
		concat, err := p.projectAssistantStream(mo.MessageStream, messageID, emit)
		if err != nil {
			return err
		}
		final = concat
	}
	if final == nil {
		return errors.New("assistant event carried no message")
	}
	return emit(core.StreamItem{Kind: core.StreamKindAssistantMessage, CreatedAt: time.Now().UTC(), Payload: map[string]any{
		"message": StreamMessageFromSchema(final, p.provider),
	}})
}

func (p *agentEventProjector) projectAssistantStream(stream *schema.StreamReader[*schema.Message], messageID string, emit func(core.StreamItem) error) (*schema.Message, error) {
	defer stream.Close()
	frames := make([]*schema.Message, 0, 16)
	seq := 0
	for {
		frame, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read assistant stream: %w", err)
		}
		frames = append(frames, frame)
		if frame.Content == "" && frame.ReasoningContent == "" && len(frame.ToolCalls) == 0 {
			continue
		}
		seq++
		if err := emit(core.StreamItem{Kind: core.StreamKindAssistantDelta, CreatedAt: time.Now().UTC(), Payload: map[string]any{
			"assistant_delta": &core.StreamAssistantDelta{
				Role:      string(schema.Assistant),
				Delta:     frame.Content,
				Reasoning: frame.ReasoningContent,
				Sequence:  seq,
				MessageID: messageID,
				ToolCalls: streamPlannedToolCalls(frame.ToolCalls),
				Meta:      streamMessageMeta(frame),
			},
		}}); err != nil {
			return nil, err
		}
	}
	if len(frames) == 0 {
		return nil, errors.New("assistant stream returned no frames")
	}
	concat, err := schema.ConcatMessages(frames)
	if err != nil {
		return nil, fmt.Errorf("concat assistant stream: %w", err)
	}
	return concat, nil
}

func streamPlannedToolCalls(calls []schema.ToolCall) []core.StreamPlannedToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]core.StreamPlannedToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, core.StreamPlannedToolCall{
			ID:            call.ID,
			Name:          call.Function.Name,
			ArgumentsJSON: call.Function.Arguments,
		})
	}
	return out
}

func streamMessageMeta(message *schema.Message) map[string]any {
	if message == nil {
		return nil
	}
	meta := make(map[string]any)
	if message.ResponseMeta != nil && message.ResponseMeta.FinishReason != "" {
		meta["finish_reason"] = message.ResponseMeta.FinishReason
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}
func activeProviderName(chatModel einomodel.BaseChatModel) string {
	if ap, ok := chatModel.(interface{ ActiveProvider() string }); ok {
		return ap.ActiveProvider()
	}
	return ""
}

func StreamMessageFromSchema(message *schema.Message, activeProvider string) *core.StreamMessage {
	if message == nil {
		return nil
	}
	stream := &core.StreamMessage{
		Role:       string(message.Role),
		Content:    strings.TrimSpace(message.Content),
		Reasoning:  strings.TrimSpace(message.ReasoningContent),
		ToolCallID: message.ToolCallID,
		ToolName:   message.ToolName,
	}
	meta := make(map[string]any)
	if activeProvider != "" {
		meta["active_provider"] = activeProvider
	}
	if len(message.ToolCalls) > 0 {
		stream.ToolCalls = make([]core.StreamPlannedToolCall, 0, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			stream.ToolCalls = append(stream.ToolCalls, core.StreamPlannedToolCall{
				ID:            call.ID,
				Name:          call.Function.Name,
				ArgumentsJSON: call.Function.Arguments,
			})
		}
	}
	if len(meta) > 0 {
		stream.Meta = meta
	}
	return stream
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
