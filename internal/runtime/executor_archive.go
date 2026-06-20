package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/store"
)

func (e *Executor) finalizePostRun(ctx context.Context, runID string, runStatus events.RunStatus, input, output string) error {
	if err := e.store.SyncAssistantMessageForRunStatus(ctx, runID, runStatus); err != nil {
		return fmt.Errorf("sync assistant message: %w", err)
	}
	if err := e.persistConversationSegment(ctx, runID, runStatus); err != nil {
		return err
	}
	if e.archiveRunFunc == nil {
		return errors.New("run archive finalizer is not initialized")
	}
	if err := e.archiveRunFunc(ctx, runID, runStatus); err != nil {
		return err
	}
	return e.appendRunHistory(ctx, runID, runStatus, input, output)
}

func (e *Executor) persistConversationSegment(ctx context.Context, runID string, runStatus events.RunStatus) error {
	if _, err := e.store.CreateSegmentFromRun(ctx, runID, runStatus); err != nil {
		return fmt.Errorf("create conversation segment: %w", err)
	}
	return nil
}

func (e *Executor) appendRunHistory(ctx context.Context, runID string, runStatus events.RunStatus, input, output string) error {
	if e.runRuntime.MemoryModule() == nil {
		return errors.New("memory module is not initialized")
	}
	run, err := e.store.LoadRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("load run for memory history: %w", err)
	}
	archive, err := e.store.GetRunArchive(ctx, runID)
	if err != nil {
		return fmt.Errorf("load run archive for memory history: %w", err)
	}
	filesChanged := historyFilesChanged(archive)
	if err := e.runRuntime.MemoryModule().AppendHistory(ctx, memorymodule.HistoryEvent{
		SessionID:    run.SessionID,
		RunID:        runID,
		Status:       string(runStatus),
		Summary:      compactArchiveText(strings.TrimSpace(input + " " + output)),
		FilesChanged: filesChanged,
		Timestamp:    time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("append memory history: %w", err)
	}
	return nil
}

func historyFilesChanged(archive *model.RunArchive) []string {
	if archive == nil {
		return nil
	}
	return append([]string(nil), archive.TouchedPaths...)
}

func (e *Executor) archiveRun(ctx context.Context, runID string, runStatus events.RunStatus) error {
	run, err := e.store.LoadRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("archive run: load run: %w", err)
	}
	records, err := e.store.ListByRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("archive run: list tool results: %w", err)
	}
	archive := buildRunArchive(*run, runStatus, records)
	if err := e.store.UpsertRunArchive(ctx, archive); err != nil {
		return fmt.Errorf("archive run: %w", err)
	}
	return e.updateSessionSummary(ctx, *run, runStatus, toolNamesFromToolResults(records))
}

func buildRunArchive(run events.RunRecord, runStatus events.RunStatus, records []store.ToolResultRecord) model.RunArchive {
	return model.RunArchive{
		RunID:         run.RunID,
		SessionID:     run.SessionID,
		InputExcerpt:  compactArchiveText(run.Input),
		OutputExcerpt: compactArchiveText(run.Output),
		TouchedPaths:  archiveSignalsFromToolResults(records),
		ToolNames:     toolNamesFromToolResults(records),
		RunStatus:     string(runStatus),
		CreatedAt:     time.Now().UTC(),
	}
}

func (e *Executor) updateSessionSummary(ctx context.Context, run events.RunRecord, runStatus events.RunStatus, toolNames []string) error {
	if e.sessionSummarySvc == nil || strings.TrimSpace(run.SessionID) == "" {
		return nil
	}
	if _, err := e.sessionSummarySvc.Update(ctx, run.SessionID, run.RunID, string(runStatus), buildSessionSummaryText(run, toolNames)); err != nil {
		return fmt.Errorf("archive run: update session summary: %w", err)
	}
	return nil
}

func buildSessionSummaryText(run events.RunRecord, toolNames []string) string {
	lines := []string{
		"Last request: " + compactArchiveText(run.Input),
		"Last outcome: " + firstNonEmpty(compactArchiveText(run.Output), compactArchiveText(run.Error), string(run.Status)),
	}
	if len(toolNames) > 0 {
		lines = append(lines, "Tools used: "+strings.Join(toolNames, ", "))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
