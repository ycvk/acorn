package orchestration

import (
	"context"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/tooling"
)

type DirectResponseRequest struct {
	AgentName         string
	AgentDescription  string
	SessionID         string
	RunID             string
	ChatModel         einomodel.BaseChatModel
	AssistantStreamer AssistantStreamer
	Catalog           *tooling.Catalog
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

type SingleAgentRequest struct {
	AgentName         string
	AgentDescription  string
	SessionID         string
	RunID             string
	ChatModel         einomodel.BaseChatModel
	AssistantStreamer AssistantStreamer
	Catalog           *tooling.Catalog
	ContextResult     AssembleResultView
	AllowedToolNames  []string
	ExcludedToolNames []string
	InstructionSuffix string
}

type PlanExecuteRequest struct {
	AgentName         string
	AgentDescription  string
	SessionID         string
	RunID             string
	ChatModel         einomodel.BaseChatModel
	Catalog           *tooling.Catalog
	ContextResult     AssembleResultView
	AllowedToolNames  []string
	ExcludedToolNames []string
	InstructionSuffix string
	ChildExecutor     ChildAgentExecutor
}

type RunAssembly struct {
	Runner           *adk.Runner
	Instruction      string
	CompressionState any
}

type SingleAgentAssembly = RunAssembly

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

type ToolLifecycleBinder func(ctx context.Context, state ToolLifecycleStateView, catalog *tooling.Catalog, infos []*schema.ToolInfo) context.Context
