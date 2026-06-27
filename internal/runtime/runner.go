package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"sync/atomic"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	mcpprovider "github.com/ycvk/acorn/internal/mcp"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/workspace"
)

type RunnerFactory struct {
	deps RuntimeDeps

	registry     *Registry
	currentRunID atomic.Value
	runIDMu      sync.Mutex

	runChatModelBuilder func(context.Context, RunnerBuildRequest) (einomodel.BaseChatModel, error)
	mcpCache            *mcpManagerCache
	toolRegistry        core.ToolRegistry
}

func NewRunnerFactory(cfg *config.Config, store RuntimeStore, opts RunnerFactoryOptions) (*RunnerFactory, error) {
	deps, err := buildRuntimeDeps(cfg, store, opts)
	if err != nil {
		return nil, fmt.Errorf("build runtime deps: %w", err)
	}
	return assembleRunnerFactory(deps), nil
}

func (f *RunnerFactory) New(ctx context.Context, req RunnerBuildRequest) (*ActiveRunner, error) {
	return f.buildRun(ctx, req)
}

func (f *RunnerFactory) BuildCapabilitySpecs(ctx context.Context) ([]core.ToolSpec, error) {
	toolset, err := buildToolset(ctx, f.deps, "")
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

func (f *RunnerFactory) Config() *config.Config {
	return f.deps.Config
}

func (f *RunnerFactory) MemoryModule() memory.Service {
	return f.deps.MemoryModule
}

func (f *RunnerFactory) NewChatModel(ctx context.Context) (einomodel.BaseChatModel, error) {
	return newChatModel(ctx, f.deps.Config)
}

func (f *RunnerFactory) buildRunChatModel(ctx context.Context, req RunnerBuildRequest) (einomodel.BaseChatModel, error) {
	if f.runChatModelBuilder != nil {
		return f.runChatModelBuilder(ctx, req)
	}
	return newChatModel(ctx, f.deps.Config)
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

// Close releases the cached MCP manager.
func (f *RunnerFactory) Close() error {
	return closeMCPCache(f.mcpCache)
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
	mcpManager, err := bootstrapRunMCP(ctx, f.deps, f.mcpCache, req)
	if err != nil {
		return nil, err
	}
	capabilities, err := buildRunCapabilities(ctx, f.deps, req.SessionID, req.RunID, mcpManager)
	if err != nil {
		return nil, err
	}
	return &capabilityAssembly{mcpManager: mcpManager, capabilities: capabilities}, nil
}

type capabilityAssembly struct {
	mcpManager   *mcpprovider.Manager
	capabilities *runCapabilities
}

func buildRuntimeDeps(cfg *config.Config, store RuntimeStore, opts RunnerFactoryOptions) (RuntimeDeps, error) {
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
	artifactService := opts.ArtifactService
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

func resolveContextPlane(cfg *config.Config, store RuntimeStore, opts RunnerFactoryOptions) (*ContextPlane, error) {
	if opts.ContextPlane != nil {
		return opts.ContextPlane, nil
	}
	return buildDefaultContextPlane(cfg, store, opts)
}

func assembleRuntimeDeps(cfg *config.Config, store RuntimeStore, opts RunnerFactoryOptions, ws *workspace.Workspace, loader *skills.Loader, artifactService core.ArtifactService, contextPlane *ContextPlane) RuntimeDeps {
	return RuntimeDeps{
		Config:            cfg,
		Store:             store,
		Loader:            loader,
		MemoryModule:      opts.MemoryModule,
		ContextPlane:      contextPlane,
		MCPPendingActions: opts.MCPPendingActionStore,
		Workspace:         ws,
		ArtifactService:   artifactService,
		ExtraLocalTools:   append([]einotool.BaseTool(nil), opts.ExtraLocalTools...),
		Handlers:          append([]adk.ChatModelAgentMiddleware(nil), opts.Handlers...),
		ToolRegistry:      opts.ToolRegistry,
	}
}

func resolveWorkspace(cfg *config.Config, override *workspace.Workspace) (*workspace.Workspace, error) {
	if override != nil {
		return override, nil
	}
	return cfg.Workspace()
}

func buildDefaultContextPlane(cfg *config.Config, store RuntimeStore, opts RunnerFactoryOptions) (*ContextPlane, error) {
	memoryBudget, maxContextTokens, tokenCounter, err := resolveContextPlaneTokenPolicy(cfg)
	if err != nil {
		return nil, err
	}
	return NewDefaultPlane(DefaultOptions{
		MemoryContextTokenBudget: memoryBudget,
		MaxContextTokens:         maxContextTokens,
		TokenCounter:             tokenCounter,
	}), nil
}

func resolveContextPlaneTokenPolicy(cfg *config.Config) (memoryBudget, maxContextTokens int, tokenCounter TokenCounter, err error) {
	if cfg == nil {
		return 0, 0, nil, nil
	}
	memoryBudget = cfg.Memory.Search.MemoryContextTokenBudget
	maxContextTokens = cfg.Context.WindowTokens
	tokenCounter, err = NewTokenCounter()
	if err != nil {
		return 0, 0, nil, fmt.Errorf("token counter: %w", err)
	}
	return memoryBudget, maxContextTokens, tokenCounter, nil
}

func assembleRunnerFactory(deps RuntimeDeps) *RunnerFactory {
	return &RunnerFactory{
		deps:         deps,
		registry:     NewRegistry(),
		mcpCache:     &mcpManagerCache{},
		toolRegistry: deps.ToolRegistry,
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
	state *ToolLifecycleState,
	catalog *tools.Catalog,
	infos []*schema.ToolInfo,
) context.Context {
	if state != nil {
		return WithToolLifecycleContext(ctx, state, catalog, infos)
	}
	return ctx
}

func bindSessionID(ctx context.Context, sessionID string) context.Context {
	return core.WithSessionID(ctx, sessionID)
}

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
	if mcpMgr := capabilityAssembly.mcpManager; mcpMgr != nil {
		mcpMgr.SetSamplingExecutor(samplingExecutorAdapter{model: chatModel})
	}
	memoryPrepared, err := prepareRunMemory(ctx, f.deps, req)
	if err != nil {
		return nil, err
	}
	contextResult, err := assembleContext(ctx, f.deps, req, capabilities, memoryPrepared)
	if err != nil {
		return nil, err
	}
	agentAssembly, err := buildDirectResponse(ctx, f.deps, DirectResponseRequest{
		AgentName:         f.deps.Config.Agent.Name,
		AgentDescription:  f.deps.Config.Agent.Description,
		SessionID:         req.SessionID,
		RunID:             req.RunID,
		ChatModel:         chatModel,
		AssistantStreamer: NewDirectAssistantStreamer(f.deps.Store),
		Catalog:           capabilities.catalog,
		ContextResult:     contextResult,
		AllowedToolNames:  append([]string(nil), req.AllowedToolNames...),
		ExcludedToolNames: append([]string(nil), req.ExcludedToolNames...),
		InstructionSuffix: req.InstructionSuffix,
	})
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
	MemoryModule          memory.Service
	ContextPlane          *ContextPlane
	MCPPendingActionStore core.SessionStore
	ArtifactService       core.ArtifactService
	ToolRegistry          core.ToolRegistry
}

// RunnerBuildRequest holds the parameters for building a new run.
type RunnerBuildRequest struct {
	SessionID         string
	RunID             string
	Input             string
	SkillID           string
	AllowedToolNames  []string
	Sink              core.StreamSink
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
	ContextResult  *AssembleResult
	ContextSession Session
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
	chatModel, err := f.buildRunChatModel(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	capabilityAssembly, err := f.buildRunCapabilityAssembly(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	return chatModel, capabilityAssembly, nil
}

const capabilityDiscoveryInstruction = `Capability discovery rules:
- Before answering a capability question or saying you cannot do something, inspect the skill catalog and currently loaded tools already present in context.
- If a relevant skill may exist but the catalog summary is not enough, call skill_list or skill_view before answering.
- If a relevant capability depends on deferred tools, call load_tools before concluding the capability is unavailable.
- Prefer the matching skill and tool path over a generic limitation answer.`

const ambientAgentInstruction = `Ambient agent loop — you are not a code CLI or a chat assistant. You wake when external events arrive (triggers, webhooks, operator messages). Each run follows this cycle:
1. Orient: call worldstate_load to read the world projection you left on last wake. This is your cross-run memory — without it you start from zero every time.
2. Assess: decide whether this event actually needs action. Many fires need no response; silence is a valid outcome. Do not manufacture work.
3. Act: pick the smallest action that resolves the situation. Low-risk tools (read-only) run directly. High-risk actions (deleting, sending, deploying, mutating files, running shell) escalate to the operator via ask_operator with a Decision Card — state what you considered, what you recommend, and the risk.
4. Record: call worldstate_update with the outcome so the next wake has continuity. Facts worth keeping long-term go to remember; worldstate is for decision-relevant now-state, not an append-only log.
5. Stop: when the situation is resolved or blocked on approval, end the run. Do not loop waiting for events — the next trigger will wake you.

file/git/command/web tools are your hands, not your identity. Use them in service of the ambient cycle, not as the default activity.`

func buildStableInstruction(base string, instructionSuffix string) string {
	parts := []string{
		strings.TrimSpace(base),
		strings.TrimSpace(ambientAgentInstruction),
		strings.TrimSpace(capabilityDiscoveryInstruction),
		strings.TrimSpace(instructionSuffix),
	}
	out := make([]string, 0, len(parts))
	for _, item := range parts {
		if strings.TrimSpace(item) != "" {
			out = append(out, strings.TrimSpace(item))
		}
	}
	return strings.Join(out, "\n\n")
}

func skillEligibilityContextFromCatalog(catalog *tools.Catalog) skills.EligibilityContext {
	if catalog == nil {
		return skills.EligibilityContext{}
	}
	return tools.EligibilityContext(catalog, nil)
}

func loadStableSkillSnapshot(ctx context.Context, loader interface {
	ScanSkills(context.Context) (*skills.ScanResult, error)
}, eligibility skills.EligibilityContext) (*skills.Snapshot, error) {
	if loader == nil {
		return nil, nil
	}
	scan, err := loader.ScanSkills(ctx)
	if err != nil {
		return nil, fmt.Errorf("load skills: %w", err)
	}
	if scan == nil {
		return nil, nil
	}
	snapshot, err := skills.BuildSnapshot(*scan, eligibility)
	if err != nil {
		return nil, fmt.Errorf("build skill snapshot: %w", err)
	}
	copied := skills.CopySnapshot(snapshot)
	return &copied, nil
}

func stableSkillsFromSnapshot(snapshot *skills.Snapshot) []skills.Spec {
	if snapshot == nil || len(snapshot.Skills) == 0 {
		return nil
	}
	items := make([]skills.Spec, 0, len(snapshot.Skills))
	for _, item := range snapshot.Skills {
		items = append(items, skills.CopySpec(item.Spec))
	}
	return items
}

func emitMemoryPreparedEvent(ctx context.Context, store core.EventAppender, req RunnerBuildRequest, workspaceScope string, result *memory.PrepareResult) error {
	if store == nil || strings.TrimSpace(req.RunID) == "" {
		return nil
	}
	prepared := &core.StreamMemoryPrepared{
		Query:          strings.TrimSpace(req.Input),
		WorkspaceScope: strings.TrimSpace(workspaceScope),
	}
	if result != nil {
		prepared.NudgeCount = len(result.Nudges)
		prepared.EntryCount = len(result.Entries)
		prepared.Nudges = streamMemoryNudges(result.Nudges)
		prepared.Entries = streamMemoryEntries(result.Entries)
	}
	_, err := AppendStreamItem(ctx, store, req.Sink, core.StreamItem{
		RunID:     req.RunID,
		Kind:      core.StreamKindMemoryPrepared,
		CreatedAt: time.Now().UTC(),
		Payload:   map[string]any{"memory_prepared": prepared},
	})
	return err
}

func streamMemoryNudges(nudges []memory.Nudge) []core.StreamMemoryPreparedNudge {
	out := make([]core.StreamMemoryPreparedNudge, 0, len(nudges))
	for _, n := range nudges {
		out = append(out, core.StreamMemoryPreparedNudge{
			Ref: n.Ref, Kind: n.Kind, Title: n.Title, Status: n.Status, Reason: n.Reason,
		})
	}
	return out
}

func streamMemoryEntries(entries []memory.Entry) []core.StreamMemoryPreparedEntry {
	out := make([]core.StreamMemoryPreparedEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, core.StreamMemoryPreparedEntry{
			Ref: e.Ref, Kind: e.Kind, Title: e.Title,
		})
	}
	return out
}
func streamSkillRequirementsFromDomain(item skills.Requirements) core.StreamSkillRequirements {
	return core.StreamSkillRequirements{
		Tools:    append([]string(nil), item.Tools...),
		Toolsets: append([]string(nil), item.Toolsets...),
		Bins:     append([]string(nil), item.Bins...),
		Env:      append([]string(nil), item.Env...),
	}
}
