package contextplane

import (
	"context"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
)

// PolicyProvider provides context policy configuration.
type PolicyProvider interface {
	ContextPolicy() (ContextPolicyView, error)
}

// ContextPolicyView is a read-only view of context policy.
type ContextPolicyView interface {
	WindowTokens() int
	CompactMarginTokens() int
	PreserveRecentTurns() int
	SummaryMaxTokens() int
	HandoffFrameDisabled() bool
}

// BuildHandlersRequest is the request to build compression middleware.
type BuildHandlersRequest struct {
	Policy            PolicyProvider
	ChatModel         einomodel.BaseChatModel
	RuntimeStorageDir string
	State             any
	EmitCompressed    func(context.Context, CompressionOutcome) error
	EmitPressure      func(context.Context, BudgetPressure) error
}

// ContextPlaneV2 is a revised ContextPlane interface with reduced dependencies.
type ContextPlaneV2 interface {
	Assemble(context.Context, AssembleRequest) (*AssembleResult, error)
	OnToolCall(context.Context, ToolCallEvent) error
	OnToolResult(context.Context, ToolResultEvent) error
	DeferredLoad(context.Context, DeferredLoadRequest) (*DeferredLoadResult, error)
	Budget(context.Context, BudgetRequest) (BudgetStatus, error)
	BuildHandlersV2(context.Context, BuildHandlersRequest) ([]adk.ChatModelAgentMiddleware, error)
}
