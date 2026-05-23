package orchestration

import (
	"context"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// SessionOwner is the subset of contextplane.ContextSession used by orchestration.
type SessionOwner interface {
	BeforeModelCall(context.Context, ModelCallRequest) (*ModelInput, error)
	ReactiveCompact(context.Context, ModelCallRequest, error) (*ModelInput, error)
	RecordMessages(context.Context, []adk.Message) error
	RecordAssistant(context.Context, adk.Message) error
	RecordToolResults(context.Context, []adk.Message) error
}

// ModelCallRequest is the subset of contextplane.ModelCallRequest used by orchestration.
type ModelCallRequest struct {
	CallID       string
	QuerySource  string
	AllowCompact bool
	ToolInfos    []*schema.ToolInfo
}

// ModelInput is the subset of contextplane.ModelInput used by orchestration.
type ModelInput struct {
	Messages []adk.Message
}

// ToolLifecycleStateView is the read-only view of tool lifecycle state.
type ToolLifecycleStateView interface {
	IsLoaded(toolName string) bool
}

// AssembleResultView is the read-only view of context plane assembly result.
type AssembleResultView struct {
	Messages          []*schema.Message
	LifecycleState    ToolLifecycleStateView
	EagerToolNames    []string
	DeferredToolNames []string
}

// DefaultOverflowChecker implements ContextOverflowChecker using the standard check.
type DefaultOverflowChecker struct{}

// IsContextOverflowError reports whether err is a context-overflow error.
func (DefaultOverflowChecker) IsContextOverflowError(err error) bool {
	return err != nil && isContextOverflowError(err)
}

// isContextOverflowError is the standard check; it may be overridden in tests.
var isContextOverflowError = func(err error) bool {
	// Default implementation: inspect error message or type.
	// This is a placeholder that will be wired to contextplane.IsContextOverflowError
	// by runtime during adapter setup.
	return false
}
