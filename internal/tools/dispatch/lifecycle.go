package dispatch

import (
	"context"
	"strings"
	"sync"
)

// ToolCallEvent describes a tool invocation for lifecycle validation.
type ToolCallEvent struct {
	RunID     string
	SessionID string
	TurnIndex int
	CallID    string
	ToolName  string
	Arguments string
}

// ToolResultEvent describes a tool result for lifecycle validation.
type ToolResultEvent struct {
	RunID        string
	SessionID    string
	TurnIndex    int
	CallID       string
	ToolName     string
	Arguments    string
	Result       string
	IsError      bool
	ErrorReason  string
	ResultTokens int
}

// ToolLifecycleRejectedError is returned when a tool call is rejected by the
// tool lifecycle (deferred or not loaded).
type ToolLifecycleRejectedError struct {
	ToolName string
	Reason   string
}

func (e *ToolLifecycleRejectedError) Error() string {
	if e == nil {
		return "tool lifecycle rejected"
	}
	if e.ToolName == "" && e.Reason == "" {
		return "tool call rejected"
	}
	if e.ToolName == "" {
		return "tool call rejected: " + e.Reason
	}
	if e.Reason == "" {
		return "tool " + e.ToolName + " rejected by lifecycle"
	}
	return "tool " + e.ToolName + " rejected by lifecycle: " + e.Reason
}

// lifecycleState is the minimal lifecycle state interface that
// OnToolCall/OnToolResult need to validate tool availability.
type lifecycleState interface {
	Mu() *sync.Mutex
	IsLoaded(toolName string) bool
	IsDeferred(toolName string) bool
}

type toolLifecycleContextKey struct{}

type toolLifecycleContext struct {
	State lifecycleState
}

func toolLifecycleContextFromContext(ctx context.Context) *toolLifecycleContext {
	if ctx == nil {
		return nil
	}
	raw := ctx.Value(toolLifecycleContextKey{})
	lc, ok := raw.(*toolLifecycleContext)
	if !ok {
		return nil
	}
	return lc
}

// OnToolCall validates that a tool is loaded (not deferred) before execution.
func OnToolCall(ctx context.Context, event ToolCallEvent) error {
	toolName := strings.TrimSpace(event.ToolName)
	if toolName == "" {
		return errToolCallRequiresToolName
	}
	lc := toolLifecycleContextFromContext(ctx)
	if lc == nil || lc.State == nil {
		return errToolLifecycleNotInitialized
	}
	lc.State.Mu().Lock()
	loaded := lc.State.IsLoaded(toolName)
	deferred := lc.State.IsDeferred(toolName)
	lc.State.Mu().Unlock()
	if loaded {
		return nil
	}
	if deferred {
		return &ToolLifecycleRejectedError{
			ToolName: toolName,
			Reason:   "deferred; load it with load_tools before calling it",
		}
	}
	return &ToolLifecycleRejectedError{
		ToolName: toolName,
		Reason:   "not loaded or enabled for this run",
	}
}

// OnToolResult records a tool result event. The durable ledger was removed;
// this now only validates the event payload.
func OnToolResult(ctx context.Context, event ToolResultEvent) error {
	if strings.TrimSpace(event.ToolName) == "" {
		return errToolResultRequiresToolName
	}
	if strings.TrimSpace(event.CallID) == "" {
		return errToolResultRequiresCallID
	}
	if strings.TrimSpace(event.RunID) == "" {
		return errToolResultRequiresRunID
	}
	return nil
}

var (
	errToolCallRequiresToolName    = toolLifecycleError("tool call event requires tool_name")
	errToolLifecycleNotInitialized = toolLifecycleError("tool lifecycle state is not initialized")
	errToolResultRequiresToolName  = toolLifecycleError("tool result event requires tool_name")
	errToolResultRequiresCallID    = toolLifecycleError("tool result event requires call_id")
	errToolResultRequiresRunID     = toolLifecycleError("tool result event requires run_id")
)

type toolLifecycleError string

func (e toolLifecycleError) Error() string { return string(e) }
