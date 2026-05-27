package runtime

import (
	"context"
	"fmt"
	"io"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/providers"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/stream"
)

func streamAssistantInterleaved(
	ctx context.Context,
	model einomodel.BaseChatModel,
	messages []*schema.Message,
	opts assistantStreamOptions,
) *orchestration.InterleavedStream {
	streamOpts := make([]einomodel.Option, 0, 1)
	if len(opts.ToolInfos) > 0 {
		streamOpts = append(streamOpts, einomodel.WithTools(opts.ToolInfos))
	}
	callSite := opts.CallSite
	if callSite == "" {
		callSite = providers.CallSiteAssistant
	}
	modelStream, err := model.Stream(providers.WithCallSite(ctx, callSite), messages, streamOpts...)

	s := &orchestration.InterleavedStream{
		ToolCallCh:     make(chan schema.ToolCall, 8),
		FinalMessageCh: make(chan orchestration.AssistantStreamResult, 1),
		ErrCh:          make(chan error, 1),
	}

	go func() {
		defer close(s.ToolCallCh)
		defer close(s.FinalMessageCh)
		defer close(s.ErrCh)

		if err != nil {
			select {
			case s.ErrCh <- err:
			case <-ctx.Done():
			}
			return
		}
		if modelStream == nil {
			select {
			case s.ErrCh <- fmt.Errorf("assistant stream returned nil stream"):
			case <-ctx.Done():
			}
			return
		}
		defer modelStream.Close()

		accumulator := newAssistantStreamAccumulator(opts.MessageID)
		frames := make([]*schema.Message, 0, 4)
		sink := stream.StreamSinkFromContext(ctx)

		for {
			frame, recvErr := modelStream.Recv()
			if recvErr != nil {
				if recvErr == io.EOF {
					break
				}
				select {
				case s.ErrCh <- recvErr:
				case <-ctx.Done():
				}
				return
			}
			if frame == nil {
				select {
				case s.ErrCh <- fmt.Errorf("assistant stream returned nil frame"):
				case <-ctx.Done():
				}
				return
			}
			frames = append(frames, frame)

			if frame.Content == "" && frame.ReasoningContent == "" && len(frame.ToolCalls) == 0 {
				continue
			}
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
				if _, appendErr := stream.AppendStreamItem(ctx, opts.Appender, opts.Sink, item); appendErr != nil {
					select {
					case s.ErrCh <- appendErr:
					case <-ctx.Done():
					}
					return
				}
			} else if sink != nil {
				if sinkErr := sink(item); sinkErr != nil {
					select {
					case s.ErrCh <- sinkErr:
					case <-ctx.Done():
					}
					return
				}
			}
		}

		if len(frames) == 0 {
			select {
			case s.ErrCh <- fmt.Errorf("assistant stream returned no frames"):
			case <-ctx.Done():
			}
			return
		}

		finalMessage, concatErr := schema.ConcatMessages(frames)
		if concatErr != nil {
			select {
			case s.ErrCh <- fmt.Errorf("concat assistant stream: %w", concatErr):
			case <-ctx.Done():
			}
			return
		}

		// Tool call chunks from the stream are merged by Eino's ConcatMessages
		// into complete calls before dispatch to avoid partial-argument execution.
		for _, tc := range finalMessage.ToolCalls {
			select {
			case s.ToolCallCh <- tc:
			case <-ctx.Done():
				return
			}
		}

		select {
		case s.FinalMessageCh <- orchestration.AssistantStreamResult{
			Message:    finalMessage,
			StopReason: normalizeAssistantStopReason(finalMessage),
			RawReason:  assistantRawFinishReason(finalMessage),
		}:
		case <-ctx.Done():
		}
	}()

	return s
}

type directAssistantStreamer struct {
	appender runtimeapi.EventAppender
}

func newDirectAssistantStreamer(appender runtimeapi.EventAppender) *directAssistantStreamer {
	return &directAssistantStreamer{appender: appender}
}

func (s *directAssistantStreamer) StreamAssistantMessage(ctx context.Context, req orchestration.AssistantStreamRequest) (*orchestration.AssistantStreamResult, error) {
	return streamAssistantMessage(ctx, req.Model, req.Messages, assistantStreamOptions{
		MessageID: req.MessageID,
		RunID:     req.RunID,
		Appender:  s.appender,
		Sink:      stream.StreamSinkFromContext(ctx),
		ToolInfos: req.ToolInfos,
		CallSite:  req.CallSite,
	})
}

func (s *directAssistantStreamer) StreamAssistantInterleaved(ctx context.Context, req orchestration.AssistantStreamRequest) *orchestration.InterleavedStream {
	return streamAssistantInterleaved(ctx, req.Model, req.Messages, assistantStreamOptions{
		MessageID: req.MessageID,
		RunID:     req.RunID,
		Appender:  s.appender,
		Sink:      stream.StreamSinkFromContext(ctx),
		ToolInfos: req.ToolInfos,
		CallSite:  req.CallSite,
	})
}
