package runtime

import (
	"context"
	"errors"
	"fmt"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/tools/dispatch"
)

type RoundOptions struct {
	CallSite       string
	BeforeToolCall func(context.Context, schema.ToolCall) error
}

type ToolCallRejectedError struct {
	Call schema.ToolCall
	Err  error
}

func (e *ToolCallRejectedError) Error() string {
	return fmt.Sprintf("agent loop rejected tool call %q (%s) before execution: %v", e.Call.ID, e.Call.Function.Name, e.Err)
}

func (e *ToolCallRejectedError) Unwrap() error {
	return e.Err
}

type ToolExecutionError struct {
	Err error
}

func (e *ToolExecutionError) Error() string {
	return fmt.Sprintf("agent loop get tool results: %v", e.Err)
}

func (e *ToolExecutionError) Unwrap() error {
	return e.Err
}

func ExecuteRound(ctx context.Context, model einomodel.BaseChatModel, streamer core.AssistantStreamer, toolNode dispatch.ToolInvoker, messages []*schema.Message, toolInfos []*schema.ToolInfo, runID string, messageID string, opts RoundOptions) (*schema.Message, []*schema.Message, bool, error) {
	interleaved := streamer.StreamAssistantInterleaved(ctx, core.AssistantStreamRequest{
		RunID:     runID,
		MessageID: messageID,
		Model:     model,
		Messages:  messages,
		ToolInfos: toolInfos,
		CallSite:  opts.CallSite,
	})

	executor := toolNode.NewStreamingExecutor(ctx)
	result, err := consumeInterleavedForAgentLoop(ctx, interleaved, executor, opts.BeforeToolCall)
	// A ToolCallRejectedError means BeforeToolCall blocked one call in a
	// batch. Already-submitted calls should still return results, so we
	// must NOT Discard. Collect their results and surface the rejection.
	var rejected *ToolCallRejectedError
	if err != nil && errors.As(err, &rejected) {
		var msg *schema.Message
		var toolMessages []*schema.Message
		if result != nil && result.Message != nil {
			msg = result.Message
		}
		// Collect results from already-submitted calls. If none were
		// submitted (rejection on the first call), GetRemainingResults
		// returns an error — treat that as "no results" not a failure.
		tm, tmErr := executor.GetRemainingResults(ctx)
		if tmErr != nil {
			// No submitted calls to collect; the rejection is the
			// only signal the agent needs.
			toolMessages = nil
		} else {
			toolMessages = tm
		}
		return msg, toolMessages, false, err
	}
	if err != nil {
		executor.Discard()
		if result != nil && result.Message != nil {
			return result.Message, nil, false, err
		}
		return nil, nil, false, err
	}
	if result == nil || result.Message == nil {
		return nil, nil, false, errors.New("agent loop assistant stream returned nil message")
	}

	msg := result.Message
	switch result.StopReason {
	case core.AssistantStopReasonMaxOutput:
		executor.Discard()
		return assistantMessageWithoutToolCalls(msg), nil, true, nil
	case core.AssistantStopReasonEndTurn, core.AssistantStopReasonToolCalls:
	case core.AssistantStopReasonUnknown:
		executor.Discard()
		return msg, nil, false, fmt.Errorf("agent loop unsupported assistant finish reason %q", result.RawReason)
	default:
		executor.Discard()
		return msg, nil, false, fmt.Errorf("agent loop unsupported assistant stop reason %q", result.StopReason)
	}
	if len(msg.ToolCalls) == 0 {
		return msg, nil, false, nil
	}

	toolMessages, err := executor.GetRemainingResults(ctx)
	if err != nil {
		return msg, nil, false, &ToolExecutionError{Err: err}
	}

	return msg, toolMessages, false, nil
}

func assistantMessageWithoutToolCalls(msg *schema.Message) *schema.Message {
	if msg == nil || len(msg.ToolCalls) == 0 {
		return msg
	}
	sanitized := *msg
	sanitized.ToolCalls = nil
	if msg.Extra != nil {
		sanitized.Extra = make(map[string]any, len(msg.Extra))
		for key, value := range msg.Extra {
			sanitized.Extra[key] = value
		}
	}
	return &sanitized
}

func ExecuteToolCalls(ctx context.Context, toolNode dispatch.ToolInvoker, msg *schema.Message) ([]*schema.Message, error) {
	if toolNode == nil {
		return nil, errors.New("direct response requires tool node")
	}
	if msg == nil {
		return nil, errors.New("assistant message is required")
	}
	if len(msg.ToolCalls) == 0 {
		return nil, nil
	}
	executor := toolNode.NewStreamingExecutor(ctx)
	for _, call := range msg.ToolCalls {
		executor.Submit(call)
	}
	toolMessages, err := executor.GetRemainingResults(ctx)
	if err != nil {
		executor.Discard()
		return nil, err
	}
	return toolMessages, nil
}

func outputLimitContinuationMessage() *schema.Message {
	msg := schema.UserMessage("Output token limit hit. Continue directly from the previous assistant message. Do not apologize or recap.")
	msg.Extra = map[string]any{"acorn_meta": "output_limit_continuation"}
	return msg
}

func consumeInterleavedForAgentLoop(ctx context.Context, interleaved *core.InterleavedStream, executor dispatch.StreamingExecutor, beforeToolCall func(context.Context, schema.ToolCall) error) (*core.AssistantStreamResult, error) {
	var finalResult *core.AssistantStreamResult
	var rejectedErr *ToolCallRejectedError
	for {
		select {
		case call, ok := <-interleaved.ToolCallCh:
			if !ok {
				interleaved.ToolCallCh = nil
				if rejectedErr != nil {
					return finalResult, rejectedErr
				}
				if finalResult != nil {
					return finalResult, nil
				}
				continue
			}
			if beforeToolCall != nil && rejectedErr == nil {
				if err := beforeToolCall(ctx, call); err != nil {
					rejectedErr = &ToolCallRejectedError{Call: call, Err: err}
					continue
				}
			}
			if rejectedErr != nil {
				continue
			}
			executor.Submit(call)
		case result, ok := <-interleaved.FinalMessageCh:
			if !ok {
				interleaved.FinalMessageCh = nil
				continue
			}
			finalResult = new(core.AssistantStreamResult)
			*finalResult = result
			if rejectedErr != nil {
				return finalResult, rejectedErr
			}
			if interleaved.ToolCallCh == nil {
				return finalResult, nil
			}
			continue
		case err, ok := <-interleaved.ErrCh:
			if !ok {
				interleaved.ErrCh = nil
				continue
			}
			return nil, err
		case <-ctx.Done():
			executor.Discard()
			return nil, ctx.Err()
		}
	}
}
