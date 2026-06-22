package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/orchestration"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/runtime/tool"
	"github.com/ycvk/acorn/internal/runtime/toolset"
	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/tools"
)

type RunnerFactory struct {
	deps RuntimeDeps

	mu                 sync.Mutex
	cachedManager      *mcpprovider.Manager
	lastSessionOverlay string

	registry     *Registry
	currentRunID atomic.Value

	runChatModelBuilder func(context.Context, RunnerBuildRequest) (einomodel.BaseChatModel, error)
}

const (
	noEligibleSkillMatchReason = "no_eligible_match"
	ambiguousTopScoreReason    = "ambiguous_top_score"
)

func NewRunnerFactory(cfg *config.Config, store RunnerFactoryStore, opts RunnerFactoryOptions) (*RunnerFactory, error) {
	deps, err := buildRuntimeDeps(cfg, store, opts)
	if err != nil {
		return nil, fmt.Errorf("build runtime deps: %w", err)
	}
	return assembleRunnerFactory(deps), nil
}

func (f *RunnerFactory) New(ctx context.Context, req RunnerBuildRequest) (*ActiveRunner, error) {
	return f.buildRun(ctx, req)
}

func (f *RunnerFactory) BuildCapabilitySpecs(ctx context.Context) ([]tooling.ToolSpec, error) {
	toolset, err := f.buildToolset(ctx, "", true)
	if err != nil {
		return nil, err
	}
	specs := toolset.Catalog().Specs()
	for i := range specs {
		specs[i].Tool = nil
	}
	if err := toolset.Close(); err != nil {
		return nil, fmt.Errorf("close capability toolset: %w", err)
	}
	return specs, nil
}

func (f *RunnerFactory) Registry() *Registry {
	return f.registry
}

func (f *RunnerFactory) Config() *config.Config {
	return f.deps.Config
}

func (f *RunnerFactory) MemoryModule() memorymodule.Service {
	return f.deps.MemoryModule
}

func (f *RunnerFactory) SessionSummarySvc() *model.SessionSummaryService {
	return f.deps.SessionSummarySvc
}

func (f *RunnerFactory) NewChatModel(ctx context.Context) (einomodel.BaseChatModel, error) {
	return f.newChatModel(ctx)
}

func (r *ActiveRunner) Close() error {
	var closeErr error
	if r.CloseRunTools != nil {
		closeErr = r.CloseRunTools()
		r.CloseRunTools = nil
	}
	if r.Factory != nil && r.RunID != "" {
		r.Factory.registry.Clear(r.RunID)
		r.Factory.ClearCurrentRunID(r.RunID)
	}
	return closeErr
}

func (f *RunnerFactory) setCurrentRunID(runID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.currentRunID.Store(runID)
}

func (f *RunnerFactory) ClearCurrentRunID(runID string) {
	if runID == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.currentRunIDValue() == runID {
		f.currentRunID.Store("")
	}
}

func (f *RunnerFactory) currentRunIDValue() string {
	value := f.currentRunID.Load()
	runID, ok := value.(string)
	if !ok {
		return ""
	}
	return runID
}

func newInMemoryCheckpointStore() *inMemoryCheckpointStore {
	return &inMemoryCheckpointStore{data: make(map[string][]byte)}
}

type localToolset struct {
	catalog *tools.Catalog
	closers []io.Closer
}

func (f *RunnerFactory) newChatModel(ctx context.Context) (einomodel.BaseChatModel, error) {
	if f == nil || f.deps.Config == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	return newRuntimeChatModel(ctx, f.deps.Config, nil, nil)
}

func (f *RunnerFactory) buildRunChatModel(ctx context.Context, req RunnerBuildRequest) (einomodel.BaseChatModel, error) {
	if f == nil || f.deps.Config == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	if f.runChatModelBuilder != nil {
		return f.runChatModelBuilder(ctx, req)
	}

	return f.newChatModel(ctx)
}

func (f *RunnerFactory) buildRunCapabilityAssembly(ctx context.Context, req RunnerBuildRequest) (*capabilityAssembly, error) {
	if f == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	mcpManager, err := f.bootstrapRunMCP(ctx, req)
	if err != nil {
		return nil, err
	}
	capabilities, err := f.buildRunCapabilities(ctx, req.SessionID, mcpManager)
	if err != nil {
		return nil, err
	}
	return &capabilityAssembly{mcpManager: mcpManager, capabilities: capabilities}, nil
}

func (f *RunnerFactory) prepareRunMemory(ctx context.Context, req RunnerBuildRequest) (*memorymodule.PrepareResult, error) {
	if f == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	if f.deps.MemoryModule == nil {
		return nil, errors.New("memory module is not initialized")
	}
	workspaceSlug := f.workspaceSlug()
	result, err := f.deps.MemoryModule.Prepare(ctx, memorymodule.PrepareRequest{
		RunID:         req.RunID,
		SessionID:     req.SessionID,
		WorkspaceSlug: workspaceSlug,
		UserInput:     req.Input,
	})
	if err != nil {
		return nil, fmt.Errorf("prepare memory: %w", err)
	}
	if err := f.emitRunMemoryEvents(ctx, req, workspaceSlug, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (f *RunnerFactory) workspaceSlug() string {
	if f.deps.Workspace == nil {
		return ""
	}
	return memorymodule.WorkspaceSlug(f.deps.Workspace.Root())
}

func (f *RunnerFactory) emitRunMemoryEvents(ctx context.Context, req RunnerBuildRequest, workspaceSlug string, result *memorymodule.PrepareResult) error {
	if err := emitMemoryPreparedEvent(ctx, f.deps.Store, req, memorymodule.WorkspaceScope(workspaceSlug), result); err != nil {
		return err
	}
	return nil
}

func (f *RunnerFactory) assembleContext(
	ctx context.Context,
	req RunnerBuildRequest,
	caps *runCapabilities,
	selection *runSelection,
	memoryPrepared *memorymodule.PrepareResult,
) (*contextplane.AssembleResult, error) {
	if f == nil || f.deps.ContextPlane == nil {
		return nil, errors.New("context plane is not initialized")
	}
	if caps == nil {
		return nil, errors.New("run capabilities are required")
	}
	result, err := f.deps.ContextPlane.Assemble(ctx, buildAssembleRequest(req, caps, selection, memoryPrepared))
	if err != nil {
		return nil, err
	}
	return result, nil
}

// buildAssembly dispatches to the direct_response orchestration plane,
// reusing the common baseAssemblyFields helper so agent/session/tool fields
// are not duplicated across request constructors.
func (f *RunnerFactory) buildAssembly(
	ctx context.Context,
	req RunnerBuildRequest,
	catalog *tooling.Catalog,
	chatModel einomodel.BaseChatModel,
	contextResult *contextplane.AssembleResult,
) (*orchestration.RunAssembly, error) {
	if f == nil || f.deps.Orchestration == nil {
		return nil, fmt.Errorf("orchestration plane is not initialized")
	}
	bf := f.baseAssemblyFields(req, catalog, chatModel, contextResult)
	return f.deps.Orchestration.BuildDirectResponse(ctx, f.directResponseRequest(bf, req))
}

func (f *RunnerFactory) baseAssemblyFields(req RunnerBuildRequest, catalog *tooling.Catalog, chatModel einomodel.BaseChatModel, contextResult *contextplane.AssembleResult) baseAssemblyFields {
	return baseAssemblyFields{
		agentName:         f.deps.Config.Agent.Name,
		agentDescription:  f.deps.Config.Agent.Description,
		sessionID:         req.SessionID,
		runID:             req.RunID,
		chatModel:         chatModel,
		catalog:           catalog,
		contextResult:     AssembleResultToView(contextResult),
		allowedToolNames:  append([]string(nil), req.AllowedToolNames...),
		excludedToolNames: append([]string(nil), req.ExcludedToolNames...),
	}
}

func (f *RunnerFactory) directResponseRequest(bf baseAssemblyFields, req RunnerBuildRequest) orchestration.DirectResponseRequest {
	return orchestration.DirectResponseRequest{
		AgentName:         bf.agentName,
		AgentDescription:  bf.agentDescription,
		SessionID:         bf.sessionID,
		RunID:             bf.runID,
		ChatModel:         bf.chatModel,
		AssistantStreamer: tool.NewDirectAssistantStreamer(f.deps.Store),
		Catalog:           bf.catalog,
		ContextResult:     bf.contextResult,
		AllowedToolNames:  bf.allowedToolNames,
		ExcludedToolNames: bf.excludedToolNames,
		InstructionSuffix: req.InstructionSuffix,
	}
}

func (f *RunnerFactory) buildRunCapabilities(ctx context.Context, sessionID string, mcpManager *mcpprovider.Manager) (*runCapabilities, error) {
	toolset, err := f.buildRunToolset(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = toolset.Close()
		}
	}()
	catalog, err := f.assembleRunCapabilitiesCatalog(ctx, toolset, mcpManager)
	if err != nil {
		return nil, err
	}
	skillSnapshot, err := loadStableSkillSnapshot(ctx, f.deps.Loader, skillEligibilityContextFromCatalog(catalog))
	if err != nil {
		return nil, err
	}
	return &runCapabilities{
		catalog:       catalog,
		skillSnapshot: skillSnapshot,
		stableSkills:  stableSkillsFromSnapshot(skillSnapshot),
		close:         toolset.Close,
	}, nil
}

func (f *RunnerFactory) assembleRunCapabilitiesCatalog(ctx context.Context, toolset *toolset.Toolset, mcpManager *mcpprovider.Manager) (*tooling.Catalog, error) {
	specs := append([]tooling.ToolSpec(nil), toolset.Catalog().Specs()...)
	mcpSpecs, err := f.buildMCPToolSpecs(ctx, mcpManager)
	if err != nil {
		return nil, err
	}
	specs = append(specs, mcpSpecs...)
	return tooling.NewCatalog(ctx, specs)
}
