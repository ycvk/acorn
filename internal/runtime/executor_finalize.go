package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/stream"
)

func (e *Executor) failRunSetup(ctx context.Context, runID string, setupErr error, sink stream.StreamSink) error {
	if strings.TrimSpace(runID) == "" || setupErr == nil {
		return setupErr
	}
	durableCtx := DurableContext(ctx)
	if err := e.emitRunFailed(durableCtx, runID, sink, setupErr.Error()); err != nil {
		return err
	}
	return e.store.FinishRunContext(durableCtx, runID, events.RunStatusFailed, "", setupErr.Error())
}

func (e *Executor) failSetupOrErr(ctx context.Context, runID string, setupErr error, sink stream.StreamSink) error {
	if failErr := e.failRunSetup(ctx, runID, setupErr, sink); failErr != nil {
		return failErr
	}
	return setupErr
}

func (e *Executor) recordFinalizationFailure(ctx context.Context, runID, output string, finalizationErr error, sink stream.StreamSink) error {
	durableCtx := DurableContext(ctx)
	message := fmt.Sprintf("run finalization failed: %v", finalizationErr)
	var errs []error
	if err := e.store.FinishRunContext(durableCtx, runID, events.RunStatusFailed, output, message); err != nil {
		errs = append(errs, fmt.Errorf("mark run failed after finalization failure: %w", err))
	}
	if err := e.emitRunFailed(durableCtx, runID, sink, message); err != nil {
		errs = append(errs, fmt.Errorf("append finalization failure event: %w", err))
	}
	errs = append([]error{finalizationErr}, errs...)
	return errors.Join(errs...)
}

func (e *Executor) verifyAndRecordSkill(ctx context.Context, runID string, selected *SelectedSkill, status events.RunStatus, output string, sink stream.StreamSink) error {
	_ = ctx
	if selected == nil || strings.TrimSpace(runID) == "" || status != events.RunStatusFailed {
		return nil
	}
	_, err := stream.AppendStreamItem(ctx, e.store, sink, stream.StreamItem{
		RunID: runID,
		Kind:  stream.StreamKindSkillFailed,
		Payload: map[string]any{"skill": &stream.StreamSkill{
			SelectedID:    selected.Skill.ID,
			Name:          selected.Skill.Name,
			Source:        selected.Skill.Source,
			Path:          selected.Skill.Path,
			Summary:       selected.Skill.Summary,
			Requirements:  StreamSkillRequirementsFromDomain(selected.Skill.Requires),
			FailureReason: failureReasonForStatus(status, output),
		}},
	})
	return err
}

func (e *Executor) finishCollectedRun(ctx context.Context, runID, input string, state RunState, selectedSkill *SelectedSkill, sink stream.StreamSink) (*Result, error) {
	switch {
	case state.failure != nil:
		return e.finishFailedRun(ctx, runID, input, state, selectedSkill, sink)
	case state.interrupt != nil:
		return e.finishInterruptedRun(ctx, runID, state)
	default:
		return e.finishSucceededRun(ctx, runID, input, state, selectedSkill, sink)
	}
}

func (e *Executor) finishFailedRun(ctx context.Context, runID, input string, state RunState, selectedSkill *SelectedSkill, sink stream.StreamSink) (*Result, error) {
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
	return &Result{
		RunID:  runID,
		Status: events.RunStatusFailed,
		Output: state.lastOutput,
		Error:  state.failure.Error(),
	}, nil
}

func (e *Executor) finishInterruptedRun(ctx context.Context, runID string, state RunState) (*Result, error) {
	durableCtx := DurableContext(ctx)
	if err := e.store.MarkInterruptedContext(durableCtx, runID, state.lastOutput); err != nil {
		return nil, err
	}
	return &Result{
		RunID:       runID,
		Status:      events.RunStatusInterrupted,
		Output:      state.lastOutput,
		Interrupted: state.interrupt,
	}, nil
}

func (e *Executor) finishSucceededRun(ctx context.Context, runID, input string, state RunState, selectedSkill *SelectedSkill, sink stream.StreamSink) (*Result, error) {
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
	if err := e.emitRunCompleted(durableCtx, runID, state.lastOutput, sink); err != nil {
		return nil, err
	}
	if err := e.store.FinishRunContext(durableCtx, runID, events.RunStatusSucceeded, state.lastOutput, ""); err != nil {
		return nil, err
	}
	return &Result{
		RunID:  runID,
		Status: events.RunStatusSucceeded,
		Output: state.lastOutput,
	}, nil
}
