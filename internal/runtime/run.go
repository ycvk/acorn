package runtime

import (
	"context"
	"errors"

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

func (f *RunnerFactory) buildRun(ctx context.Context, req RunnerBuildRequest) (active *ActiveRunner, err error) {
	if f == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	mode := events.OrchestrationMode(req.OrchestrationMode).Normalize()
	if err = f.validateRunMode(mode); err != nil {
		return nil, err
	}
	cleanup, regErr := f.registerRunForBuild(req)
	if regErr != nil {
		return nil, regErr
	}
	var capabilities *runCapabilities
	defer func() {
		if err == nil {
			return
		}
		cleanup()
		if capabilities != nil {
			_ = capabilities.Close()
		}
	}()
	chatModel, capabilityAssembly, prereqErr := f.buildRunPrerequisites(ctx, req)
	if prereqErr != nil {
		return nil, prereqErr
	}
	capabilities = capabilityAssembly.capabilities
	active, err = f.assembleRunnerByMode(ctx, req, mode, chatModel, capabilityAssembly)
	return active, err
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
	contextResult, err := f.assembleContext(ctx, req, capabilities, nil, memoryPrepared)
	if err != nil {
		return nil, err
	}
	agentAssembly, err := f.buildAssembly(ctx, events.ModeDirectResponse, req, capabilities.catalog, chatModel, contextResult)
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
	return f.buildAssembly(ctx, mode, req, catalog, chatModel, contextResult)
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
