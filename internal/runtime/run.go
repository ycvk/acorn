package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/orchestration"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/stream"
	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/workingstate"
	"github.com/ycvk/acorn/internal/workspace"
)

func (f *RunnerFactory) buildRun(ctx context.Context, req RunnerBuildRequest) (*ActiveRunner, error) {
	if f == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	mode := events.OrchestrationMode(req.OrchestrationMode).Normalize()
	if mode != events.ModeDirectResponse {
		if f.deps.Workspace == nil {
			return nil, errors.New("workspace contract is not initialized")
		}
	}

	rc := &RunContext{
		RunID:    req.RunID,
		ParentID: strings.TrimSpace(req.ParentRunID),
	}
	if err := f.registry.Register(rc); err != nil {
		return nil, fmt.Errorf("register run context: %w", err)
	}
	registeredRunContext := true
	keepRunContext := false
	defer func() {
		if keepRunContext {
			return
		}
		if registeredRunContext {
			f.registry.Clear(req.RunID)
		}
		f.ClearCurrentRunID(req.RunID)
	}()

	f.setCurrentRunID(req.RunID)

	switch mode {
	case events.ModeDirectResponse, events.ModeSingleAgent, events.ModePlanExecute:
	default:
		return nil, fmt.Errorf("unsupported orchestration mode %q", mode)
	}

	chatModel, err := f.buildRunChatModel(ctx, req)
	if err != nil {
		return nil, err
	}

	capabilityAssembly, err := f.buildRunCapabilityAssembly(ctx, req)
	if err != nil {
		return nil, err
	}
	capabilities := capabilityAssembly.capabilities
	defer func() {
		if keepRunContext || capabilities == nil {
			return
		}
		_ = capabilities.Close()
	}()

	if mode == events.ModeDirectResponse {
		activeRunner, err := f.newDirectResponseRunner(ctx, req, chatModel, capabilityAssembly)
		if err != nil {
			return nil, err
		}
		keepRunContext = true
		return activeRunner, nil
	}

	memoryPrepared, err := f.prepareRunMemory(ctx, req)
	if err != nil {
		return nil, err
	}

	selection, err := f.resolveRunSelection(ctx, req, capabilities)
	if err != nil {
		return nil, err
	}

	contextResult, err := f.assembleToolContext(ctx, req, capabilities, selection, memoryPrepared)
	if err != nil {
		return nil, err
	}

	agentAssembly, err := f.buildToolEnabledAssembly(ctx, mode, req, capabilities, chatModel, contextResult)
	if err != nil {
		return nil, err
	}

	activeRunner := &ActiveRunner{
		Mcp:              capabilityAssembly.mcpManager,
		Runner:           agentAssembly.Runner,
		SelectedSkill:    CopySelectedSkill(selection.selectedSkill),
		Instruction:      agentAssembly.Instruction,
		ChatModel:        chatModel,
		Factory:          f,
		ContextResult:    contextResult,
		RunID:            req.RunID,
		CompressionState: agentAssembly.CompressionState,
		ToolCatalog:      capabilities.catalog,
		CloseRunTools:    capabilities.Close,
	}
	keepRunContext = true
	return activeRunner, nil
}

func (f *RunnerFactory) newDirectResponseRunner(ctx context.Context, req RunnerBuildRequest, chatModel einomodel.BaseChatModel, capabilityAssembly *capabilityAssembly) (*ActiveRunner, error) {
	if capabilityAssembly == nil || capabilityAssembly.capabilities == nil {
		return nil, errors.New("run capabilities are required")
	}
	capabilities := capabilityAssembly.capabilities
	memoryPrepared, err := f.prepareRunMemory(ctx, req)
	if err != nil {
		return nil, err
	}
	contextResult, err := f.assembleDirectContext(ctx, req, memoryPrepared, capabilities.skillSnapshot, capabilities.catalog)
	if err != nil {
		return nil, err
	}
	agentAssembly, err := f.buildDirectResponseAssembly(ctx, req, capabilities.catalog, chatModel, contextResult)
	if err != nil {
		return nil, err
	}
	return &ActiveRunner{
		Mcp:              capabilityAssembly.mcpManager,
		Runner:           agentAssembly.Runner,
		Instruction:      agentAssembly.Instruction,
		ChatModel:        chatModel,
		Factory:          f,
		ContextResult:    contextResult,
		RunID:            req.RunID,
		CompressionState: agentAssembly.CompressionState,
		ToolCatalog:      capabilities.catalog,
		CloseRunTools:    capabilities.Close,
	}, nil
}

func (f *RunnerFactory) buildToolEnabledAssembly(
	ctx context.Context,
	mode events.OrchestrationMode,
	req RunnerBuildRequest,
	caps *runCapabilities,
	chatModel einomodel.BaseChatModel,
	contextResult *contextplane.AssembleResult,
) (*orchestration.RunAssembly, error) {
	var catalog *tooling.Catalog
	if caps != nil {
		catalog = caps.catalog
	}
	switch mode {
	case events.ModePlanExecute:
		return f.buildPlanExecuteAssembly(ctx, req, catalog, chatModel, contextResult)
	case events.ModeSingleAgent:
		return f.buildSingleAgentAssembly(ctx, req, catalog, chatModel, contextResult)
	default:
		return nil, fmt.Errorf("unsupported orchestration mode %q", mode)
	}
}

// RunnerFactoryOptions holds the optional dependencies for creating a RunnerFactory.
type RunnerFactoryOptions struct {
	Loader                    *skills.Loader
	DecisionProfileService    *decision.ProfileService
	ExtraLocalTools           []einotool.BaseTool
	Workspace                 *workspace.Workspace
	Handlers                  []adk.ChatModelAgentMiddleware
	CheckpointService         *workingstate.Service
	SessionSummaryService     *model.SessionSummaryService
	MemoryModule              memorymodule.Service
	ContextPlane              contextplane.ContextPlane
	MCPPendingActionStore     mcpprovider.PendingActionStore
	ChildAgentExecutorFactory ChildAgentExecutorFactory
}

// RunnerBuildRequest holds the parameters for building a new run.
type RunnerBuildRequest struct {
	SessionID         string
	RunID             string
	Input             string
	SkillID           string
	AllowedToolNames  []string
	Sink              stream.StreamSink
	ExcludedToolNames []string
	InstructionSuffix string
	OrchestrationMode events.OrchestrationMode
	ParentRunID       string
}

// ActiveRunner represents a fully built and ready-to-execute run.
type ActiveRunner struct {
	Mcp              *mcpprovider.Manager
	Runner           *adk.Runner
	SelectedSkill    *SelectedSkill
	Instruction      string
	ChatModel        einomodel.BaseChatModel
	Factory          *RunnerFactory
	ContextResult    *contextplane.AssembleResult
	ContextSession   contextplane.ContextSession
	RunID            string
	CompressionState any
	ToolCatalog      *tooling.Catalog
	CloseRunTools    func() error
}

// RunRuntime is the execution runtime facade required by Executor.
type RunRuntime interface {
	New(ctx context.Context, req RunnerBuildRequest) (*ActiveRunner, error)
	Registry() *Registry
	Config() *config.Config
	MemoryModule() memorymodule.Service
	SessionSummarySvc() *model.SessionSummaryService
	NewChatModel(ctx context.Context) (einomodel.BaseChatModel, error)
}
