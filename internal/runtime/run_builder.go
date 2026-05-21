package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	einomodel "github.com/cloudwego/eino/components/model"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/tooling"
)

type runBuilder struct {
	factory          *RunnerFactory
	modelProvider    *modelProviderAssembler
	capabilities     *capabilityAssembler
	contextSelection *contextSelectionAssembler
}

func newRunBuilder(factory *RunnerFactory) *runBuilder {
	builder := &runBuilder{factory: factory}
	builder.modelProvider = &modelProviderAssembler{factory: factory}
	builder.capabilities = &capabilityAssembler{factory: factory}
	builder.contextSelection = &contextSelectionAssembler{factory: factory}
	return builder
}

func (b *runBuilder) Build(ctx context.Context, req RunnerBuildRequest) (*ActiveRunner, error) {
	if b == nil || b.factory == nil || b.factory.cfg == nil || b.factory.store == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	f := b.factory
	mode := orchestration.NormalizeOrchestrationMode(req.OrchestrationMode)
	if mode != orchestration.OrchestrationModeDirectResponse {
		if f.workspaceErr != nil {
			return nil, fmt.Errorf("workspace contract: %w", f.workspaceErr)
		}
		if f.workspace == nil {
			return nil, errors.New("workspace contract is not initialized")
		}
	}

	rc := &RunContext{
		RunID:    req.RunID,
		ParentID: strings.TrimSpace(req.ParentRunID),
		Budget:   NewRunBudget(f.cfg.Agent.MaxIterations),
		Sink:     req.Sink,
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
		f.clearCurrentRunID(req.RunID)
	}()

	f.setCurrentRunID(req.RunID)

	switch mode {
	case orchestration.OrchestrationModeDirectResponse, orchestration.OrchestrationModeSingleAgent, orchestration.OrchestrationModePlanExecute:
	default:
		return nil, fmt.Errorf("unsupported orchestration mode %q", mode)
	}

	chatModel, err := b.ensureModelProviderAssembler().BuildRunChatModel(ctx, req)
	if err != nil {
		return nil, err
	}

	capabilityAssembly, err := b.ensureCapabilityAssembler().BuildRunCapabilities(ctx, req)
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

	if mode == orchestration.OrchestrationModeDirectResponse {
		activeRunner, err := b.newDirectResponseRunner(ctx, req, chatModel, capabilityAssembly)
		if err != nil {
			return nil, err
		}
		keepRunContext = true
		return activeRunner, nil
	}

	memoryPrepared, err := b.ensureContextSelectionAssembler().PrepareMemory(ctx, req)
	if err != nil {
		return nil, err
	}

	selection, err := b.ensureContextSelectionAssembler().ResolveSelection(ctx, req, capabilities, chatModel)
	if err != nil {
		return nil, err
	}

	contextResult, err := b.ensureContextSelectionAssembler().AssembleToolContext(ctx, req, capabilities, selection, memoryPrepared)
	if err != nil {
		return nil, err
	}

	agentAssembly, err := b.buildToolEnabledAssembly(ctx, mode, req, capabilities, chatModel, contextResult)
	if err != nil {
		return nil, err
	}

	activeRunner := &ActiveRunner{
		mcp:              capabilityAssembly.mcpManager,
		runner:           agentAssembly.Runner,
		selectedSkill:    copySelectedSkill(selection.selectedSkill),
		instruction:      agentAssembly.Instruction,
		chatModel:        chatModel,
		factory:          f,
		contextResult:    contextResult,
		runID:            req.RunID,
		compressionState: agentAssembly.CompressionState,
		toolCatalog:      capabilities.catalog,
		closeRunTools:    capabilities.Close,
	}
	keepRunContext = true
	return activeRunner, nil
}

func (b *runBuilder) newDirectResponseRunner(ctx context.Context, req RunnerBuildRequest, chatModel einomodel.BaseChatModel, capabilityAssembly *capabilityAssembly) (*ActiveRunner, error) {
	if capabilityAssembly == nil || capabilityAssembly.capabilities == nil {
		return nil, errors.New("run capabilities are required")
	}
	capabilities := capabilityAssembly.capabilities
	memoryPrepared, err := b.ensureContextSelectionAssembler().PrepareMemory(ctx, req)
	if err != nil {
		return nil, err
	}
	contextResult, err := b.ensureContextSelectionAssembler().AssembleDirectContext(ctx, req, memoryPrepared, capabilities.catalog)
	if err != nil {
		return nil, err
	}
	agentAssembly, err := b.factory.buildDirectResponseAssembly(ctx, req, capabilities.catalog, chatModel, contextResult)
	if err != nil {
		return nil, err
	}
	return &ActiveRunner{
		mcp:              capabilityAssembly.mcpManager,
		runner:           agentAssembly.Runner,
		instruction:      agentAssembly.Instruction,
		chatModel:        chatModel,
		factory:          b.factory,
		contextResult:    contextResult,
		runID:            req.RunID,
		compressionState: agentAssembly.CompressionState,
		toolCatalog:      capabilities.catalog,
		closeRunTools:    capabilities.Close,
	}, nil
}

func (b *runBuilder) buildToolEnabledAssembly(
	ctx context.Context,
	mode orchestration.OrchestrationMode,
	req RunnerBuildRequest,
	caps *runCapabilities,
	chatModel einomodel.BaseChatModel,
	contextResult *contextplane.AssembleResult,
) (*orchestration.RunAssembly, error) {
	if b == nil || b.factory == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	var catalog *tooling.Catalog
	if caps != nil {
		catalog = caps.catalog
	}
	switch mode {
	case orchestration.OrchestrationModePlanExecute:
		return b.factory.buildPlanExecuteAssembly(ctx, req, catalog, chatModel, contextResult)
	case orchestration.OrchestrationModeSingleAgent:
		return b.factory.buildSingleAgentAssembly(ctx, req, catalog, chatModel, contextResult)
	default:
		return nil, fmt.Errorf("unsupported orchestration mode %q", mode)
	}
}

func (b *runBuilder) ensureModelProviderAssembler() *modelProviderAssembler {
	if b.modelProvider == nil {
		b.modelProvider = &modelProviderAssembler{factory: b.factory}
	}
	return b.modelProvider
}

func (b *runBuilder) ensureCapabilityAssembler() *capabilityAssembler {
	if b.capabilities == nil {
		b.capabilities = &capabilityAssembler{factory: b.factory}
	}
	return b.capabilities
}

func (b *runBuilder) ensureContextSelectionAssembler() *contextSelectionAssembler {
	if b.contextSelection == nil {
		b.contextSelection = &contextSelectionAssembler{factory: b.factory}
	}
	return b.contextSelection
}

func (f *RunnerFactory) ensureRunBuilder() *runBuilder {
	if f.runBuilder == nil {
		f.runBuilder = newRunBuilder(f)
	}
	return f.runBuilder
}
