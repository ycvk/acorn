package runtime

import (
	"context"

	"github.com/cloudwego/eino/adk"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/events"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/providerusage"
	"github.com/ycvk/acorn/internal/runtimehistory"
	storecore "github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/toolresult"
)

// ExecutorStore is the store contract required by the Executor.
type ExecutorStore interface {
	adk.CheckPointStore
	contextplane.RunContextSnapshotStore
	toolresult.Ledger
	providerusage.Recorder
	runDecisionStore
	EventAppender
	LoadPlanBySession(ctx context.Context, sessionID string) (*storecore.PlanRecord, error)
	SavePlan(ctx context.Context, plan *storecore.PlanRecord) error
	AppendEvidenceRef(ctx context.Context, resultRef string, ref toolresult.EvidenceRef) (toolresult.Record, error)

	CreateFreshSessionTurn(ctx context.Context, sessionID, title, input string) (int, error)
	CreateBoundRunWithParams(ctx context.Context, params storecore.RunCreateParams) error
	LoadRun(ctx context.Context, runID string) (*events.RunRecord, error)
	LoadEvents(ctx context.Context, runID string) ([]events.EventRecord, error)
	LoadEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]events.EventRecord, error)
	FinishRunContext(ctx context.Context, runID string, status events.RunStatus, output, errText string) error
	MarkInterruptedContext(ctx context.Context, runID, output string) error
	UpdateRunOutputContext(ctx context.Context, runID, output string) error
	SyncAssistantMessageForRun(ctx context.Context, runID string) error
	SyncAssistantMessageForRunStatus(ctx context.Context, runID string, status events.RunStatus) error
	CreateSegmentFromRun(ctx context.Context, runID string, runStatus events.RunStatus) (int64, error)
	GetRunArchive(ctx context.Context, runID string) (*runtimehistory.RunArchive, error)
	UpsertRunArchive(ctx context.Context, archive runtimehistory.RunArchive) error
	ListProviderUsagesByRun(ctx context.Context, runID string) ([]providerusage.Record, error)
	FindLatestInterruptedRun(ctx context.Context) (*events.RunRecord, error)
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
