package orchestration

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/contextplane"
)

type AgentLoop struct {
	model    einomodel.BaseChatModel
	toolNode ToolInvoker
	streamer AssistantStreamer
	session  contextplane.ContextSession
}

func NewAgentLoop(model einomodel.BaseChatModel, toolNode ToolInvoker, streamer AssistantStreamer, session contextplane.ContextSession) *AgentLoop {
	return &AgentLoop{
		model:    model,
		toolNode: toolNode,
		streamer: streamer,
		session:  session,
	}
}

type AgentLoopIteration struct {
	Message            *schema.Message
	ToolMessages       []*schema.Message
	OutputLimitReached bool
}

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

func (l *AgentLoop) RunOneIteration(ctx context.Context, toolInfos []*schema.ToolInfo, runID string, messageID string, allowCompact bool) (*AgentLoopIteration, error) {
	modelReq := contextplane.ModelCallRequest{
		CallID:       messageID,
		QuerySource:  "agent_loop",
		AllowCompact: allowCompact,
		ToolInfos:    toolInfos,
	}
	modelInput, err := l.session.BeforeModelCall(ctx, modelReq)
	if err != nil {
		return nil, fmt.Errorf("agent loop before model call: %w", err)
	}
	msg, toolMessages, outputLimitReached, err := RunActionRound(ctx, l.model, l.streamer, l.toolNode, modelInput.Messages, toolInfos, runID, messageID, allowCompact, l.agentLoopCompact(modelReq), RoundOptions{})
	if err == nil {
		if err := l.recordRoundResults(ctx, msg, toolMessages, outputLimitReached); err != nil {
			return nil, err
		}
	}
	return &AgentLoopIteration{Message: msg, ToolMessages: toolMessages, OutputLimitReached: outputLimitReached}, err
}

func (l *AgentLoop) agentLoopCompact(modelReq contextplane.ModelCallRequest) CompactFn {
	return func(ctx context.Context, streamErr error) ([]*schema.Message, error) {
		recovered, err := l.session.ReactiveCompact(ctx, modelReq, streamErr)
		if err != nil {
			return nil, fmt.Errorf("agent loop reactive compact: %w", err)
		}
		return recovered.Messages, nil
	}
}

func (l *AgentLoop) recordRoundResults(ctx context.Context, msg *schema.Message, toolMessages []*schema.Message, outputLimitReached bool) error {
	if err := l.session.RecordAssistant(ctx, msg); err != nil {
		return fmt.Errorf("agent loop record assistant: %w", err)
	}
	if len(toolMessages) > 0 {
		if err := l.session.RecordToolResults(ctx, toolMessages); err != nil {
			return fmt.Errorf("agent loop record tool results: %w", err)
		}
	}
	if outputLimitReached {
		if err := l.session.RecordMessages(ctx, []adk.Message{outputLimitContinuationMessage()}); err != nil {
			return fmt.Errorf("agent loop record output limit continuation: %w", err)
		}
	}
	return nil
}

func ExecuteRound(ctx context.Context, model einomodel.BaseChatModel, streamer AssistantStreamer, toolNode ToolInvoker, messages []*schema.Message, toolInfos []*schema.ToolInfo, runID string, messageID string, opts RoundOptions) (*schema.Message, []*schema.Message, bool, error) {
	interleaved := streamer.StreamAssistantInterleaved(ctx, AssistantStreamRequest{
		RunID:     runID,
		MessageID: messageID,
		Model:     model,
		Messages:  messages,
		ToolInfos: toolInfos,
		CallSite:  opts.CallSite,
	})

	executor := toolNode.NewStreamingExecutor(ctx)
	result, err := consumeInterleavedForAgentLoop(ctx, interleaved, executor, opts.BeforeToolCall)
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
	case AssistantStopReasonMaxOutput:
		executor.Discard()
		return assistantMessageWithoutToolCalls(msg), nil, true, nil
	case AssistantStopReasonEndTurn, AssistantStopReasonToolCalls:
	case AssistantStopReasonUnknown:
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

func ExecuteToolCalls(ctx context.Context, toolNode ToolInvoker, msg *schema.Message) ([]*schema.Message, error) {
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

func consumeInterleavedForAgentLoop(ctx context.Context, interleaved *InterleavedStream, executor StreamingExecutor, beforeToolCall func(context.Context, schema.ToolCall) error) (*AssistantStreamResult, error) {
	var finalResult *AssistantStreamResult
	for {
		select {
		case call, ok := <-interleaved.ToolCallCh:
			if !ok {
				interleaved.ToolCallCh = nil
				if finalResult != nil {
					return finalResult, nil
				}
				continue
			}
			if beforeToolCall != nil {
				if err := beforeToolCall(ctx, call); err != nil {
					executor.Discard()
					return finalResult, &ToolCallRejectedError{Call: call, Err: err}
				}
			}
			executor.Submit(call)
		case result, ok := <-interleaved.FinalMessageCh:
			if !ok {
				interleaved.FinalMessageCh = nil
				continue
			}
			finalResult = new(result)
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
