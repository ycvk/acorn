package runtime

import (
	"context"
	"fmt"
	"io"
	"strings"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/domain"
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
	Appender  domain.EventAppender
	Sink      domain.StreamSink
	ToolInfos []*schema.ToolInfo
	CallSite  string
}

func streamAssistantMessage(
	ctx context.Context,
	model einomodel.BaseChatModel,
	messages []*schema.Message,
	opts assistantStreamOptions,
) (*AssistantStreamResult, error) {
	if model == nil {
		return nil, fmt.Errorf("assistant stream requires chat model")
	}
	streamOpts := make([]einomodel.Option, 0, 1)
	if len(opts.ToolInfos) > 0 {
		streamOpts = append(streamOpts, einomodel.WithTools(opts.ToolInfos))
	}
	callSite := opts.CallSite
	if callSite == "" {
		callSite = domain.CallSiteAssistant
	}
	modelStream, err := model.Stream(domain.WithCallSite(ctx, callSite), messages, streamOpts...)
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
			item := domain.StreamItem{
				RunID: opts.RunID,
				Kind:  domain.StreamKindAssistantDelta,
				Payload: map[string]any{
					"assistant_delta": &StreamAssistantDelta{
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
	return &AssistantStreamResult{
		Message:    finalMessage,
		StopReason: normalizeAssistantStopReason(finalMessage),
		RawReason:  assistantRawFinishReason(finalMessage),
	}, nil
}

func normalizeAssistantStopReason(message *schema.Message) AssistantStopReason {
	if message == nil {
		return AssistantStopReasonEndTurn
	}
	raw := assistantRawFinishReason(message)
	switch raw {
	case "", "stop", "end_turn", "null":
		if len(message.ToolCalls) > 0 {
			return AssistantStopReasonToolCalls
		}
		return AssistantStopReasonEndTurn
	case "tool_calls", "tool_use":
		return AssistantStopReasonToolCalls
	case "length", "max_tokens", "max_output_tokens", "model_context_window_exceeded":
		return AssistantStopReasonMaxOutput
	default:
		return AssistantStopReasonUnknown
	}
}

func assistantRawFinishReason(message *schema.Message) string {
	if message == nil || message.ResponseMeta == nil {
		return ""
	}
	return strings.TrimSpace(strings.ToLower(message.ResponseMeta.FinishReason))
}

func streamPlannedToolCalls(calls []schema.ToolCall) []StreamPlannedToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]StreamPlannedToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, StreamPlannedToolCall{
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
