package dispatch

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/ycvk/acorn/internal/core"

	"github.com/cloudwego/eino/schema"
)

// SafeParallelToolsNode dispatches tool calls with safety-aware parallelism.
// It classifies each tool call by its ToolSafety level and detects path
// conflicts for WriteScoped tools, then executes safe calls in parallel
// while serializing conflicting or NeverParallel calls.
type SafeParallelToolsNode struct {
	tools     map[string]toolEntry
	scheduler *toolExecutionScheduler
}

type toolEntry struct {
	Tool einotool.InvokableTool
}

// NewSafeParallelToolsNode creates a safety-aware parallel tool dispatch node.
func NewSafeParallelToolsNode(
	ctx context.Context,
	tools []einotool.BaseTool,
	resolver core.ExecutionPolicyResolver,
) (*SafeParallelToolsNode, error) {
	if ctx == nil {
		return nil, fmt.Errorf("safe parallel tools node: context is required")
	}
	if resolver == nil {
		return nil, fmt.Errorf("safe parallel tools node: execution policy resolver is required")
	}
	maxP := runtime.GOMAXPROCS(0)
	if maxP > 8 {
		maxP = 8
	}
	if maxP < 1 {
		maxP = 1
	}

	entries := make(map[string]toolEntry, len(tools))
	for _, bt := range tools {
		it, ok := bt.(einotool.InvokableTool)
		if !ok {
			return nil, fmt.Errorf("safe parallel tools node: tool does not implement InvokableTool")
		}
		info, err := bt.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("safe parallel tools node: read tool info: %w", err)
		}
		name := info.Name
		_, err = resolver.ExecutionPolicy(name, nil)
		if err != nil {
			return nil, fmt.Errorf("safe parallel tools node: resolve execution policy for %q: %w", name, err)
		}
		entries[name] = toolEntry{Tool: it}
	}

	knownTools := make(map[string]struct{}, len(entries))
	for name := range entries {
		knownTools[name] = struct{}{}
	}
	return &SafeParallelToolsNode{
		tools:     entries,
		scheduler: newToolExecutionScheduler(resolver, maxP, knownTools),
	}, nil
}

func (n *SafeParallelToolsNode) NewStreamingExecutor(ctx context.Context) StreamingExecutor {
	return NewStreamingToolExecutor(n, n.scheduler, ctx)
}

func (n *SafeParallelToolsNode) invokeSingle(ctx context.Context, call classifiedCall) (*schema.Message, error) {
	resultRef := buildToolResultRef(core.GetRunID(ctx), call.toolCall.ID)
	if err := emitToolCallLifecycle(ctx, call); err != nil {
		return nil, err
	}
	if call.argsErr != "" {
		msg := schema.ToolMessage(
			fmt.Sprintf("Invalid arguments for tool %q: %s", call.toolCall.Function.Name, call.argsErr),
			call.toolCall.ID,
			schema.WithToolName(call.toolCall.Function.Name),
		)
		attachToolMessageLedgerMeta(msg, call, resultRef)
		markToolMessageFailed(msg, call.argsErr)
		if err := emitToolResultLifecycle(ctx, msg); err != nil {
			return nil, err
		}
		return msg, nil
	}
	entry, ok := n.tools[call.toolCall.Function.Name]
	if !ok {
		errMsg := fmt.Sprintf("Tool %q not found.", call.toolCall.Function.Name)
		msg := schema.ToolMessage(errMsg, call.toolCall.ID, schema.WithToolName(call.toolCall.Function.Name))
		attachToolMessageLedgerMeta(msg, call, resultRef)
		markToolMessageFailed(msg, errMsg)
		if err := emitToolResultLifecycle(ctx, msg); err != nil {
			return nil, err
		}
		return msg, nil
	}
	result, err := invokeToolWithEinoCallbacks(ctx, entry.Tool, call.toolCall)
	if err != nil {
		if IsInterruptError(err) {
			return nil, err
		}
		errorContent := result
		if errorContent == "" {
			errorContent = fmt.Sprintf("Tool call %q failed: %s", call.toolCall.Function.Name, err.Error())
		}
		msg := schema.ToolMessage(errorContent, call.toolCall.ID, schema.WithToolName(call.toolCall.Function.Name))
		attachToolMessageLedgerMeta(msg, call, resultRef)
		_ = core.TurnIndexFromContext(ctx) // TODO(phase7): restore context.AnnotateMessageTurn
		markToolMessageFailed(msg, err.Error())
		if err := emitToolResultLifecycle(ctx, msg); err != nil {
			return nil, err
		}
		return msg, nil
	}
	msg := schema.ToolMessage(result, call.toolCall.ID, schema.WithToolName(call.toolCall.Function.Name))
	attachToolMessageLedgerMeta(msg, call, resultRef)
	_ = core.TurnIndexFromContext(ctx) // TODO(phase7): restore context.AnnotateMessageTurn
	if err := attachToolSideEffects(msg, call.toolCall.Function.Name, result); err != nil {
		return nil, err
	}
	if err := emitToolResultLifecycle(ctx, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func attachToolMessageLedgerMeta(msg *schema.Message, call classifiedCall, resultRef string) {
	if msg == nil {
		return
	}
	if msg.Extra == nil {
		msg.Extra = make(map[string]any, 2)
	}
	msg.Extra["tool_result_ref"] = strings.TrimSpace(resultRef)
	msg.Extra["tool_arguments_json"] = call.toolCall.Function.Arguments
}

func attachToolSideEffects(msg *schema.Message, toolName string, result string) error {
	sideEffects, err := toolSideEffectsFromResult(toolName, result)
	if err != nil {
		return err
	}
	if len(sideEffects) == 0 {
		return nil
	}
	if msg.Extra == nil {
		msg.Extra = make(map[string]any, 3)
	}
	msg.Extra["tool_side_effects"] = append([]SideEffectRef(nil), sideEffects...)
	return nil
}

func invokeToolWithEinoCallbacks(ctx context.Context, tool einotool.InvokableTool, call schema.ToolCall) (string, error) {
	toolName := strings.TrimSpace(call.Function.Name)
	callbackCtx := callbacks.ReuseHandlers(ctx, &callbacks.RunInfo{
		Name:      toolName,
		Type:      toolCallbackType(tool),
		Component: components.ComponentOfTool,
	})
	extra := map[string]any{"tool_call_id": call.ID}
	callbackCtx = callbacks.OnStart(callbackCtx, &einotool.CallbackInput{
		ArgumentsInJSON: call.Function.Arguments,
		Extra:           extra,
	})
	callbackCtx = WithToolAuditCallID(callbackCtx, call.ID)
	result, err := tool.InvokableRun(callbackCtx, call.Function.Arguments)
	if err != nil {
		callbacks.OnError(callbackCtx, err)
		return result, err
	}
	callbacks.OnEnd(callbackCtx, &einotool.CallbackOutput{
		Response: result,
		Extra:    extra,
	})
	return result, nil
}

func toolCallbackType(tool einotool.InvokableTool) string {
	if typ, ok := components.GetType(tool); ok {
		if trimmed := strings.TrimSpace(typ); trimmed != "" {
			return trimmed
		}
	}
	return "InvokableTool"
}

func emitToolCallLifecycle(ctx context.Context, call classifiedCall) error {
	return OnToolCall(ctx, ToolCallEvent{
		RunID:     core.GetRunID(ctx),
		SessionID: core.GetSessionID(ctx),
		TurnIndex: core.TurnIndexFromContext(ctx),
		CallID:    call.toolCall.ID,
		ToolName:  call.toolCall.Function.Name,
		Arguments: call.toolCall.Function.Arguments,
	})
}

func emitToolResultLifecycle(ctx context.Context, msg *schema.Message) error {
	if msg == nil {
		return errors.New("tool lifecycle result message is nil")
	}
	var failed bool
	if rawFailed, ok := msg.Extra["tool_error"]; ok {
		if value, ok := rawFailed.(bool); ok {
			failed = value
		}
	}
	var reason string
	if rawReason, ok := msg.Extra["tool_error_reason"]; ok {
		if value, ok := rawReason.(string); ok {
			reason = value
		}
	}
	var arguments string
	if rawArgs, ok := msg.Extra["tool_arguments_json"]; ok {
		if value, ok := rawArgs.(string); ok {
			arguments = value
		}
	}
	return OnToolResult(ctx, ToolResultEvent{
		RunID:        core.GetRunID(ctx),
		SessionID:    core.GetSessionID(ctx),
		TurnIndex:    core.TurnIndexFromContext(ctx),
		CallID:       msg.ToolCallID,
		ToolName:     msg.ToolName,
		Arguments:    arguments,
		Result:       msg.Content,
		IsError:      failed,
		ErrorReason:  reason,
		ResultTokens: len(msg.Content) / 4,
	})
}
func markToolMessageFailed(msg *schema.Message, reason string) {
	if msg == nil {
		return
	}
	if msg.Extra == nil {
		msg.Extra = make(map[string]any, 2)
	}
	msg.Extra["tool_error"] = true
	msg.Extra["tool_error_reason"] = reason
}

// IsInterruptError returns true if the error represents an Eino compose
// interrupt or an ADK interrupt signal.
func IsInterruptError(err error) bool {
	if _, ok := compose.ExtractInterruptInfo(err); ok {
		return true
	}
	var signal *adk.InterruptSignal
	return errors.As(err, &signal)
}
