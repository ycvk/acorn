package runtime

import (
	"context"
	"encoding/gob"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/workspace"
)

func compactText(value string, limit int) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	runes := []rune(trimmed)
	if limit <= 0 || len(runes) <= limit {
		return trimmed, false
	}
	return string(runes[:limit]) + "...", true
}

var registerOnce sync.Once

func RegisterTypes() {
	registerOnce.Do(func() {
		gob.Register(ElicitationInterruptState{})
		gob.Register(&DirectResponseInterruptData{})
	})
}

type ElicitationInterruptInfo struct {
	Kind            string
	ActionID        string
	Message         string
	RequestedSchema any
}

type ElicitationInterruptState struct {
	ActionID string
}

// RuntimeStore is the store contract required by the runtime.
// It composes session persistence with artifact and OAuth token storage.
type RuntimeStore interface {
	core.SessionStore
	core.ArtifactStore
}

type RuntimeDeps struct {
	Config            *config.Config
	Store             RuntimeStore
	Loader            *skills.Loader
	SessionSummarySvc *SessionSummaryService
	MemoryModule      memory.Service
	ContextPlane      Plane
	MCPPendingActions core.SessionStore
	Workspace         *workspace.Workspace
	ArtifactService   core.ArtifactService
	ExtraLocalTools   []einotool.BaseTool
	Handlers          []adk.ChatModelAgentMiddleware
	ToolRegistry      core.ToolRegistry
	// ToolBuilder overrides the default audited tool builder for testing.
	// nil means use BuildAuditedTools.
	ToolBuilder func(ctx context.Context, store RuntimeStore, specs []core.ToolSpec, excludedToolNames []string, allowedToolNames []string, runID string) ([]einotool.BaseTool, error)
	// ToolNodeFactory overrides the default safe parallel tools node for testing.
	// nil means use NewSafeParallelToolsNode.
	ToolNodeFactory func(ctx context.Context, tools []einotool.BaseTool, resolver core.ExecutionPolicyResolver) (tools.ToolInvoker, error)
	// CheckpointStore overrides the default in-memory checkpoint store for testing.
	CheckpointStore adk.CheckPointStore
}

func (d RuntimeDeps) CloneForWorkspace(ws *workspace.Workspace) RuntimeDeps {
	clone := d
	clone.Workspace = ws
	return clone
}

func (e *Executor) bootstrapContextSessionMessages(
	ctx context.Context,
	req core.ExecuteRequest,
	runID string,
	active *ActiveRunner,
) ([]adk.Message, error) {
	if err := e.validateBootstrapDeps(active); err != nil {
		return nil, err
	}
	counter, err := NewTokenCounter()
	if err != nil {
		return nil, fmt.Errorf("build context session token counter: %w", err)
	}
	session := e.buildContextSession(active, e.runRuntime.Config().Context, counter)
	input, err := session.Bootstrap(ctx, BootstrapRequest{
		SessionID:       req.SessionID,
		RunID:           runID,
		TurnIndex:       req.TurnIndex,
		InitialMessages: prepareInitialMessages(req, active),
		Assembly:        active.ContextResult,
	})
	if err != nil {
		return nil, fmt.Errorf("bootstrap context session: %w", err)
	}
	active.ContextSession = session
	return input.Messages, nil
}

func (e *Executor) validateBootstrapDeps(active *ActiveRunner) error {
	if e == nil || e.runRuntime == nil || e.runRuntime.Config() == nil {
		return fmt.Errorf("context session bootstrap requires runtime config")
	}
	if active == nil {
		return fmt.Errorf("context session bootstrap requires active runner")
	}
	return nil
}

func (e *Executor) buildContextSession(active *ActiveRunner, contextPolicy config.ContextConfig, counter TokenCounter) Session {
	return NewDefaultSession(SessionOptions{
		TokenCounter:        counter,
		Model:               active.ChatModel,
		WindowTokens:        contextPolicy.WindowTokens,
		CompactMargin:       contextPolicy.CompactMarginTokens,
		MaskAfterTurns:      contextPolicy.MaskAfterTurns,
		PreserveRecentTurns: contextPolicy.PreserveRecentTurns,
	})
}

func prepareInitialMessages(req core.ExecuteRequest, active *ActiveRunner) []adk.Message {
	initialMessages := append([]adk.Message(nil), req.Messages...)
	if instruction := strings.TrimSpace(active.Instruction); instruction != "" {
		initialMessages = append([]adk.Message{schema.SystemMessage(instruction)}, initialMessages...)
	}
	return initialMessages
}

// --- direct_response orchestration types ---
type DirectResponseRequest struct {
	AgentName         string
	AgentDescription  string
	SessionID         string
	RunID             string
	ChatModel         einomodel.BaseChatModel
	AssistantStreamer core.AssistantStreamer
	Catalog           *tools.Catalog
	ContextResult     AssembleResultView
	AllowedToolNames  []string
	ExcludedToolNames []string
	InstructionSuffix string
}

type RunAssembly struct {
	Runner      *adk.Runner
	Instruction string
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
