package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"path/filepath"
	"sync/atomic"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/config"
	cp "github.com/ycvk/acorn/internal/context"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/memory"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/skills"
	corestore "github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/workspace"
)

type RunnerFactory struct {
	deps RuntimeDeps

	registry     *Registry
	currentRunID atomic.Value
	runIDMu      sync.Mutex

	modelBuilder  *ModelBuilder
	capabilityAsm *CapabilityAssembler
	contextAsm    *ContextAssembler
	mcpAssembler  *MCPAssembler
}

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

func (f *RunnerFactory) BuildCapabilitySpecs(ctx context.Context) ([]tools.ToolSpec, error) {
	toolset, err := f.capabilityAsm.buildToolset(ctx, "", true)
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

func (f *RunnerFactory) MemoryModule() memory.Service {
	return f.deps.MemoryModule
}

func (f *RunnerFactory) SessionSummarySvc() *domain.SessionSummaryService {
	return f.deps.SessionSummarySvc
}

func (f *RunnerFactory) NewChatModel(ctx context.Context) (einomodel.BaseChatModel, error) {
	return f.modelBuilder.newChatModel(ctx)
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
	f.runIDMu.Lock()
	defer f.runIDMu.Unlock()
	f.currentRunID.Store(runID)
}

func (f *RunnerFactory) ClearCurrentRunID(runID string) {
	if runID == "" {
		return
	}
	f.runIDMu.Lock()
	defer f.runIDMu.Unlock()
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

// Close releases the cached MCP manager owned by the MCPAssembler.
func (f *RunnerFactory) Close() error {
	return f.mcpAssembler.Close()
}

// ReconcileMCPProviders reconciles the cached MCP manager's providers.
func (f *RunnerFactory) ReconcileMCPProviders(ctx context.Context, providerConfigs []mcpprovider.ProviderConfig) error {
	return f.mcpAssembler.ReconcileMCPProviders(ctx, providerConfigs)
}

func newInMemoryCheckpointStore() *inMemoryCheckpointStore {
	return &inMemoryCheckpointStore{data: make(map[string][]byte)}
}

type localToolset struct {
	catalog *tools.LocalCatalog
	closers []io.Closer
}

func (f *RunnerFactory) buildRunCapabilityAssembly(ctx context.Context, req RunnerBuildRequest) (*capabilityAssembly, error) {
	if f == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	mcpManager, err := f.mcpAssembler.bootstrapRunMCP(ctx, req)
	if err != nil {
		return nil, err
	}
	capabilities, err := f.capabilityAsm.buildRunCapabilities(ctx, req.SessionID, mcpManager)
	if err != nil {
		return nil, err
	}
	return &capabilityAssembly{mcpManager: mcpManager, capabilities: capabilities}, nil
}

type capabilityAssembly struct {
	mcpManager   *mcpprovider.Manager
	capabilities *runCapabilities
}

func buildRuntimeDeps(cfg *config.Config, store RunnerFactoryStore, opts RunnerFactoryOptions) (RuntimeDeps, error) {
	if cfg == nil {
		return RuntimeDeps{}, errors.New("config is required")
	}
	if store == nil {
		return RuntimeDeps{}, errors.New("store is required")
	}
	ws, err := resolveWorkspace(cfg, opts.Workspace)
	if err != nil {
		return RuntimeDeps{}, fmt.Errorf("workspace: %w", err)
	}
	artifactService, err := buildArtifactService(cfg, store)
	if err != nil {
		return RuntimeDeps{}, fmt.Errorf("artifact service: %w", err)
	}
	if opts.MemoryModule == nil {
		return RuntimeDeps{}, errors.New("memory module is required")
	}
	loader := resolveLoader(cfg, opts.Loader)
	contextPlane, err := resolveContextPlane(cfg, store, opts)
	if err != nil {
		return RuntimeDeps{}, fmt.Errorf("context plane: %w", err)
	}
	return assembleRuntimeDeps(cfg, store, opts, ws, loader, artifactService, contextPlane), nil
}

func resolveLoader(cfg *config.Config, loader *skills.Loader) *skills.Loader {
	if loader == nil {
		return skills.NewLoader(cfg)
	}
	return loader
}

func resolveContextPlane(cfg *config.Config, store RunnerFactoryStore, opts RunnerFactoryOptions) (cp.Plane, error) {
	if opts.ContextPlane != nil {
		return opts.ContextPlane, nil
	}
	return buildDefaultContextPlane(cfg, store, opts)
}

func assembleRuntimeDeps(cfg *config.Config, store RunnerFactoryStore, opts RunnerFactoryOptions, ws *workspace.Workspace, loader *skills.Loader, artifactService *corestore.ArtifactService, contextPlane cp.Plane) RuntimeDeps {
	return RuntimeDeps{
		Config:            cfg,
		Store:             store,
		Loader:            loader,
		SessionSummarySvc: opts.SessionSummaryService,
		MemoryModule:      opts.MemoryModule,
		ContextPlane:      contextPlane,
		MCPPendingActions: opts.MCPPendingActionStore,
		Workspace:         ws,
		ArtifactService:   artifactService,
		ExtraLocalTools:   append([]einotool.BaseTool(nil), opts.ExtraLocalTools...),
		Handlers:          append([]adk.ChatModelAgentMiddleware(nil), opts.Handlers...),
	}
}

func resolveWorkspace(cfg *config.Config, override *workspace.Workspace) (*workspace.Workspace, error) {
	if override != nil {
		return override, nil
	}
	return cfg.Workspace()
}

func buildArtifactService(cfg *config.Config, store RunnerFactoryStore) (*corestore.ArtifactService, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	if strings.TrimSpace(cfg.Runtime.StorageDir) == "" {
		return nil, errors.New("storage_dir is required")
	}
	artifactStore, ok := store.(corestore.ArtifactStore)
	if !ok {
		return nil, errors.New("store must implement corestore.ArtifactStore")
	}
	return corestore.NewArtifactService(filepath.Join(cfg.Runtime.StorageDir, "artifacts"), artifactStore)
}

func buildDefaultContextPlane(cfg *config.Config, store RunnerFactoryStore, opts RunnerFactoryOptions) (cp.Plane, error) {
	memoryBudget, maxContextTokens, tokenCounter, err := resolveContextPlaneTokenPolicy(cfg)
	if err != nil {
		return nil, err
	}
	return cp.NewDefaultPlane(cp.DefaultOptions{
		MemoryContextTokenBudget: memoryBudget,
		MaxContextTokens:         maxContextTokens,
		TokenCounter:             tokenCounter,
	}), nil
}

func resolveContextPlaneTokenPolicy(cfg *config.Config) (memoryBudget, maxContextTokens int, tokenCounter cp.TokenCounter, err error) {
	if cfg == nil {
		return 0, 0, nil, nil
	}
	memoryBudget = cfg.Memory.Search.MemoryContextTokenBudget
	maxContextTokens = cfg.Context.WindowTokens
	tokenCounter, err = cp.NewTokenCounter()
	if err != nil {
		return 0, 0, nil, fmt.Errorf("token counter: %w", err)
	}
	return memoryBudget, maxContextTokens, tokenCounter, nil
}

func assembleRunnerFactory(deps RuntimeDeps) *RunnerFactory {
	return &RunnerFactory{
		deps:          deps,
		registry:      NewRegistry(),
		modelBuilder:  NewModelBuilder(deps.Config),
		capabilityAsm: NewCapabilityAssembler(deps),
		contextAsm:    NewContextAssembler(deps),
		mcpAssembler:  NewMCPAssembler(deps),
	}
}

type runCapabilities struct {
	catalog       *tools.Catalog
	skillSnapshot *skills.Snapshot
	stableSkills  []skills.Spec
	close         func() error
}

func (c *runCapabilities) Close() error {
	if c == nil || c.close == nil {
		return nil
	}
	return c.close()
}

// inMemoryCheckpointStore is a process-local adk.CheckPointStore. The schema
// reduction removed the SQLite-backed checkpoints table; runs re-bootstrap
// their context from persisted messages on resume, so a volatile store is
// sufficient for within-process run/resume continuity.
type inMemoryCheckpointStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (s *inMemoryCheckpointStore) Get(_ context.Context, checkPointID string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, ok := s.data[checkPointID]
	if !ok {
		return nil, false, nil
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	return cp, true, nil
}

func (s *inMemoryCheckpointStore) Set(_ context.Context, checkPointID string, checkPoint []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]byte, len(checkPoint))
	copy(cp, checkPoint)
	s.data[checkPointID] = cp
	return nil
}
func bindToolLifecycle(
	ctx context.Context,
	state ToolLifecycleStateView,
	catalog *tools.Catalog,
	infos []*schema.ToolInfo,
) context.Context {
	if adapter, ok := state.(toolLifecycleStateAdapter); ok && adapter.state != nil {
		return cp.WithToolLifecycleContext(ctx, adapter.state, catalog, infos)
	}
	return ctx
}

func bindSessionID(ctx context.Context, sessionID string) context.Context {
	return domain.WithSessionID(ctx, sessionID)
}

type toolLifecycleStateAdapter struct {
	state *cp.ToolLifecycleState
}

func (a toolLifecycleStateAdapter) IsLoaded(toolName string) bool {
	if a.state == nil {
		return false
	}
	_, ok := a.state.LoadedTools[toolName]
	return ok
}

func AssembleResultToView(result *cp.AssembleResult) AssembleResultView {
	if result == nil {
		return AssembleResultView{}
	}
	return AssembleResultView{
		Messages:          result.Messages,
		LifecycleState:    toolLifecycleStateAdapter{state: result.LifecycleState},
		EagerToolNames:    result.EagerToolNames,
		DeferredToolNames: result.DeferredToolNames,
	}
}
