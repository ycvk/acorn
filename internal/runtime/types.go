package runtime

import (
	"context"
	"encoding/gob"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/memory"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/runtime/tooldispatch"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/toolkit"
	"github.com/ycvk/acorn/internal/workspace"
)

func compactText(value string, limit int) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	runes := []rune(trimmed)
	if limit <= 0 || len(runes) <= limit {
		return trimmed, false
	}
	return string(runes[:limit]) + "...", true
}

func NewRunID() string {
	return fmt.Sprintf("run_%d", time.Now().UTC().UnixNano())
}

func newSessionID() string {
	return fmt.Sprintf("session_%d", time.Now().UTC().UnixNano())
}

func InterruptPayloadFromStream(interrupt *domain.StreamInterrupt) map[string]any {
	if interrupt == nil {
		return nil
	}
	payload := map[string]any{"context_count": interrupt.ContextCount}
	contexts := make([]map[string]any, 0, len(interrupt.Contexts))
	for _, item := range interrupt.Contexts {
		contexts = append(contexts, map[string]any{
			"id":            item.ID,
			"address":       item.Address,
			"info":          item.Info,
			"is_root_cause": item.IsRootCause,
		})
	}
	payload["contexts"] = contexts
	return payload
}

func DurableContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

func CurrentRunID(ctx context.Context) string {
	return domain.GetRunID(ctx)
}

var registerOnce sync.Once

// RegisterTypes registers all types required for runtime serialization.
// This replaces the former scattered init() registrations and must be called
// once during application bootstrap before any runtime operations.
// Safe to call multiple times; subsequent calls are no-ops.
func RegisterTypes() {
	registerOnce.Do(func() {
		gob.Register(ElicitationInterruptState{})
		gob.Register(&DirectResponseInterruptData{})
	})
}

type ElicitationInterruptInfo struct {
	Kind            string
	ActionID        string
	Message         string
	RequestedSchema any
}

type ElicitationInterruptState struct {
	ActionID string
}
type SelectedSkill = contextplane.SelectedSkill

type SkillMatch struct {
	Skill          skills.Spec
	Score          int
	MatchedTerms   []string
	TriggerMatched bool
	FilteredReason string
}

func CopySelectedSkill(selected *SelectedSkill) *SelectedSkill {
	return contextplane.CopySelectedSkill(selected)
}

// ExecutorStore is the store contract required by the Executor.
type ExecutorStore interface {
	domain.EventAppender
	CreateFreshSessionTurn(ctx context.Context, sessionID, title, input string) (int, error)
	CreateBoundRunWithParams(ctx context.Context, params store.RunCreateParams) error
	LoadRun(ctx context.Context, runID string) (*domain.RunRecord, error)
	FinishRunContext(ctx context.Context, runID string, status domain.RunStatus, output, errText string) error
	MarkInterruptedContext(ctx context.Context, runID, output string) error
	UpdateRunOutputContext(ctx context.Context, runID, output string) error
	LoadEvents(ctx context.Context, runID string) ([]domain.EventRecord, error)
	LoadEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]domain.EventRecord, error)
	SyncAssistantMessageForRun(ctx context.Context, runID string) error
	SyncAssistantMessageForRunStatus(ctx context.Context, runID string, status domain.RunStatus) error
}

// RunnerFactoryStore is the store contract required by the RunnerFactory.
// It extends ExecutorStore with the MCP token + pending-action stores needed
// for run bootstrapping.
type RunnerFactoryStore interface {
	ExecutorStore
	mcpprovider.TokenStore
	mcpprovider.PendingActionStore
}
type RuntimeDeps struct {
	Config            *config.Config
	Store             RunnerFactoryStore
	Loader            *skills.Loader
	SessionSummarySvc *domain.SessionSummaryService
	MemoryModule      memory.Service
	ContextPlane      contextplane.ContextPlane
	MCPPendingActions mcpprovider.PendingActionStore
	Workspace         *workspace.Workspace
	ArtifactService   *store.ArtifactService
	ExtraLocalTools   []einotool.BaseTool
	Handlers          []adk.ChatModelAgentMiddleware

	// ToolBuilder overrides the default audited tool builder for testing.
	// nil means use BuildAuditedTools.
	ToolBuilder func(ctx context.Context, store RunnerFactoryStore, specs []toolkit.ToolSpec, excludedToolNames []string, allowedToolNames []string, runID string) ([]einotool.BaseTool, error)
	// ToolNodeFactory overrides the default safe parallel tools node for testing.
	// nil means use NewSafeParallelToolsNode.
	ToolNodeFactory func(ctx context.Context, tools []einotool.BaseTool, resolver toolkit.ExecutionPolicyResolver) (tooldispatch.ToolInvoker, error)
	// CheckpointStore overrides the default in-memory checkpoint store for testing.
	CheckpointStore adk.CheckPointStore
}

func (d RuntimeDeps) CloneForWorkspace(ws *workspace.Workspace) RuntimeDeps {
	clone := d
	clone.Workspace = ws
	return clone
}

// RunContext represents a single run in the execution tree. It carries the
// parent-child links used for cascade cleanup of subagent runs.
type RunContext struct {
	RunID    string
	ParentID string   // empty for root runs
	ChildIDs []string // child run IDs (stored as strings, not pointers, to avoid GC retention)
	Depth    int      // 0 for root; propagated to subagents
}

// Registry provides thread-safe registration of RunContext instances.
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

// Clear removes a run and all its descendants from the registry.
func (r *Registry) Clear(runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.entries == nil {
		return
	}

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

// RunController tracks per-run cancellation functions so an in-flight run can
// be interrupted by ID.
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
		return fmt.Errorf("run controller is nil")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("%w: empty run id", domain.ErrRunNotActive)
	}
	c.activeMu.Lock()
	cancel, ok := c.activeCancels[runID]
	c.activeMu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", domain.ErrRunNotActive, runID)
	}
	cancel()
	return nil
}
func (e *Executor) bootstrapContextSessionMessages(
	ctx context.Context,
	req domain.ExecuteRequest,
	runID string,
	active *ActiveRunner,
) ([]adk.Message, error) {
	if err := e.validateBootstrapDeps(active); err != nil {
		return nil, err
	}
	counter, err := contextplane.NewTokenCounter()
	if err != nil {
		return nil, fmt.Errorf("build context session token counter: %w", err)
	}
	session := e.buildContextSession(active, e.runRuntime.Config().Context, counter)
	input, err := session.Bootstrap(ctx, contextplane.BootstrapRequest{
		SessionID:       req.SessionID,
		RunID:           runID,
		TurnIndex:       req.TurnIndex,
		InitialMessages: prepareInitialMessages(req, active),
		Assembly:        active.ContextResult,
	})
	if err != nil {
		return nil, fmt.Errorf("bootstrap context session: %w", err)
	}
	active.ContextSession = session
	return input.Messages, nil
}

func (e *Executor) validateBootstrapDeps(active *ActiveRunner) error {
	if e == nil || e.runRuntime == nil || e.runRuntime.Config() == nil {
		return fmt.Errorf("context session bootstrap requires runtime config")
	}
	if active == nil {
		return fmt.Errorf("context session bootstrap requires active runner")
	}
	return nil
}

func (e *Executor) buildContextSession(active *ActiveRunner, contextPolicy config.ContextConfig, counter contextplane.TokenCounter) contextplane.ContextSession {
	return contextplane.NewDefaultContextSession(contextplane.ContextSessionOptions{
		TokenCounter:        counter,
		Model:               active.ChatModel,
		WindowTokens:        contextPolicy.WindowTokens,
		CompactMargin:       contextPolicy.CompactMarginTokens,
		MaskAfterTurns:      contextPolicy.MaskAfterTurns,
		PreserveRecentTurns: contextPolicy.PreserveRecentTurns,
	})
}

func prepareInitialMessages(req domain.ExecuteRequest, active *ActiveRunner) []adk.Message {
	initialMessages := append([]adk.Message(nil), req.Messages...)
	if instruction := strings.TrimSpace(active.Instruction); instruction != "" {
		initialMessages = append([]adk.Message{schema.SystemMessage(instruction)}, initialMessages...)
	}
	return initialMessages
}

// --- direct_response orchestration types ---

type DirectResponseRequest struct {
	AgentName         string
	AgentDescription  string
	SessionID         string
	RunID             string
	ChatModel         einomodel.BaseChatModel
	AssistantStreamer domain.AssistantStreamer
	Catalog           *toolkit.Catalog
	ContextResult     AssembleResultView
	AllowedToolNames  []string
	ExcludedToolNames []string
	InstructionSuffix string
}

type RunAssembly struct {
	Runner      *adk.Runner
	Instruction string
}

// ToolLifecycleStateView is the read-only view of tool lifecycle state.
type ToolLifecycleStateView interface {
	IsLoaded(toolName string) bool
}

// AssembleResultView is the read-only view of context plane assembly result.
type AssembleResultView struct {
	Messages          []*schema.Message
	LifecycleState    ToolLifecycleStateView
	EagerToolNames    []string
	DeferredToolNames []string
}
