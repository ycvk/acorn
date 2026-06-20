package runtime

import (
	"context"

	"github.com/cloudwego/eino/adk"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/providers"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/store"
)

// ExecutorStore is the store contract required by the Executor. The previously
// narrow SessionTurnStore/RunStore/EventStore/ArchiveStore/EvidenceStore/
// ProviderUsageStore/runDecisionStore interfaces are inlined here (they were
// only embedded, never used standalone), collapsing the consumer-owned port
// surface.
type ExecutorStore interface {
	adk.CheckPointStore
	contextplane.RunContextSnapshotStore
	contextplane.ContextBoundaryStore
	store.ToolResultLedger
	providers.UsageRecorder
	runtimeapi.EventAppender
	runtimeapi.PlanPersistenceStore
	CreateFreshSessionTurn(ctx context.Context, sessionID, title, input string) (int, error)
	CreateBoundRunWithParams(ctx context.Context, params store.RunCreateParams) error
	LoadRun(ctx context.Context, runID string) (*events.RunRecord, error)
	FinishRunContext(ctx context.Context, runID string, status events.RunStatus, output, errText string) error
	MarkInterruptedContext(ctx context.Context, runID, output string) error
	UpdateRunOutputContext(ctx context.Context, runID, output string) error
	LoadEvents(ctx context.Context, runID string) ([]events.EventRecord, error)
	LoadEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]events.EventRecord, error)
	SyncAssistantMessageForRun(ctx context.Context, runID string) error
	SyncAssistantMessageForRunStatus(ctx context.Context, runID string, status events.RunStatus) error
	CreateSegmentFromRun(ctx context.Context, runID string, runStatus events.RunStatus) (int64, error)
	GetRunArchive(ctx context.Context, runID string) (*model.RunArchive, error)
	UpsertRunArchive(ctx context.Context, archive model.RunArchive) error
	AppendEvidenceRef(ctx context.Context, resultRef string, ref store.EvidenceRef) (store.ToolResultRecord, error)
	ListProviderUsagesByRun(ctx context.Context, runID string) ([]providers.UsageRecord, error)
	SaveRunDecision(context.Context, decision.Record) error
	LoadRunDecision(context.Context, string) (*decision.Record, error)
}

// RunnerFactoryStore is the store contract required by the RunnerFactory. It
// extends ExecutorStore with the MCP token + pending-action stores needed for
// run bootstrapping.
type RunnerFactoryStore interface {
	ExecutorStore
	mcpprovider.TokenStore
	mcpprovider.PendingActionStore
}
