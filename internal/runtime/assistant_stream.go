package runtime

import (
	"context"
	"fmt"
	"io"
	"strings"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/providerusage"
	"github.com/ycvk/acorn/internal/stream"
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
	Appender  EventAppender
	Sink      stream.StreamSink
	ToolInfos []*schema.ToolInfo
	CallSite  string
}

func streamAssistantMessage(
	ctx context.Context,
	model einomodel.BaseChatModel,
	messages []*schema.Message,
	opts assistantStreamOptions,
) (*orchestration.AssistantStreamResult, error) {
	if model == nil {
		return nil, fmt.Errorf("assistant stream requires chat model")
	}
	streamOpts := make([]einomodel.Option, 0, 1)
	if len(opts.ToolInfos) > 0 {
		streamOpts = append(streamOpts, einomodel.WithTools(opts.ToolInfos))
	}
	callSite := opts.CallSite
	if callSite == "" {
		callSite = providerusage.CallSiteAssistant
	}
	modelStream, err := model.Stream(providerusage.WithCallSite(ctx, callSite), messages, streamOpts...)
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
			item := stream.StreamItem{
				RunID: opts.RunID,
				Kind:  stream.StreamKindAssistantDelta,
				Payload: &stream.AssistantDeltaPayload{
					AssistantDelta: &stream.StreamAssistantDelta{
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
				if _, err := stream.AppendStreamItem(ctx, opts.Appender, opts.Sink, item); err != nil {
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
	return &orchestration.AssistantStreamResult{
		Message:    finalMessage,
		StopReason: normalizeAssistantStopReason(finalMessage),
		RawReason:  assistantRawFinishReason(finalMessage),
	}, nil
}

func normalizeAssistantStopReason(message *schema.Message) orchestration.AssistantStopReason {
	if message == nil {
		return orchestration.AssistantStopReasonEndTurn
	}
	raw := assistantRawFinishReason(message)
	switch raw {
	case "", "stop", "end_turn", "null":
		if len(message.ToolCalls) > 0 {
			return orchestration.AssistantStopReasonToolCalls
		}
		return orchestration.AssistantStopReasonEndTurn
	case "tool_calls", "tool_use":
		return orchestration.AssistantStopReasonToolCalls
	case "length", "max_tokens", "max_output_tokens", "model_context_window_exceeded":
		return orchestration.AssistantStopReasonMaxOutput
	default:
		return orchestration.AssistantStopReasonUnknown
	}
}

func assistantRawFinishReason(message *schema.Message) string {
	if message == nil || message.ResponseMeta == nil {
		return ""
	}
	return strings.TrimSpace(strings.ToLower(message.ResponseMeta.FinishReason))
}

func streamPlannedToolCalls(calls []schema.ToolCall) []stream.StreamPlannedToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]stream.StreamPlannedToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, stream.StreamPlannedToolCall{
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
