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
	if call.argsErr != "" {
		msg := schema.ToolMessage(
			fmt.Sprintf("Invalid arguments for tool %q: %s", call.toolCall.Function.Name, call.argsErr),
			call.toolCall.ID,
			schema.WithToolName(call.toolCall.Function.Name),
		)
		attachToolMessageLedgerMeta(msg, call, resultRef)
		markToolMessageFailed(msg, call.argsErr)
		return msg, nil
	}
	entry, ok := n.tools[call.toolCall.Function.Name]
	if !ok {
		errMsg := fmt.Sprintf("Tool %q not found.", call.toolCall.Function.Name)
		msg := schema.ToolMessage(errMsg, call.toolCall.ID, schema.WithToolName(call.toolCall.Function.Name))
		attachToolMessageLedgerMeta(msg, call, resultRef)
		markToolMessageFailed(msg, errMsg)
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
		annotateTurnIndex(msg, ctx)
		markToolMessageFailed(msg, err.Error())
		return msg, nil
	}
	msg := schema.ToolMessage(result, call.toolCall.ID, schema.WithToolName(call.toolCall.Function.Name))
	attachToolMessageLedgerMeta(msg, call, resultRef)
	annotateTurnIndex(msg, ctx)
	if err := attachToolSideEffects(msg, call.toolCall.Function.Name, result); err != nil {
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

// annotateTurnIndex stamps the current turn index onto a tool message, using
// the turn index plumbed via context. This allows downstream masking to
// determine message age relative to the current turn.
func annotateTurnIndex(msg *schema.Message, ctx context.Context) {
	if msg == nil {
		return
	}
	turnIndex := core.TurnIndexFromContext(ctx)
	if msg.Extra == nil {
		msg.Extra = make(map[string]any, 2)
	}
	msg.Extra[core.TurnIndexExtraKey] = turnIndex
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
