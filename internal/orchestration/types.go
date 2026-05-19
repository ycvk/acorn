package orchestration

import (
	"context"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/orchestrationmode"
	"github.com/ycvk/acorn/internal/tooling"
)

type PlanningPromptProvider interface {
	BuildPlanningPromptSection(enabledToolNames []string) (string, error)
}

type OrchestrationMode = orchestrationmode.Mode

const (
	OrchestrationModeDirectResponse = orchestrationmode.DirectResponse
	OrchestrationModeSingleAgent    = orchestrationmode.SingleAgent
	OrchestrationModePlanExecute    = orchestrationmode.PlanExecute
)

func NormalizeOrchestrationMode(mode OrchestrationMode) OrchestrationMode {
	return orchestrationmode.Normalize(mode)
}

type DirectResponseRequest struct {
	AgentName         string
	AgentDescription  string
	SessionID         string
	RunID             string
	ChatModel         einomodel.BaseChatModel
	AssistantStreamer AssistantStreamer
	Catalog           *tooling.Catalog
	ContextResult     *contextplane.AssembleResult
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
	AgentName              string
	AgentDescription       string
	SessionID              string
	RunID                  string
	ChatModel              einomodel.BaseChatModel
	AssistantStreamer      AssistantStreamer
	Catalog                *tooling.Catalog
	ContextResult          *contextplane.AssembleResult
	AllowedToolNames       []string
	ExcludedToolNames      []string
	InstructionSuffix      string
	PlanningPromptProvider PlanningPromptProvider
}

type PlanExecuteRequest struct {
	AgentName              string
	AgentDescription       string
	SessionID              string
	RunID                  string
	ChatModel              einomodel.BaseChatModel
	Catalog                *tooling.Catalog
	ContextResult          *contextplane.AssembleResult
	AllowedToolNames       []string
	ExcludedToolNames      []string
	InstructionSuffix      string
	PlanningPromptProvider PlanningPromptProvider
	ChildExecutor          ChildAgentExecutor
}

type RunAssembly struct {
	Runner           *adk.Runner
	Instruction      string
	CompressionState *contextplane.CompressionState
}

type SingleAgentAssembly = RunAssembly

type ToolLifecycleBinder func(ctx context.Context, state *contextplane.ToolLifecycleState, catalog *tooling.Catalog, infos []*schema.ToolInfo) context.Context
