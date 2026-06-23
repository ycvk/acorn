package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"path/filepath"
	"sync/atomic"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/memory"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/skills"
	corestore "github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/stream"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/workspace"
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

func (f *RunnerFactory) BuildCapabilitySpecs(ctx context.Context) ([]tools.ToolSpec, error) {
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

func (f *RunnerFactory) MemoryModule() memory.Service {
	return f.deps.MemoryModule
}

func (f *RunnerFactory) SessionSummarySvc() *domain.SessionSummaryService {
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
	catalog *tools.LocalCatalog
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

func (f *RunnerFactory) prepareRunMemory(ctx context.Context, req RunnerBuildRequest) (*memory.PrepareResult, error) {
	if f == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	if f.deps.MemoryModule == nil {
		return nil, errors.New("memory module is not initialized")
	}
	workspaceSlug := f.workspaceSlug()
	result, err := f.deps.MemoryModule.Prepare(ctx, memory.PrepareRequest{
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
	return memory.WorkspaceSlug(f.deps.Workspace.Root())
}

func (f *RunnerFactory) emitRunMemoryEvents(ctx context.Context, req RunnerBuildRequest, workspaceSlug string, result *memory.PrepareResult) error {
	if err := emitMemoryPreparedEvent(ctx, f.deps.Store, req, memory.WorkspaceScope(workspaceSlug), result); err != nil {
		return err
	}
	return nil
}

func (f *RunnerFactory) assembleContext(
	ctx context.Context,
	req RunnerBuildRequest,
	caps *runCapabilities,
	selection *runSelection,
	memoryPrepared *memory.PrepareResult,
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
	catalog *tools.Catalog,
	chatModel einomodel.BaseChatModel,
	contextResult *contextplane.AssembleResult,
) (*RunAssembly, error) {
	if f == nil || f.deps.Config == nil {
		return nil, fmt.Errorf("runner factory is not initialized")
	}
	bf := f.baseAssemblyFields(req, catalog, chatModel, contextResult)
	return buildDirectResponse(ctx, f.deps, f.directResponseRequest(bf, req))
}

func (f *RunnerFactory) baseAssemblyFields(req RunnerBuildRequest, catalog *tools.Catalog, chatModel einomodel.BaseChatModel, contextResult *contextplane.AssembleResult) baseAssemblyFields {
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

func (f *RunnerFactory) directResponseRequest(bf baseAssemblyFields, req RunnerBuildRequest) DirectResponseRequest {
	return DirectResponseRequest{
		AgentName:         bf.agentName,
		AgentDescription:  bf.agentDescription,
		SessionID:         bf.sessionID,
		RunID:             bf.runID,
		ChatModel:         bf.chatModel,
		AssistantStreamer: stream.NewDirectAssistantStreamer(f.deps.Store),
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

func (f *RunnerFactory) assembleRunCapabilitiesCatalog(ctx context.Context, toolset *Toolset, mcpManager *mcpprovider.Manager) (*tools.Catalog, error) {
	specs := append([]tools.ToolSpec(nil), toolset.Catalog().Specs()...)
	mcpSpecs, err := f.buildMCPToolSpecs(ctx, mcpManager)
	if err != nil {
		return nil, err
	}
	specs = append(specs, mcpSpecs...)
	return tools.NewCatalog(ctx, specs)
}

type chatModelBuilder func(context.Context, config.ProviderConfig) (einomodel.BaseChatModel, error)

func buildRuntimeChatModel(ctx context.Context, cfg *config.Config, newModel chatModelBuilder) (einomodel.BaseChatModel, error) {
	model, _, err := buildRuntimeChatModelWithProvider(ctx, cfg, newModel)
	return model, err
}

func buildRuntimeChatModelWithProvider(ctx context.Context, cfg *config.Config, newModel chatModelBuilder) (einomodel.BaseChatModel, config.ProviderConfig, error) {
	if cfg == nil {
		return nil, config.ProviderConfig{}, errors.New("config is required")
	}
	if newModel == nil {
		newModel = newOpenAIChatModel
	}

	provider, err := cfg.EnabledProvider()
	if err != nil {
		return nil, config.ProviderConfig{}, err
	}
	model, err := newModel(ctx, provider)
	if err != nil {
		return nil, config.ProviderConfig{}, fmt.Errorf("init provider %s: %w", provider.Name, err)
	}
	return model, provider, nil
}

func newRuntimeChatModel(
	ctx context.Context,
	cfg *config.Config,
	newModel chatModelBuilder,
	_ any,
) (einomodel.BaseChatModel, error) {
	return buildRuntimeChatModel(ctx, cfg, newModel)
}

type capabilityAssembly struct {
	mcpManager   *mcpprovider.Manager
	capabilities *runCapabilities
}

func buildAssembleRequest(req RunnerBuildRequest, caps *runCapabilities, selection *runSelection, memoryPrepared *memory.PrepareResult) contextplane.AssembleRequest {
	var selectedSkill *SelectedSkill
	if selection != nil {
		selectedSkill = selection.selectedSkill
	}
	return contextplane.AssembleRequest{
		RunID:          req.RunID,
		SessionID:      req.SessionID,
		Input:          req.Input,
		SelectedSkill:  selectedSkill,
		SkillSnapshot:  caps.skillSnapshot,
		MemoryPrepared: memoryPrepared,
		ToolCatalog:    caps.catalog,
	}
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

func resolveContextPlane(cfg *config.Config, store RunnerFactoryStore, opts RunnerFactoryOptions) (contextplane.ContextPlane, error) {
	if opts.ContextPlane != nil {
		return opts.ContextPlane, nil
	}
	return buildDefaultContextPlane(cfg, store, opts)
}

func assembleRuntimeDeps(cfg *config.Config, store RunnerFactoryStore, opts RunnerFactoryOptions, ws *workspace.Workspace, loader *skills.Loader, artifactService *corestore.ArtifactService, contextPlane contextplane.ContextPlane) RuntimeDeps {
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

func buildDefaultContextPlane(cfg *config.Config, store RunnerFactoryStore, opts RunnerFactoryOptions) (contextplane.ContextPlane, error) {
	memoryBudget, maxContextTokens, tokenCounter, err := resolveContextPlaneTokenPolicy(cfg)
	if err != nil {
		return nil, err
	}
	return contextplane.NewDefaultContextPlane(contextplane.DefaultOptions{
		MemoryContextTokenBudget: memoryBudget,
		MaxContextTokens:         maxContextTokens,
		TokenCounter:             tokenCounter,
	}), nil
}

func resolveContextPlaneTokenPolicy(cfg *config.Config) (memoryBudget, maxContextTokens int, tokenCounter contextplane.TokenCounter, err error) {
	if cfg == nil {
		return 0, 0, nil, nil
	}
	memoryBudget = cfg.Memory.Search.MemoryContextTokenBudget
	maxContextTokens = cfg.Context.WindowTokens
	tokenCounter, err = contextplane.NewTokenCounter()
	if err != nil {
		return 0, 0, nil, fmt.Errorf("token counter: %w", err)
	}
	return memoryBudget, maxContextTokens, tokenCounter, nil
}

func assembleRunnerFactory(deps RuntimeDeps) *RunnerFactory {
	return &RunnerFactory{
		deps:     deps,
		registry: NewRegistry(),
	}
}

type baseAssemblyFields struct {
	agentName         string
	agentDescription  string
	sessionID         string
	runID             string
	chatModel         einomodel.BaseChatModel
	catalog           *tools.Catalog
	contextResult     AssembleResultView
	allowedToolNames  []string
	excludedToolNames []string
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

// buildRunnerAgentHandlers assembles the chat-model middleware chain. With the
// compaction subpackage removed, compression is driven by the context session
// rather than by model-call middleware; this builder now only appends the
// caller-supplied extra handlers.
func buildRunnerAgentHandlers(
	_ context.Context,
	cfg *config.Config,
	_ contextplane.ContextPlane,
	extraHandlers []adk.ChatModelAgentMiddleware,
	_ einomodel.BaseChatModel,
	_ any,
) ([]adk.ChatModelAgentMiddleware, error) {
	if cfg == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	handlers := make([]adk.ChatModelAgentMiddleware, 0, len(extraHandlers))
	handlers = append(handlers, extraHandlers...)
	return handlers, nil
}

func bindToolLifecycle(
	ctx context.Context,
	state ToolLifecycleStateView,
	catalog *tools.Catalog,
	infos []*schema.ToolInfo,
) context.Context {
	if adapter, ok := state.(toolLifecycleStateAdapter); ok && adapter.state != nil {
		return contextplane.WithToolLifecycleContext(ctx, adapter.state, catalog, infos)
	}
	return ctx
}

func bindSessionID(ctx context.Context, sessionID string) context.Context {
	return domain.WithSessionID(ctx, sessionID)
}

type toolLifecycleStateAdapter struct {
	state *contextplane.ToolLifecycleState
}

func (a toolLifecycleStateAdapter) IsLoaded(toolName string) bool {
	if a.state == nil {
		return false
	}
	_, ok := a.state.LoadedTools[toolName]
	return ok
}

func AssembleResultToView(result *contextplane.AssembleResult) AssembleResultView {
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

// newOpenAIChatModel builds an OpenAI-compatible chat model from provider config.
func newOpenAIChatModel(ctx context.Context, cfg config.ProviderConfig) (einomodel.BaseChatModel, error) {
	chatCfg := &openai.ChatModelConfig{
		APIKey:              cfg.APIKey,
		BaseURL:             cfg.BaseURL,
		Model:               cfg.Model,
		Timeout:             time.Duration(cfg.TimeoutSeconds) * time.Second,
		MaxCompletionTokens: new(cfg.MaxCompletionTokens),
		Temperature:         new(cfg.Temperature),
	}
	if cfg.ReasoningEffort != "" {
		chatCfg.ReasoningEffort = openai.ReasoningEffortLevel(cfg.ReasoningEffort)
	}
	if len(cfg.ExtraFields) > 0 {
		chatCfg.ExtraFields = cfg.ExtraFields
	}
	model, err := openai.NewChatModel(ctx, chatCfg)
	if err != nil {
		return nil, fmt.Errorf("build openai-compatible chat model: %w", err)
	}
	return model, nil
}
