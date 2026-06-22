package orchestration

import (
	"context"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/toolkit"
)

type DirectResponseRequest struct {
	AgentName         string
	AgentDescription  string
	SessionID         string
	RunID             string
	ChatModel         einomodel.BaseChatModel
	AssistantStreamer AssistantStreamer
	Catalog           *toolkit.Catalog
	ContextResult     AssembleResultView
	AllowedToolNames  []string
	ExcludedToolNames []string
	InstructionSuffix string
}

type AssistantStreamRequest struct {
	RunID     string
	MessageID string
	Model     einomodel.BaseChatModel
	Messages  []*schema.Message
	ToolInfos []*schema.ToolInfo
	CallSite  string
}

type AssistantStopReason string

const (
	AssistantStopReasonEndTurn   AssistantStopReason = "end_turn"
	AssistantStopReasonToolCalls AssistantStopReason = "tool_calls"
	AssistantStopReasonMaxOutput AssistantStopReason = "max_output"
	AssistantStopReasonUnknown   AssistantStopReason = "unknown"
)

type AssistantStreamResult struct {
	Message    *schema.Message
	StopReason AssistantStopReason
	RawReason  string
}

type InterleavedStream struct {
	ToolCallCh     chan schema.ToolCall
	FinalMessageCh chan AssistantStreamResult
	ErrCh          chan error
}

type AssistantStreamer interface {
	StreamAssistantMessage(ctx context.Context, req AssistantStreamRequest) (*AssistantStreamResult, error)
	StreamAssistantInterleaved(ctx context.Context, req AssistantStreamRequest) *InterleavedStream
}

type RunAssembly struct {
	Runner      *adk.Runner
	Instruction string
}

// StreamingExecutor submits tool calls and collects results in streaming fashion.
type StreamingExecutor interface {
	Submit(call schema.ToolCall)
	GetRemainingResults(ctx context.Context) ([]*schema.Message, error)
	Discard()
}

// ToolInvoker creates streaming executors for parallel tool execution.
type ToolInvoker interface {
	NewStreamingExecutor(ctx context.Context) StreamingExecutor
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

type ToolLifecycleBinder func(ctx context.Context, state ToolLifecycleStateView, catalog *toolkit.Catalog, infos []*schema.ToolInfo) context.Context

// ToolBuilder constructs the concrete tool set from specs.
type ToolBuilder func(
	ctx context.Context,
	specs []toolkit.ToolSpec,
	excludedToolNames []string,
	allowedToolNames []string,
	runID string,
) ([]einotool.BaseTool, error)

// ToolNodeFactory creates a ToolInvoker from concrete tools.
type ToolNodeFactory func(ctx context.Context, tools []einotool.BaseTool, resolver toolkit.ExecutionPolicyResolver) (ToolInvoker, error)

// InstructionBuilder builds the system instruction from base prompt and suffix.
type InstructionBuilder func(base string, suffix string) string

// HandlersBuilder constructs middleware handlers for the agent.
type HandlersBuilder func(ctx context.Context, chatModel einomodel.BaseChatModel, compressionState any) ([]adk.ChatModelAgentMiddleware, error)

// DefaultPlaneOptions configures a DefaultPlane.
type DefaultPlaneOptions struct {
	SystemPrompt         string
	MaxIterations        int
	CheckpointStore      adk.CheckPointStore
	ToolBuilder          ToolBuilder
	ToolNodeFactory      ToolNodeFactory
	HandlersBuilder      HandlersBuilder
	InstructionBuilder   InstructionBuilder
	ToolLifecycleBinder  ToolLifecycleBinder
	SessionContextBinder func(ctx context.Context, sessionID string) context.Context
}

// DefaultPlane is the single orchestration plane for direct_response mode.
type DefaultPlane struct {
	systemPrompt         string
	maxIterations        int
	checkpointStore      adk.CheckPointStore
	toolBuilder          ToolBuilder
	toolNodeFactory      ToolNodeFactory
	handlersBuilder      HandlersBuilder
	instructionBuilder   InstructionBuilder
	toolLifecycleBinder  ToolLifecycleBinder
	sessionContextBinder func(ctx context.Context, sessionID string) context.Context
}

func NewDefaultPlane(opts DefaultPlaneOptions) *DefaultPlane {
	return &DefaultPlane{
		systemPrompt:         opts.SystemPrompt,
		maxIterations:        opts.MaxIterations,
		checkpointStore:      opts.CheckpointStore,
		toolBuilder:          opts.ToolBuilder,
		toolNodeFactory:      opts.ToolNodeFactory,
		handlersBuilder:      opts.HandlersBuilder,
		instructionBuilder:   opts.InstructionBuilder,
		toolLifecycleBinder:  opts.ToolLifecycleBinder,
		sessionContextBinder: opts.SessionContextBinder,
	}
}
