package runtime

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"encoding/json"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/runtime/orchestration"
	"github.com/ycvk/acorn/internal/toolkit"
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
	resolver toolkit.ExecutionPolicyResolver,
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
	resultRef := buildToolResultRef(getRunID(ctx), call.toolCall.ID)
	if err := emitToolCallLifecycle(ctx, call); err != nil {
		if rejected, ok := errors.AsType[*contextplane.ToolCallRejectedError](err); ok {
			msg := schema.ToolMessage(rejected.Error(), call.toolCall.ID, schema.WithToolName(call.toolCall.Function.Name))
			attachToolMessageLedgerMeta(msg, call, resultRef)
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
		// Interrupt errors must propagate to the graph layer so that
		// compose.ExtractInterruptInfo can catch them and checkpoint the run.
		if IsInterruptError(err) {
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
		contextplane.AnnotateMessageTurn(msg, domain.TurnIndexFromContext(ctx))
		markToolMessageFailed(msg, err.Error())
		if err := emitToolResultLifecycle(ctx, msg); err != nil {
			return nil, err
		}
		return msg, nil
	}
	msg := schema.ToolMessage(result, call.toolCall.ID, schema.WithToolName(call.toolCall.Function.Name))
	attachToolMessageLedgerMeta(msg, call, resultRef)
	contextplane.AnnotateMessageTurn(msg, domain.TurnIndexFromContext(ctx))
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
	return contextplane.OnToolCall(ctx, contextplane.ToolCallEvent{
		RunID:     getRunID(ctx),
		SessionID: domain.SessionIDFromContext(ctx),
		TurnIndex: domain.TurnIndexFromContext(ctx),
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
	return contextplane.OnToolResult(ctx, contextplane.ToolResultEvent{
		RunID:        getRunID(ctx),
		SessionID:    domain.SessionIDFromContext(ctx),
		TurnIndex:    domain.TurnIndexFromContext(ctx),
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
// interrupt or an ADK interrupt signal — both require graph-level handling
// and must not be silently absorbed into a tool result message.
func IsInterruptError(err error) bool {
	if _, ok := compose.ExtractInterruptInfo(err); ok {
		return true
	}
	var signal *adk.InterruptSignal
	return errors.As(err, &signal)
}

type StreamingToolExecutor struct {
	node          *SafeParallelToolsNode
	scheduler     *toolExecutionScheduler
	ctx           context.Context
	siblingCtx    context.Context
	siblingCancel context.CancelFunc

	mu                sync.Mutex
	submitted         []*submittedTool
	resultIndex       map[string]int
	completed         int
	discarded         bool
	classificationErr error
	sem               chan struct{}
}

type submittedTool struct {
	call    schema.ToolCall
	status  toolExecutionStatus
	isSafe  bool
	index   int
	paths   []string
	argsErr string
	result  *schema.Message
	err     error
}

type toolExecutionStatus int

const (
	statusQueued toolExecutionStatus = iota
	statusExecuting
	statusCompleted
	statusYielded
)

func NewStreamingToolExecutor(node *SafeParallelToolsNode, scheduler *toolExecutionScheduler, ctx context.Context) *StreamingToolExecutor {
	siblingCtx, cancel := context.WithCancel(ctx)
	maxP := 1
	if scheduler != nil && scheduler.maxParallel > 0 {
		maxP = scheduler.maxParallel
	}
	return &StreamingToolExecutor{
		node:          node,
		scheduler:     scheduler,
		ctx:           ctx,
		siblingCtx:    siblingCtx,
		siblingCancel: cancel,
		resultIndex:   make(map[string]int),
		sem:           make(chan struct{}, maxP),
	}
}

func (e *StreamingToolExecutor) Submit(call schema.ToolCall) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.discarded || e.classificationErr != nil {
		return
	}

	idx := len(e.submitted)
	e.resultIndex[strings.TrimSpace(call.ID)] = idx

	var args map[string]any
	argsErr := ""
	if unmarshalErr := json.Unmarshal([]byte(call.Function.Arguments), &args); unmarshalErr != nil {
		args = nil
		argsErr = unmarshalErr.Error()
	}

	policy := toolkit.ToolExecutionPolicy{ParallelPolicy: toolkit.ParallelPolicySerial}
	isSafe := false
	var paths []string
	if argsErr == "" {
		policyErr := error(nil)
		var pathErr error
		resolvedPolicy, policyErr := e.scheduler.resolver.ExecutionPolicy(call.Function.Name, args)
		if policyErr == nil {
			policy = resolvedPolicy
		}
		isSafe = policy.ParallelPolicy == toolkit.ParallelPolicyReadOnly
		if strings.TrimSpace(policy.PathArg) != "" {
			paths, pathErr = executionPathsFromArgs(args, policy.PathArg, policy.ParallelPolicy == toolkit.ParallelPolicySerial)
			if pathErr != nil {
				argsErr = pathErr.Error()
			}
		}
	}

	st := &submittedTool{
		call:    call,
		status:  statusQueued,
		isSafe:  isSafe,
		index:   idx,
		paths:   paths,
		argsErr: argsErr,
	}
	e.submitted = append(e.submitted, st)

	if e.canExecuteImmediately(st) {
		e.startExecution(st)
	}
}

func (e *StreamingToolExecutor) canExecuteImmediately(st *submittedTool) bool {
	for _, other := range e.submitted {
		if other.status != statusExecuting {
			continue
		}
		if (!st.isSafe && len(st.paths) == 0) || (!other.isSafe && len(other.paths) == 0) {
			return false
		}
		if pathsOverlap(st.paths, other.paths) {
			return false
		}
	}
	return true
}

func (e *StreamingToolExecutor) startExecution(st *submittedTool) {
	st.status = statusExecuting

	go func() {
		e.sem <- struct{}{}
		defer func() {
			if r := recover(); r != nil {
				e.mu.Lock()
				if !e.discarded {
					st.status = statusCompleted
					st.err = fmt.Errorf("tool execution panic: %v", r)
					e.completed++
				}
				e.mu.Unlock()
			}
			<-e.sem
		}()

		call := classifiedCall{
			index:    st.index,
			toolCall: st.call,
			safety:   toolkit.ParallelPolicySerial,
			argsErr:  st.argsErr,
			paths:    st.paths,
		}
		if len(st.paths) > 0 {
			call.safety = toolkit.ParallelPolicySerial
		} else if st.isSafe {
			call.safety = toolkit.ParallelPolicyReadOnly
		}

		msg, err := e.node.invokeSingle(e.siblingCtx, call)

		e.mu.Lock()
		defer e.mu.Unlock()

		if e.discarded {
			return
		}

		st.status = statusCompleted
		st.result = msg
		st.err = err

		if err != nil {
			if IsInterruptError(err) {
				e.siblingCancel()
			}
		}

		e.completed++
	}()
}

func (e *StreamingToolExecutor) GetRemainingResults(ctx context.Context) ([]*schema.Message, error) {
	e.mu.Lock()
	classificationErr := e.classificationErr
	if len(e.submitted) == 0 {
		e.mu.Unlock()
		return nil, fmt.Errorf("safe parallel tools node: no tool calls in input message")
	}
	e.mu.Unlock()
	if classificationErr != nil {
		return nil, classificationErr
	}
	for {
		e.mu.Lock()
		allDone := e.completed >= len(e.submitted)
		e.mu.Unlock()

		if allDone {
			break
		}

		e.mu.Lock()
		for _, st := range e.submitted {
			if st.status != statusQueued {
				continue
			}
			if e.canExecuteImmediately(st) {
				e.startExecution(st)
			}
		}
		e.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-e.siblingCtx.Done():
			select {
			case <-time.After(100 * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		case <-time.After(50 * time.Millisecond):
		}
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	output := make([]*schema.Message, len(e.submitted))
	for i, st := range e.submitted {
		if st.err != nil {
			if IsInterruptError(st.err) {
				return nil, st.err
			}
			return nil, fmt.Errorf("tool %q (id %s) runtime failure: %w", st.call.Function.Name, st.call.ID, st.err)
		}
		output[i] = st.result
	}
	return output, nil
}

func (e *StreamingToolExecutor) Discard() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.discarded = true
	e.siblingCancel()
}

type toolExecutionScheduler struct {
	resolver    toolkit.ExecutionPolicyResolver
	maxParallel int
	knownTools  map[string]struct{}
}

func newToolExecutionScheduler(resolver toolkit.ExecutionPolicyResolver, maxParallel int, knownTools map[string]struct{}) *toolExecutionScheduler {
	if maxParallel < 1 {
		maxParallel = 1
	}
	copied := make(map[string]struct{}, len(knownTools))
	for name := range knownTools {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			copied[trimmed] = struct{}{}
		}
	}
	return &toolExecutionScheduler{
		resolver:    resolver,
		maxParallel: maxParallel,
		knownTools:  copied,
	}
}

type classifiedCall struct {
	index    int
	toolCall schema.ToolCall
	safety   toolkit.ParallelPolicy
	argsErr  string
	paths    []string
}

func pathsOverlap(left []string, right []string) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(left))
	for _, path := range left {
		if trimmed := strings.TrimSpace(path); trimmed != "" {
			seen[trimmed] = struct{}{}
		}
	}
	for _, path := range right {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			return true
		}
	}
	return false
}

func executionPathsFromArgs(args map[string]any, pathArg string, required bool) ([]string, error) {
	key := strings.TrimSpace(pathArg)
	if key == "" {
		return nil, nil
	}
	if args == nil {
		if required {
			return nil, fmt.Errorf("missing required %s argument", key)
		}
		return nil, nil
	}
	raw, ok := args[key]
	if !ok {
		if required {
			return nil, fmt.Errorf("missing required %s argument", key)
		}
		return nil, nil
	}
	paths, ok := normalizeExecutionPaths(raw)
	if !ok {
		if required {
			return nil, fmt.Errorf("%s argument must be a string or array of strings", key)
		}
		return nil, nil
	}
	if len(paths) == 0 {
		if required {
			return nil, fmt.Errorf("missing required %s argument", key)
		}
		return nil, nil
	}
	return paths, nil
}

func normalizeExecutionPaths(raw any) ([]string, bool) {
	switch value := raw.(type) {
	case string:
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return []string{trimmed}, true
		}
		return nil, true
	case []string:
		return executionTrimmedPaths(value), true
	case []any:
		paths := make([]string, 0, len(value))
		for _, item := range value {
			path, ok := item.(string)
			if !ok {
				return nil, false
			}
			paths = append(paths, path)
		}
		return executionTrimmedPaths(paths), true
	default:
		return nil, false
	}
}

func executionTrimmedPaths(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
