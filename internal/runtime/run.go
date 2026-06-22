package runtime

import (
	"context"
	"errors"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/model"
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
	active, err = f.assembleRunnerByMode(ctx, req, events.ModeDirectResponse, chatModel, capabilityAssembly)
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
		Mcp:           capabilityAssembly.mcpManager,
		Runner:        agentAssembly.Runner,
		Instruction:   agentAssembly.Instruction,
		ChatModel:     chatModel,
		Factory:       f,
		ContextResult: contextResult,
		RunID:         req.RunID,
		ToolCatalog:   capabilities.catalog,
		CloseRunTools: capabilities.Close,
	}, nil
}

// RunnerFactoryOptions holds the optional dependencies for creating a RunnerFactory.
type RunnerFactoryOptions struct {
	Loader                *skills.Loader
	ExtraLocalTools       []einotool.BaseTool
	Workspace             *workspace.Workspace
	Handlers              []adk.ChatModelAgentMiddleware
	CheckpointService     *workingstate.Service
	SessionSummaryService *model.SessionSummaryService
	MemoryModule          memorymodule.Service
	ContextPlane          contextplane.ContextPlane
	MCPPendingActionStore mcpprovider.PendingActionStore
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
}

type ActiveRunner struct {
	Mcp            *mcpprovider.Manager
	Runner         *adk.Runner
	SelectedSkill  *SelectedSkill
	Instruction    string
	ChatModel      einomodel.BaseChatModel
	Factory        *RunnerFactory
	ContextResult  *contextplane.AssembleResult
	ContextSession contextplane.ContextSession
	RunID          string
	ToolCatalog    *tooling.Catalog
	CloseRunTools  func() error
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
