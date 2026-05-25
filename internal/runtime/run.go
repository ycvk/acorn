package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/crystallization"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/orchestration"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/runtimehistory"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/workingstate"
	"github.com/ycvk/acorn/internal/workspace"
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
	mode := events.OrchestrationMode(req.OrchestrationMode).Normalize()
	if mode != events.ModeDirectResponse {
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
	case events.ModeDirectResponse, events.ModeSingleAgent, events.ModePlanExecute:
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

	if mode == events.ModeDirectResponse {
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

	selection, err := b.ensureContextSelectionAssembler().ResolveSelection(ctx, req, capabilities)
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
	mode events.OrchestrationMode,
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
	case events.ModePlanExecute:
		return b.factory.buildPlanExecuteAssembly(ctx, req, catalog, chatModel, contextResult)
	case events.ModeSingleAgent:
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

// RunContext represents a single run in the execution tree.
// It replaces the activeRunID/activeSink singleton pattern with
// per-run context that supports parent-child relationships and
// mid-finalization safety.
type RunContext struct {
	RunID    string
	ParentID string   // empty for root runs
	ChildIDs []string // NOT []*RunContext — prevent GC leak (Oracle ruling)
	Depth    int      // 0 for root
	Budget   *RunBudget
	Sink     StreamSink

	// finalizing is true during finishCollectedRun.
	// Children in finalization phase are allowed to complete
	// when InterruptTree is called on a parent.
	finalizing atomic.Bool
}

// IsFinalizing returns true if this run is in its finalization phase.
func (rc *RunContext) IsFinalizing() bool {
	if rc == nil {
		return false
	}
	return rc.finalizing.Load()
}

// SetFinalizing marks the run as entering finalization.
func (rc *RunContext) SetFinalizing() {
	if rc != nil {
		rc.finalizing.Store(true)
	}
}

// RunBudget tracks iteration and token budget for a run.
type RunBudget struct {
	MaxIterations int // from config Agent.MaxIterations
}

// NewRunBudget creates a RunBudget with the given limits.
func NewRunBudget(maxIterations int) *RunBudget {
	return &RunBudget{
		MaxIterations: maxIterations,
	}
}

// Registry provides thread-safe registration of RunContext instances.
// Uses sync.Mutex (NOT sync.Map — Metis ruling #3a for atomic parent-child registration).
type Registry struct {
	mu      sync.Mutex
	entries map[string]*RunContext // keyed by runID
}

// NewRegistry creates a new Registry.
func NewRegistry() *Registry {
	return &Registry{
		entries: make(map[string]*RunContext),
	}
}

// Register atomically adds a RunContext and links it to its parent.
// Returns error if parent not found.
func (r *Registry) Register(ctx *RunContext) error {
	if ctx == nil {
		return fmt.Errorf("run context is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if ctx.ParentID != "" {
		parent, ok := r.entries[ctx.ParentID]
		if !ok {
			return fmt.Errorf("parent run %s not found", ctx.ParentID)
		}
		parent.ChildIDs = append(parent.ChildIDs, ctx.RunID)
		ctx.Depth = parent.Depth + 1
	}

	r.entries[ctx.RunID] = ctx
	return nil
}

// Clear removes a run and all its children from the registry.
func (r *Registry) Clear(runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.entries == nil {
		return
	}

	// BFS to collect all descendant IDs
	queue := []string{runID}
	for len(queue) > 0 {
		currentID := queue[0]
		queue = queue[1:]
		current, ok := r.entries[currentID]
		if !ok {
			continue
		}
		if current.ChildIDs != nil {
			queue = append(queue, current.ChildIDs...)
		}
		delete(r.entries, currentID)
	}
}

// Get returns a RunContext by ID.
func (r *Registry) Get(runID string) (*RunContext, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.entries == nil {
		return nil, false
	}
	ctx, ok := r.entries[runID]
	return ctx, ok
}

// InterruptTree interrupts a run and all its children.
// Children in finalization phase are allowed to complete.
// cancelFuncs maps runID to context.CancelFunc.
// The interrupt order is leaf-to-root: children are cancelled first,
// then the target run itself.
func (r *Registry) InterruptTree(runID string, cancelFuncs map[string]context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.entries == nil {
		return
	}

	// BFS to collect all descendants, then reverse for leaf-to-root order
	var toInterrupt []string
	queue := []string{runID}
	for len(queue) > 0 {
		currentID := queue[0]
		queue = queue[1:]
		current, ok := r.entries[currentID]
		if !ok {
			continue
		}
		// Skip runs in finalization phase
		if current.IsFinalizing() {
			continue
		}
		toInterrupt = append(toInterrupt, currentID)
		if current.ChildIDs != nil {
			queue = append(queue, current.ChildIDs...)
		}
	}

	// Reverse order: interrupt children (leaves) before parents
	for i := len(toInterrupt) - 1; i >= 0; i-- {
		id := toInterrupt[i]
		if cancel, ok := cancelFuncs[id]; ok {
			cancel()
		}
	}
}

type RunController struct {
	activeMu      sync.Mutex
	activeCancels map[string]context.CancelFunc
}

func NewRunController() *RunController {
	return &RunController{}
}

func (c *RunController) Register(runID string, cancel context.CancelFunc) {
	if c == nil || strings.TrimSpace(runID) == "" || cancel == nil {
		return
	}
	c.activeMu.Lock()
	defer c.activeMu.Unlock()
	if c.activeCancels == nil {
		c.activeCancels = make(map[string]context.CancelFunc)
	}
	c.activeCancels[runID] = cancel
}

func (c *RunController) Clear(runID string) {
	if c == nil || strings.TrimSpace(runID) == "" {
		return
	}
	c.activeMu.Lock()
	defer c.activeMu.Unlock()
	if c.activeCancels == nil {
		return
	}
	delete(c.activeCancels, runID)
}

func (c *RunController) Interrupt(runID string) error {
	if c == nil {
		return errors.New("run controller is nil")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("%w: empty run id", ErrRunNotActive)
	}
	c.activeMu.Lock()
	cancel, ok := c.activeCancels[runID]
	c.activeMu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrRunNotActive, runID)
	}
	cancel()
	return nil
}

// InterruptTree interrupts a run and all its children in the execution tree.
// Children in finalization phase are allowed to complete.
func (c *RunController) InterruptTree(runID string, registry *Registry) {
	if c == nil || registry == nil {
		return
	}
	c.activeMu.Lock()
	cancelFuncs := make(map[string]context.CancelFunc, len(c.activeCancels))
	for id, cancel := range c.activeCancels {
		cancelFuncs[id] = cancel
	}
	c.activeMu.Unlock()

	registry.InterruptTree(runID, cancelFuncs)
}

// RunnerFactoryOptions holds the optional dependencies for creating a RunnerFactory.
type RunnerFactoryOptions struct {
	Loader                 *skills.Loader
	DecisionProfileService *decision.ProfileService
	ExtraLocalTools        []einotool.BaseTool
	Workspace              *workspace.Workspace
	Handlers               []adk.ChatModelAgentMiddleware
	CheckpointService      *workingstate.Service
	SessionSummaryService  *runtimehistory.SessionSummaryService
	MemoryModule           memorymodule.Service
	ContextPlane           contextplane.ContextPlane
	MCPPendingActionStore  mcpprovider.PendingActionStore
}

// RunnerBuildRequest holds the parameters for building a new run.
type RunnerBuildRequest struct {
	SessionID         string
	RunID             string
	Input             string
	SkillID           string
	AllowedToolNames  []string
	Sink              StreamSink
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

// RunBuilder is the interface required by Executor to build and manage runs.
type RunBuilder interface {
	New(ctx context.Context, req RunnerBuildRequest) (*ActiveRunner, error)
	Registry() *Registry
	ConsumeEventError(runID string) error
	Config() *config.Config
	MemoryModule() memorymodule.Service
	SessionSummarySvc() *runtimehistory.SessionSummaryService
	NewChatModel(ctx context.Context) (einomodel.BaseChatModel, error)
	Crystallizer() crystallization.Service
}
