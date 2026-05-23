package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/crystallization"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/runtimehistory"
	"github.com/ycvk/acorn/internal/toolresult"
)

func (e *Executor) finishCollectedRun(ctx context.Context, runID, input string, state runState, selectedSkill *SelectedSkill, sink StreamSink) (*Result, error) {
	switch {
	case state.failure != nil:
		return e.finishFailedRun(ctx, runID, input, state, selectedSkill, sink)
	case state.interrupt != nil:
		return e.finishInterruptedRun(ctx, runID, state)
	default:
		return e.finishSucceededRun(ctx, runID, input, state, selectedSkill, sink)
	}
}

func (e *Executor) finishFailedRun(ctx context.Context, runID, input string, state runState, selectedSkill *SelectedSkill, sink StreamSink) (*Result, error) {
	durableCtx := DurableContext(ctx)
	if !state.emittedRunFailed && state.failure != nil {
		if err := e.emitRunFailed(durableCtx, runID, sink, state.failure.Error()); err != nil {
			return nil, err
		}
	}
	if err := e.store.FinishRunContext(durableCtx, runID, events.RunStatusFailed, state.lastOutput, state.failure.Error()); err != nil {
		return nil, err
	}
	if err := e.verifyAndRecordSkill(durableCtx, runID, selectedSkill, events.RunStatusFailed, state.lastOutput, sink); err != nil {
		return nil, err
	}
	if err := e.finalizePostRun(durableCtx, runID, events.RunStatusFailed, input, state.lastOutput); err != nil {
		return nil, errors.Join(state.failure, fmt.Errorf("finalize failed run: %w", err))
	}
	summary, err := e.traceSummary(durableCtx, runID)
	if err != nil {
		return nil, err
	}
	return &Result{
		RunID:        runID,
		Status:       events.RunStatusFailed,
		Output:       state.lastOutput,
		Error:        state.failure.Error(),
		TraceSummary: summary,
	}, nil
}

func (e *Executor) finishInterruptedRun(ctx context.Context, runID string, state runState) (*Result, error) {
	durableCtx := DurableContext(ctx)
	if err := e.store.MarkInterruptedContext(durableCtx, runID, state.lastOutput); err != nil {
		return nil, err
	}
	summary, err := e.traceSummary(durableCtx, runID)
	if err != nil {
		return nil, err
	}
	return &Result{
		RunID:        runID,
		Status:       events.RunStatusInterrupted,
		Output:       state.lastOutput,
		Interrupted:  state.interrupt,
		TraceSummary: summary,
	}, nil
}

func (e *Executor) finishSucceededRun(ctx context.Context, runID, input string, state runState, selectedSkill *SelectedSkill, sink StreamSink) (*Result, error) {
	durableCtx := DurableContext(ctx)
	if err := e.store.UpdateRunOutputContext(durableCtx, runID, state.lastOutput); err != nil {
		return nil, err
	}
	if err := e.verifyAndRecordSkill(durableCtx, runID, selectedSkill, events.RunStatusSucceeded, state.lastOutput, sink); err != nil {
		return nil, err
	}
	if err := e.finalizePostRun(durableCtx, runID, events.RunStatusSucceeded, input, state.lastOutput); err != nil {
		return nil, e.recordFinalizationFailure(durableCtx, runID, state.lastOutput, err, sink)
	}
	if e.crystallizer != nil {
		if err := e.runCrystallization(durableCtx, runID, input, state.lastOutput, selectedSkill, sink); err != nil {
			if emitErr := e.emitCrystallizationFailed(durableCtx, runID, err, sink); emitErr != nil {
				return nil, errors.Join(err, fmt.Errorf("emit crystallization failure: %w", emitErr))
			}
		}
	}
	if err := e.emitRunCompleted(durableCtx, runID, state.lastOutput, sink); err != nil {
		return nil, err
	}
	if err := e.store.FinishRunContext(durableCtx, runID, events.RunStatusSucceeded, state.lastOutput, ""); err != nil {
		return nil, err
	}
	summary, err := e.traceSummary(durableCtx, runID)
	if err != nil {
		return nil, err
	}
	return &Result{
		RunID:        runID,
		Status:       events.RunStatusSucceeded,
		Output:       state.lastOutput,
		TraceSummary: summary,
	}, nil
}

func (e *Executor) finalizePostRun(ctx context.Context, runID string, runStatus events.RunStatus, input, output string) error {
	if e == nil || e.store == nil {
		return errors.New("executor store is nil")
	}
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
	if err := e.appendRunHistory(ctx, runID, runStatus, input, output); err != nil {
		return err
	}
	return nil
}

func (e *Executor) persistConversationSegment(ctx context.Context, runID string, runStatus events.RunStatus) error {
	if e == nil || e.store == nil {
		return errors.New("executor store is nil")
	}
	if _, err := e.store.CreateSegmentFromRun(ctx, runID, runStatus); err != nil {
		return fmt.Errorf("create conversation segment: %w", err)
	}
	return nil
}

func (e *Executor) appendRunHistory(ctx context.Context, runID string, runStatus events.RunStatus, input, output string) error {
	if e == nil || e.store == nil || e.runBuilder == nil || e.runBuilder.MemoryModule() == nil {
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
	var filesChanged []string
	if archive != nil {
		filesChanged = append(filesChanged, archive.TouchedPaths...)
	}
	if err := e.runBuilder.MemoryModule().AppendHistory(ctx, memorymodule.HistoryEvent{
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

func (e *Executor) archiveRun(ctx context.Context, runID string, runStatus events.RunStatus) error {
	if e == nil || e.store == nil {
		return errors.New("executor store is nil")
	}
	run, err := e.store.LoadRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("archive run: load run: %w", err)
	}
	records, err := e.store.LoadEvents(ctx, runID)
	if err != nil {
		return fmt.Errorf("archive run: load events: %w", err)
	}
	touchedPaths, toolNames := archiveSignalsFromEvents(records)
	archive := runtimehistory.RunArchive{
		RunID:         run.RunID,
		SessionID:     run.SessionID,
		InputExcerpt:  compactArchiveText(run.Input),
		OutputExcerpt: compactArchiveText(run.Output),
		TouchedPaths:  touchedPaths,
		ToolNames:     toolNames,
		RunStatus:     string(runStatus),
		CreatedAt:     time.Now().UTC(),
	}
	if err := e.store.UpsertRunArchive(ctx, archive); err != nil {
		return fmt.Errorf("archive run: %w", err)
	}
	if e.sessionSummarySvc != nil && strings.TrimSpace(run.SessionID) != "" {
		if _, err := e.sessionSummarySvc.Update(ctx, run.SessionID, run.RunID, string(runStatus), buildSessionSummaryText(*run, toolNames)); err != nil {
			return fmt.Errorf("archive run: update session summary: %w", err)
		}
	}
	return nil
}

func (e *Executor) runCrystallization(ctx context.Context, runID, input, output string, selectedSkill *SelectedSkill, sink StreamSink) error {
	if e == nil || e.crystallizer == nil {
		return nil
	}
	archive, err := e.store.GetRunArchive(ctx, runID)
	if err != nil {
		return fmt.Errorf("load run archive for crystallization: %w", err)
	}
	var toolNames []string
	var touchedPaths []string
	if archive != nil {
		toolNames = archive.ToolNames
		touchedPaths = archive.TouchedPaths
	}
	toolResults, err := e.store.ListByRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("load tool result evidence for crystallization: %w", err)
	}
	res, err := e.crystallizer.Crystallize(ctx, crystallization.CrystallizationRequest{
		RunID:        runID,
		Input:        input,
		Output:       output,
		ToolNames:    toolNames,
		TouchedPaths: touchedPaths,
		EvidenceRefs: crystallizationEvidenceRefs(toolResults),
	})
	if err != nil {
		return err
	}
	if res != nil {
		if err := e.emitCrystallizationVerdict(ctx, runID, res, sink); err != nil {
			return fmt.Errorf("emit crystallization verdict: %w", err)
		}
	}
	return nil
}

func crystallizationEvidenceRefs(records []toolresult.Record) []string {
	if len(records) == 0 {
		return nil
	}
	refs := make([]string, 0, len(records))
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		if record.Status != toolresult.StatusSucceeded {
			continue
		}
		ref := strings.TrimSpace(record.ResultRef)
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		refs = append(refs, ref)
	}
	return refs
}

func (e *Executor) emitCrystallizationFailed(ctx context.Context, runID string, err error, sink StreamSink) error {
	if e == nil {
		return errors.New("executor is nil")
	}
	if e.store == nil {
		return errors.New("executor store is nil")
	}
	_, appendErr := AppendStreamItem(ctx, e.store, sink, StreamItem{
		RunID: runID,
		Kind:  StreamKindCrystallizationFailed,
		Payload: &CrystallizationFailedPayload{
			RunID: runID,
			Error: err.Error(),
		},
	})
	return appendErr
}

func (e *Executor) emitCrystallizationVerdict(ctx context.Context, runID string, res *crystallization.CrystallizationResult, sink StreamSink) error {
	if e == nil {
		return errors.New("executor is nil")
	}
	if e.store == nil {
		return errors.New("executor store is nil")
	}
	_, appendErr := AppendStreamItem(ctx, e.store, sink, StreamItem{
		RunID: runID,
		Kind:  StreamKindCrystallizationVerdict,
		Payload: &CrystallizationVerdictPayload{
			RunID:     runID,
			Verdict:   string(res.Verdict),
			SkillID:   res.SkillID,
			Reason:    res.Reason,
			SimilarTo: res.SimilarTo,
		},
	})
	return appendErr
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
