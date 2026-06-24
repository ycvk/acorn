package runtime

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/core"
)

type assistantStreamAccumulator struct {
	messageID string
	nextSeq   int
	content   strings.Builder
}

func newAssistantStreamAccumulator(messageID string) *assistantStreamAccumulator {
	return &assistantStreamAccumulator{
		messageID: strings.TrimSpace(messageID),
		nextSeq:   1,
	}
}

func (a *assistantStreamAccumulator) append(delta string) int {
	a.content.WriteString(delta)
	seq := a.nextSeq
	a.nextSeq++
	return seq
}

type assistantStreamOptions struct {
	MessageID string
	RunID     string
	Appender  core.EventAppender
	Sink      core.StreamSink
	ToolInfos []*schema.ToolInfo
	CallSite  string
}

func streamAssistantMessage(
	ctx context.Context,
	model einomodel.BaseChatModel,
	messages []*schema.Message,
	opts assistantStreamOptions,
) (*core.AssistantStreamResult, error) {
	if model == nil {
		return nil, fmt.Errorf("assistant stream requires chat model")
	}
	streamOpts := make([]einomodel.Option, 0, 1)
	if len(opts.ToolInfos) > 0 {
		streamOpts = append(streamOpts, einomodel.WithTools(opts.ToolInfos))
	}
	callSite := opts.CallSite
	if callSite == "" {
		callSite = core.CallSiteAssistant
	}
	modelStream, err := model.Stream(core.WithCallSite(ctx, callSite), messages, streamOpts...)
	if err != nil {
		return nil, err
	}
	if modelStream == nil {
		return nil, fmt.Errorf("assistant stream returned nil stream")
	}
	defer modelStream.Close()

	accumulator := newAssistantStreamAccumulator(opts.MessageID)
	frames := make([]*schema.Message, 0, 4)

	for {
		frame, recvErr := modelStream.Recv()
		if recvErr != nil {
			if recvErr == io.EOF {
				break
			}
			return nil, recvErr
		}
		if frame == nil {
			return nil, fmt.Errorf("assistant stream returned nil frame")
		}
		frames = append(frames, frame)
		if frame.Content == "" && frame.ReasoningContent == "" && len(frame.ToolCalls) == 0 {
			continue
		}
		if opts.Appender != nil || opts.Sink != nil {
			sequence := accumulator.append(frame.Content)
			item := core.StreamItem{
				RunID: opts.RunID,
				Kind:  core.StreamKindAssistantDelta,
				Payload: map[string]any{
					"assistant_delta": &core.StreamAssistantDelta{
						Role:      string(frame.Role),
						Delta:     frame.Content,
						Reasoning: frame.ReasoningContent,
						Sequence:  sequence,
						MessageID: accumulator.messageID,
						ToolCalls: streamPlannedToolCalls(frame.ToolCalls),
						Meta:      streamMessageMeta(frame),
					},
				},
			}
			if opts.Appender != nil {
				if _, err := AppendStreamItem(ctx, opts.Appender, opts.Sink, item); err != nil {
					return nil, err
				}
				continue
			}
			if opts.Sink != nil {
				if err := opts.Sink(item); err != nil {
					return nil, err
				}
			}
		}
	}

	if len(frames) == 0 {
		return nil, fmt.Errorf("assistant stream returned no frames")
	}
	finalMessage, err := schema.ConcatMessages(frames)
	if err != nil {
		return nil, fmt.Errorf("concat assistant stream: %w", err)
	}
	return &core.AssistantStreamResult{
		Message:    finalMessage,
		StopReason: normalizeAssistantStopReason(finalMessage),
		RawReason:  assistantRawFinishReason(finalMessage),
	}, nil
}

func normalizeAssistantStopReason(message *schema.Message) core.AssistantStopReason {
	if message == nil {
		return core.AssistantStopReasonEndTurn
	}
	raw := assistantRawFinishReason(message)
	switch raw {
	case "", "stop", "end_turn", "null":
		if len(message.ToolCalls) > 0 {
			return core.AssistantStopReasonToolCalls
		}
		return core.AssistantStopReasonEndTurn
	case "tool_calls", "tool_use":
		return core.AssistantStopReasonToolCalls
	case "length", "max_tokens", "max_output_tokens", "model_context_window_exceeded":
		return core.AssistantStopReasonMaxOutput
	default:
		return core.AssistantStopReasonUnknown
	}
}

func assistantRawFinishReason(message *schema.Message) string {
	if message == nil || message.ResponseMeta == nil {
		return ""
	}
	return strings.TrimSpace(strings.ToLower(message.ResponseMeta.FinishReason))
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

func StreamItemsFromAgentEvent(event *adk.AgentEvent, chatModel einomodel.BaseChatModel) []core.StreamItem {
	items := make([]core.StreamItem, 0, 3)
	createdAt := time.Now().UTC()
	if event.Output != nil && event.Output.MessageOutput != nil {
		if message, err := event.Output.MessageOutput.GetMessage(); err == nil && message != nil {
			items = append(items, core.StreamItem{
				Kind:      core.StreamKindAssistantMessage,
				CreatedAt: createdAt,
				Payload: map[string]any{
					"message": StreamMessageFromSchema(message, activeProviderName(chatModel)),
				},
			})
		}
	}
	if event.Action != nil && event.Action.Interrupted != nil {
		items = append(items, core.StreamItem{
			Kind:      core.StreamKindRunInterrupted,
			CreatedAt: createdAt,
			Payload: map[string]any{
				"interrupt": streamInterruptFromInfo(event.Action.Interrupted),
			},
		})
	}
	if event.Err != nil {
		items = append(items, core.StreamItem{
			Kind:      core.StreamKindRunFailed,
			CreatedAt: createdAt,
			Payload: map[string]any{
				"error": event.Err.Error(),
			},
		})
	}
	return items
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
