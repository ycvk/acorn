package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/memorymodule"
)

func (e *Executor) finalizePostRun(ctx context.Context, runID string, runStatus events.RunStatus, input, output string) error {
	if err := e.store.SyncAssistantMessageForRunStatus(ctx, runID, runStatus); err != nil {
		return fmt.Errorf("sync assistant message: %w", err)
	}
	return e.appendRunHistory(ctx, runID, runStatus, input, output)
}

func (e *Executor) appendRunHistory(ctx context.Context, runID string, runStatus events.RunStatus, input, output string) error {
	if e.runRuntime.MemoryModule() == nil {
		return errors.New("memory module is not initialized")
	}
	run, err := e.store.LoadRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("load run for memory history: %w", err)
	}
	if err := e.runRuntime.MemoryModule().AppendHistory(ctx, memorymodule.HistoryEvent{
		SessionID: run.SessionID,
		RunID:     runID,
		Status:    string(runStatus),
		Summary:   compactArchiveText(strings.TrimSpace(input + " " + output)),
		Timestamp: time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("append memory history: %w", err)
	}
	return nil
}
