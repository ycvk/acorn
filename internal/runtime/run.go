package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/memory"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tools"
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
	active, err = f.newDirectResponseRunner(ctx, req, chatModel, capabilityAssembly)
	return active, err
}

func (f *RunnerFactory) newDirectResponseRunner(ctx context.Context, req RunnerBuildRequest, chatModel einomodel.BaseChatModel, capabilityAssembly *capabilityAssembly) (*ActiveRunner, error) {
	if capabilityAssembly == nil || capabilityAssembly.capabilities == nil {
		return nil, errors.New("run capabilities are required")
	}
	capabilities := capabilityAssembly.capabilities
	memoryPrepared, err := f.contextAsm.prepareRunMemory(ctx, req)
	if err != nil {
		return nil, err
	}
	contextResult, err := f.contextAsm.assembleContext(ctx, req, capabilities, nil, memoryPrepared)
	if err != nil {
		return nil, err
	}
	agentAssembly, err := f.contextAsm.buildAssembly(ctx, req, capabilities.catalog, chatModel, contextResult)
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

type RunnerFactoryOptions struct {
	Loader                *skills.Loader
	ExtraLocalTools       []einotool.BaseTool
	Workspace             *workspace.Workspace
	Handlers              []adk.ChatModelAgentMiddleware
	SessionSummaryService *domain.SessionSummaryService
	MemoryModule          memory.Service
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
	Sink              domain.StreamSink
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
	ToolCatalog    *tools.Catalog
	CloseRunTools  func() error
}

func (f *RunnerFactory) registerRunForBuild(req RunnerBuildRequest) (func(), error) {
	rc := &RunContext{RunID: req.RunID, ParentID: strings.TrimSpace("")}
	if err := f.registry.Register(rc); err != nil {
		return nil, fmt.Errorf("register run context: %w", err)
	}
	f.setCurrentRunID(req.RunID)
	return func() {
		f.registry.Clear(req.RunID)
		f.ClearCurrentRunID(req.RunID)
	}, nil
}

func (f *RunnerFactory) buildRunPrerequisites(ctx context.Context, req RunnerBuildRequest) (einomodel.BaseChatModel, *capabilityAssembly, error) {
	chatModel, err := f.modelBuilder.buildRunChatModel(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	capabilityAssembly, err := f.buildRunCapabilityAssembly(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	return chatModel, capabilityAssembly, nil
}
