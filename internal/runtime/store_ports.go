package runtime

import (
	"context"

	"github.com/cloudwego/eino/adk"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/events"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/providerusage"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/runtimehistory"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/toolresult"
)

// SessionTurnStore creates fresh session turns.
type SessionTurnStore interface {
	CreateFreshSessionTurn(ctx context.Context, sessionID, title, input string) (int, error)
}

// RunStore manages run lifecycle.
type RunStore interface {
	CreateBoundRunWithParams(ctx context.Context, params store.RunCreateParams) error
	LoadRun(ctx context.Context, runID string) (*events.RunRecord, error)
	FinishRunContext(ctx context.Context, runID string, status events.RunStatus, output, errText string) error
	MarkInterruptedContext(ctx context.Context, runID, output string) error
	UpdateRunOutputContext(ctx context.Context, runID, output string) error
	FindLatestInterruptedRun(ctx context.Context) (*events.RunRecord, error)
}

// EventStore queries and syncs events.
type EventStore interface {
	LoadEvents(ctx context.Context, runID string) ([]events.EventRecord, error)
	LoadEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]events.EventRecord, error)
	SyncAssistantMessageForRun(ctx context.Context, runID string) error
	SyncAssistantMessageForRunStatus(ctx context.Context, runID string, status events.RunStatus) error
	CreateSegmentFromRun(ctx context.Context, runID string, runStatus events.RunStatus) (int64, error)
}

// ArchiveStore manages run archives.
type ArchiveStore interface {
	GetRunArchive(ctx context.Context, runID string) (*runtimehistory.RunArchive, error)
	UpsertRunArchive(ctx context.Context, archive runtimehistory.RunArchive) error
}

// EvidenceStore appends evidence references.
type EvidenceStore interface {
	AppendEvidenceRef(ctx context.Context, resultRef string, ref toolresult.EvidenceRef) (toolresult.Record, error)
}

// ProviderUsageStore queries provider usage records.
type ProviderUsageStore interface {
	ListProviderUsagesByRun(ctx context.Context, runID string) ([]providerusage.Record, error)
}

// ExecutorStore is the store contract required by the Executor.
type ExecutorStore interface {
	adk.CheckPointStore
	contextplane.RunContextSnapshotStore
	toolresult.Ledger
	providerusage.Recorder
	runDecisionStore
	EventAppender
	SessionTurnStore
	RunStore
	EventStore
	ArchiveStore
	runtimeapi.PlanRecordStore
	EvidenceStore
	ProviderUsageStore
}

// RunnerFactoryStore is the store contract required by the RunnerFactory.
type RunnerFactoryStore interface {
	ExecutorStore
	mcpprovider.TokenStore
	mcpprovider.PendingActionStore
}

type runDecisionStore interface {
	SaveRunDecision(context.Context, decision.Record) error
	LoadRunDecision(context.Context, string) (*decision.Record, error)
}
