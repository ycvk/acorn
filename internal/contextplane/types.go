package contextplane

import (
	"context"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/workingstate"
)

type ContextPlane interface {
	Assemble(context.Context, AssembleRequest) (*AssembleResult, error)
	OnToolCall(context.Context, ToolCallEvent) error
	OnToolResult(context.Context, ToolResultEvent) error
	DeferredLoad(context.Context, DeferredLoadRequest) (*DeferredLoadResult, error)
	BuildHandlers(context.Context, config.ContextConfig, einomodel.BaseChatModel, CompressionBuildOptions) ([]adk.ChatModelAgentMiddleware, error)
}

type AssembleRequest struct {
	RunID          string
	SessionID      string
	Input          string
	SelectedSkill  *SelectedSkill
	SkillSnapshot  *skills.Snapshot
	DecisionRecord *decision.Record
	MemoryPrepared *memorymodule.PrepareResult
	ToolCatalog    *tooling.Catalog
}

type AssembleResult struct {
	Messages             []*schema.Message
	LifecycleState       *ToolLifecycleState
	EagerToolNames       []string
	DeferredToolNames    []string
	ProcedureActivations []memorymodule.ProcedureActivation
}

type ToolCallEvent struct {
	RunID     string
	SessionID string
	TurnIndex int
	CallID    string
	ToolName  string
	Arguments string
}

type ToolResultEvent struct {
	RunID        string
	SessionID    string
	TurnIndex    int
	CallID       string
	ToolName     string
	Arguments    string
	Result       string
	IsError      bool
	ErrorReason  string
	ResultTokens int
	SideEffects  []store.SideEffectRef
}

type DeferredLoadRequest struct {
	RunID     string
	SessionID string
	Query     string
	ToolNames []string
	Limit     int
}

type DeferredLoadResult struct {
	Messages        []*schema.Message
	LoadedToolNames []string
	AlreadyLoaded   []string
}

type RunContextSnapshotStore interface {
	SaveRunContextSnapshot(context.Context, model.RunContextSnapshot) error
	LoadRunContextSnapshot(context.Context, string) (*model.RunContextSnapshot, error)
}

type ContextBoundaryStore interface {
	SaveContextBoundary(context.Context, model.ContextBoundary) error
	LoadContextBoundary(context.Context, string) (*model.ContextBoundary, error)
	LoadLatestContextBoundary(context.Context, string) (*model.ContextBoundary, error)
	ListContextBoundaries(context.Context, string) ([]model.ContextBoundary, error)
}

type CheckpointService interface {
	Get(context.Context, string) (*workingstate.Checkpoint, error)
}

type SessionSummaryService interface {
	Get(context.Context, string) (*model.SessionSummary, error)
}

type DefaultOptions struct {
	MemoryContextTokenBudget int
	MaxContextTokens         int
	TokenCounter             TokenCounter
	Store                    RunContextSnapshotStore
	CheckpointService        CheckpointService
	SessionSummaryService    SessionSummaryService
	ToolResultLedger         store.ToolResultLedger
	MemoryBudget             LayeredMemoryBudget
}

type defaultContextPlane struct {
	memoryContextTokenBudget int
	maxContextTokens         int
	tokenCounter             TokenCounter
	store                    RunContextSnapshotStore
	checkpointService        CheckpointService
	sessionSummaryService    SessionSummaryService
	toolResultLedger         store.ToolResultLedger
	compressionPipeline      *CompressionPipeline
	memoryBudget             LayeredMemoryBudget
}

type LayeredMemoryBudget struct {
	L1IndexTokens     int
	L2InitialTokens   int
	L3OnDemandReserve int
}

type ToolLifecycleState struct {
	RunID         string
	SessionID     string
	LoadedTools   map[string]LoadedToolRecord
	DeferredTools map[string]DeferredToolRecord
	RecentResults []ToolResultRecord
	MaxAgeTurns   int
	MaxResultRefs int
	mu            sync.Mutex
}

type LoadedToolRecord struct {
	Name       string
	LoadedAt   time.Time
	LoadSource string
}

type DeferredToolRecord struct {
	Name        string
	Reason      string
	Description string
}

type ToolResultRecord struct {
	CallID    string
	ToolName  string
	TurnIndex int
	ResultRef string
	Summary   string
	FullText  string
	IsError   bool
	Prunable  bool
	PrunedAt  *time.Time
}

func NewDefaultContextPlane(opts DefaultOptions) ContextPlane {
	p := &defaultContextPlane{
		memoryContextTokenBudget: opts.MemoryContextTokenBudget,
		maxContextTokens:         opts.MaxContextTokens,
		tokenCounter:             opts.TokenCounter,
		store:                    opts.Store,
		checkpointService:        opts.CheckpointService,
		sessionSummaryService:    opts.SessionSummaryService,
		toolResultLedger:         opts.ToolResultLedger,
		compressionPipeline:      NewCompressionPipeline(),
	}
	if opts.MemoryBudget.L1IndexTokens > 0 || opts.MemoryBudget.L2InitialTokens > 0 || opts.MemoryBudget.L3OnDemandReserve > 0 {
		p.memoryBudget = opts.MemoryBudget
	} else if opts.MemoryContextTokenBudget > 0 {
		p.memoryBudget = defaultLayeredBudgetFromTotal(opts.MemoryContextTokenBudget)
	}
	return p
}

func defaultLayeredBudgetFromTotal(total int) LayeredMemoryBudget {
	return LayeredMemoryBudget{
		L1IndexTokens:     total * 15 / 100,
		L2InitialTokens:   total * 70 / 100,
		L3OnDemandReserve: total * 15 / 100,
	}
}

type CompactLayer string

const (
	CompactLayerMicrocompact CompactLayer = "microcompact"
	CompactLayerAutocompact  CompactLayer = "auto"
	CompactLayerReactive     CompactLayer = "reactive"
)

type PipelineRequest struct {
	Messages           []adk.Message
	ToolInfos          []*schema.ToolInfo
	ToolState          *ToolLifecycleState
	CurrentPlan        string
	RecentTouchedPaths []string
	Trigger            CompactTrigger
	TurnIndex          int
	LastCompactTurn    int
	Pressure           BudgetPressure
	PreviousSummary    string
	PreservePolicy     PreservePolicy
	ModelProfile       ModelProfile
}

type PipelineResult struct {
	Messages      []adk.Message
	LayersApplied []CompactLayer
	TokensFreed   int
	Outcome       *CompressionOutcome
}
