package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/toolresult"
	"github.com/ycvk/acorn/internal/workspace"
)

// SafeParallelToolsNode dispatches tool calls with safety-aware parallelism.
// It classifies each tool call by its ToolSafety level and detects path
// conflicts for WriteScoped tools, then executes safe calls in parallel
// while serializing conflicting or NeverParallel calls.
//
// The node implements the same Invoke/Stream interface as compose.ToolsNode
// so it can serve as a drop-in replacement in a compose.Graph.
type SafeParallelToolsNode struct {
	tools     map[string]toolEntry
	scheduler *toolExecutionScheduler
}

type toolEntry struct {
	Tool einotool.InvokableTool
}

// NewSafeParallelToolsNode creates a safety-aware parallel tool dispatch node.
// Tools are classified using the provided execution policy resolver. maxParallel
// is clamped to min(GOMAXPROCS, 8).
func NewSafeParallelToolsNode(
	ctx context.Context,
	tools []einotool.BaseTool,
	resolver tooling.ExecutionPolicyResolver,
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

func (n *SafeParallelToolsNode) NewStreamingExecutor(ctx context.Context) orchestration.StreamingExecutor {
	return NewStreamingToolExecutor(n, n.scheduler, ctx)
}

func (n *SafeParallelToolsNode) invokeSingle(ctx context.Context, call classifiedCall) (*schema.Message, error) {
	recorder := &toolExecutionRecorder{}
	resultRef := toolresult.BuildRef(getRunID(ctx), call.toolCall.ID)
	if err := emitToolCallLifecycle(ctx, call); err != nil {
		if rejected, ok := errors.AsType[*contextplane.ToolCallRejectedError](err); ok {
			msg := schema.ToolMessage(rejected.Error(), call.toolCall.ID, schema.WithToolName(call.toolCall.Function.Name))
			attachToolMessageLedgerMeta(msg, call, resultRef)
			attachToolRecorder(msg, recorder)
			markToolMessageFailed(msg, rejected.Error())
			if emitErr := emitToolResultLifecycle(ctx, msg); emitErr != nil {
				return nil, emitErr
			}
			return msg, nil
		}
		return nil, err
	}
	if call.argsErr != "" {
		msg := schema.ToolMessage(
			fmt.Sprintf("Invalid arguments for tool %q: %s", call.toolCall.Function.Name, call.argsErr),
			call.toolCall.ID,
			schema.WithToolName(call.toolCall.Function.Name),
		)
		attachToolMessageLedgerMeta(msg, call, resultRef)
		attachToolRecorder(msg, recorder)
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
		attachToolRecorder(msg, recorder)
		markToolMessageFailed(msg, errMsg)
		if err := emitToolResultLifecycle(ctx, msg); err != nil {
			return nil, err
		}
		return msg, nil
	}
	result, err := invokeToolWithEinoCallbacks(ctx, entry.Tool, call.toolCall)
	if err != nil {
		// Interrupt errors must propagate to the graph layer so that
		// compose.ExtractInterruptInfo can catch them and checkpoint the run.
		if isInterruptError(err) {
			return nil, err
		}
		// Non-interrupt tool errors are converted to ToolMessages so the LLM
		// can see the failure and self-correct (e.g., try different arguments,
		// use a different tool, or report the issue). This prevents a single
		// tool failure from killing the entire agent loop.
		errorContent := result
		if errorContent == "" {
			errorContent = fmt.Sprintf("Tool call %q failed: %s", call.toolCall.Function.Name, err.Error())
		}
		msg := schema.ToolMessage(errorContent, call.toolCall.ID, schema.WithToolName(call.toolCall.Function.Name))
		attachToolMessageLedgerMeta(msg, call, resultRef)
		contextplane.AnnotateMessageTurn(msg, turnIndexFromContext(ctx))
		attachToolRecorder(msg, recorder)
		markToolMessageFailed(msg, err.Error())
		if err := emitToolResultLifecycle(ctx, msg); err != nil {
			return nil, err
		}
		return msg, nil
	}
	msg := schema.ToolMessage(result, call.toolCall.ID, schema.WithToolName(call.toolCall.Function.Name))
	attachToolMessageLedgerMeta(msg, call, resultRef)
	contextplane.AnnotateMessageTurn(msg, turnIndexFromContext(ctx))
	attachToolRecorder(msg, recorder)
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
	msg.Extra["tool_side_effects"] = append([]toolresult.SideEffectRef(nil), sideEffects...)
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
	callbackCtx = withToolAuditCallID(callbackCtx, call.ID)
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
	lifecycleCtx, err := requireToolLifecycle(ctx)
	if err != nil {
		return err
	}
	return lifecycleCtx.Plane.OnToolCall(ctx, contextplane.ToolCallEvent{
		RunID:     getRunID(ctx),
		SessionID: SessionIDFromContext(ctx),
		TurnIndex: turnIndexFromContext(ctx),
		CallID:    call.toolCall.ID,
		ToolName:  call.toolCall.Function.Name,
		Arguments: call.toolCall.Function.Arguments,
	})
}

func emitToolResultLifecycle(ctx context.Context, msg *schema.Message) error {
	lifecycleCtx, err := requireToolLifecycle(ctx)
	if err != nil {
		return err
	}
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
	var sideEffects []toolresult.SideEffectRef
	if rawSideEffects, ok := msg.Extra["tool_side_effects"]; ok {
		switch value := rawSideEffects.(type) {
		case []toolresult.SideEffectRef:
			sideEffects = append(sideEffects, value...)
		case []*toolresult.SideEffectRef:
			for _, item := range value {
				if item != nil {
					sideEffects = append(sideEffects, *item)
				}
			}
		}
	}
	return lifecycleCtx.Plane.OnToolResult(ctx, contextplane.ToolResultEvent{
		RunID:        getRunID(ctx),
		SessionID:    SessionIDFromContext(ctx),
		TurnIndex:    turnIndexFromContext(ctx),
		CallID:       msg.ToolCallID,
		ToolName:     msg.ToolName,
		Arguments:    arguments,
		Result:       msg.Content,
		IsError:      failed,
		ErrorReason:  reason,
		ResultTokens: len(msg.Content) / 4,
		SideEffects:  sideEffects,
	})
}

func requireToolLifecycle(ctx context.Context) (*contextplane.ToolLifecycleContext, error) {
	lifecycleCtx := contextplane.ToolLifecycleContextFromContext(ctx)
	if lifecycleCtx == nil {
		return nil, errors.New("tool lifecycle context is not initialized")
	}
	if lifecycleCtx.State == nil {
		return nil, errors.New("tool lifecycle state is not initialized")
	}
	if lifecycleCtx.Plane == nil {
		return nil, errors.New("tool lifecycle context plane is not initialized")
	}
	return lifecycleCtx, nil
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

func attachToolRecorder(msg *schema.Message, recorder *toolExecutionRecorder) {
	if msg == nil || recorder == nil {
		return
	}
	if msg.Extra == nil {
		msg.Extra = make(map[string]any, 1)
	}
	msg.Extra["plan_evidence_recorder"] = *recorder
}

func toolSideEffectsFromResult(toolName string, result string) ([]toolresult.SideEffectRef, error) {
	switch strings.TrimSpace(toolName) {
	case "create_file", "replace_span", "apply_unified_patch", "multi_edit":
		return mutationCheckpointSideEffects(toolName, result)
	case "rollback_workspace_checkpoint":
		return rollbackSideEffects(result)
	case "artifact_write":
		return artifactWriteSideEffects(result)
	case "run_verification":
		return runVerificationSideEffects(result)
	case "git_summary":
		return gitSummarySideEffects(result)
	case "terminal_session_start", "terminal_session_write", "terminal_session_signal", "terminal_session_close":
		return terminalSessionSideEffects(toolName, result)
	case "ask_operator":
		return operatorQuestionSideEffects(result)
	default:
		return nil, nil
	}
}

func operatorQuestionSideEffects(result string) ([]toolresult.SideEffectRef, error) {
	var payload struct {
		ActionID string `json:"action_id"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return nil, fmt.Errorf("parse ask_operator result: %w", err)
	}
	actionID := strings.TrimSpace(payload.ActionID)
	if actionID == "" {
		return nil, errors.New("ask_operator result missing action_id")
	}
	return []toolresult.SideEffectRef{{
		Kind: toolresult.SideEffectKindOperatorAction,
		Ref:  actionID,
	}}, nil
}

func terminalSessionSideEffects(toolName string, result string) ([]toolresult.SideEffectRef, error) {
	var payload struct {
		TerminalSessionID string `json:"terminal_session_id"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return nil, fmt.Errorf("parse %s result: %w", toolName, err)
	}
	terminalSessionID := strings.TrimSpace(payload.TerminalSessionID)
	if terminalSessionID == "" {
		return nil, fmt.Errorf("%s result missing terminal_session_id", toolName)
	}
	return []toolresult.SideEffectRef{{
		Kind: toolresult.SideEffectKindTerminalSession,
		Ref:  terminalSessionID,
	}}, nil
}

func artifactWriteSideEffects(result string) ([]toolresult.SideEffectRef, error) {
	var payload struct {
		ArtifactID string `json:"artifact_id"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return nil, fmt.Errorf("parse artifact_write result: %w", err)
	}
	artifactID := strings.TrimSpace(payload.ArtifactID)
	if artifactID == "" {
		return nil, errors.New("artifact_write result missing artifact_id")
	}
	return []toolresult.SideEffectRef{{
		Kind: toolresult.SideEffectKindArtifact,
		Ref:  artifactID,
	}}, nil
}

func runVerificationSideEffects(result string) ([]toolresult.SideEffectRef, error) {
	var payload struct {
		StdoutArtifactID string `json:"stdout_artifact_id"`
		StderrArtifactID string `json:"stderr_artifact_id"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return nil, fmt.Errorf("parse run_verification result: %w", err)
	}
	ids := normalizedSideEffectPaths([]string{payload.StdoutArtifactID, payload.StderrArtifactID})
	if len(ids) != 2 {
		return nil, errors.New("run_verification result missing stdout_artifact_id or stderr_artifact_id")
	}
	return []toolresult.SideEffectRef{
		{Kind: toolresult.SideEffectKindArtifact, Ref: ids[0]},
		{Kind: toolresult.SideEffectKindArtifact, Ref: ids[1]},
	}, nil
}

func gitSummarySideEffects(result string) ([]toolresult.SideEffectRef, error) {
	var payload struct {
		DiffArtifactID string `json:"diff_artifact_id"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return nil, fmt.Errorf("parse git_summary result: %w", err)
	}
	artifactID := strings.TrimSpace(payload.DiffArtifactID)
	if artifactID == "" {
		return nil, nil
	}
	return []toolresult.SideEffectRef{{
		Kind: toolresult.SideEffectKindArtifact,
		Ref:  artifactID,
	}}, nil
}

func mutationCheckpointSideEffects(toolName string, result string) ([]toolresult.SideEffectRef, error) {
	var payload struct {
		CheckpointID    string   `json:"checkpoint_id"`
		CheckpointPaths []string `json:"checkpoint_paths"`
		Path            string   `json:"path"`
		Paths           []string `json:"paths"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return nil, fmt.Errorf("parse %s result: %w", toolName, err)
	}
	checkpointID := strings.TrimSpace(payload.CheckpointID)
	if checkpointID == "" {
		return nil, fmt.Errorf("%s result missing checkpoint_id", toolName)
	}
	paths := normalizedSideEffectPaths(payload.CheckpointPaths)
	if len(paths) == 0 {
		paths = normalizedSideEffectPaths(payload.Paths)
	}
	if len(paths) == 0 {
		paths = normalizedSideEffectPaths([]string{payload.Path})
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("%s result missing checkpoint_paths", toolName)
	}
	effects := make([]toolresult.SideEffectRef, 0, len(paths))
	for _, path := range paths {
		effects = append(effects, toolresult.SideEffectRef{
			Kind: workspace.MutationCheckpointEffect,
			Ref:  checkpointID,
			Path: path,
		})
	}
	return effects, nil
}

func rollbackSideEffects(result string) ([]toolresult.SideEffectRef, error) {
	var payload struct {
		CheckpointID  string   `json:"checkpoint_id"`
		RollbackID    string   `json:"rollback_id"`
		Status        string   `json:"status"`
		RestoredPaths []string `json:"restored_paths"`
		ConflictPaths []string `json:"conflict_paths"`
		Error         string   `json:"error"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return nil, fmt.Errorf("parse rollback result: %w", err)
	}
	if strings.TrimSpace(payload.Status) != "succeeded" {
		return nil, nil
	}
	rollbackID := strings.TrimSpace(payload.RollbackID)
	if rollbackID == "" {
		return nil, errors.New("rollback result missing rollback_id")
	}
	paths := normalizedSideEffectPaths(payload.RestoredPaths)
	if len(paths) == 0 {
		return nil, errors.New("rollback result missing restored_paths")
	}
	effects := make([]toolresult.SideEffectRef, 0, len(paths))
	for _, path := range paths {
		effects = append(effects, toolresult.SideEffectRef{
			Kind: workspace.MutationRollbackEffect,
			Ref:  rollbackID,
			Path: path,
		})
	}
	return effects, nil
}

func normalizedSideEffectPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if trimmed := strings.TrimSpace(path); trimmed != "" {
			out = append(out, filepath.ToSlash(trimmed))
		}
	}
	return out
}

// isInterruptError returns true if the error represents an Eino compose
// interrupt or an ADK interrupt signal — both require graph-level handling
// and must not be silently absorbed into a tool result message.
func isInterruptError(err error) bool {
	if _, ok := compose.ExtractInterruptInfo(err); ok {
		return true
	}
	var signal *adk.InterruptSignal
	return errors.As(err, &signal)
}
