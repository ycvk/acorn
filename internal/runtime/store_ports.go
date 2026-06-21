package runtime

import (
	"context"

	

	"github.com/ycvk/acorn/internal/events"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/store"
)

// ExecutorStore is the store contract required by the Executor.
type ExecutorStore interface {
	
	runtimeapi.EventAppender
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
}

// RunnerFactoryStore is the store contract required by the RunnerFactory.
// It extends ExecutorStore with the MCP token + pending-action stores needed
// for run bootstrapping.
type RunnerFactoryStore interface {
	ExecutorStore
	mcpprovider.TokenStore
	mcpprovider.PendingActionStore
}
