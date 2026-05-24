package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	einomodel "github.com/cloudwego/eino/components/model"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/orchestrationmode"
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
	if b == nil || b.factory == nil || b.factory.deps.Config == nil || b.factory.deps.Store == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	f := b.factory
	mode := orchestrationmode.Normalize(req.OrchestrationMode)
	if mode != orchestrationmode.DirectResponse {
		if f.deps.Workspace == nil {
			return nil, errors.New("workspace contract is not initialized")
		}
	}

	rc := &RunContext{
		RunID:    req.RunID,
		ParentID: strings.TrimSpace(req.ParentRunID),
		Budget:   NewRunBudget(f.deps.Config.Agent.MaxIterations),
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
		f.ClearCurrentRunID(req.RunID)
	}()

	f.setCurrentRunID(req.RunID)

	switch mode {
	case orchestrationmode.DirectResponse, orchestrationmode.SingleAgent, orchestrationmode.PlanExecute:
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

	if mode == orchestrationmode.DirectResponse {
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

func (b *runBuilder) newDirectResponseRunner(ctx context.Context, req RunnerBuildRequest, chatModel einomodel.BaseChatModel, capabilityAssembly *capabilityAssembly) (*ActiveRunner, error) {
	if capabilityAssembly == nil || capabilityAssembly.capabilities == nil {
		return nil, errors.New("run capabilities are required")
	}
	capabilities := capabilityAssembly.capabilities
	memoryPrepared, err := b.ensureContextSelectionAssembler().PrepareMemory(ctx, req)
	if err != nil {
		return nil, err
	}
	contextResult, err := b.ensureContextSelectionAssembler().AssembleDirectContext(ctx, req, memoryPrepared, capabilities.skillSnapshot, capabilities.catalog)
	if err != nil {
		return nil, err
	}
	agentAssembly, err := b.factory.buildDirectResponseAssembly(ctx, req, capabilities.catalog, chatModel, contextResult)
	if err != nil {
		return nil, err
	}
	return &ActiveRunner{
		Mcp:              capabilityAssembly.mcpManager,
		Runner:           agentAssembly.Runner,
		Instruction:      agentAssembly.Instruction,
		ChatModel:        chatModel,
		Factory:          b.factory,
		ContextResult:    contextResult,
		RunID:            req.RunID,
		CompressionState: agentAssembly.CompressionState,
		ToolCatalog:      capabilities.catalog,
		CloseRunTools:    capabilities.Close,
	}, nil
}

func (b *runBuilder) buildToolEnabledAssembly(
	ctx context.Context,
	mode orchestrationmode.Mode,
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
	case orchestrationmode.PlanExecute:
		return b.factory.buildPlanExecuteAssembly(ctx, req, catalog, chatModel, contextResult)
	case orchestrationmode.SingleAgent:
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
