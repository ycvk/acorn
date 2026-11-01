package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/runtime"
)

type RunResumeService struct {
	store     core.SessionStore
	resumeRun func(context.Context, string, map[string]any, core.StreamSink) (*runtime.Result, error)

	mu       sync.Mutex
	inflight map[string]struct{}
}

type resumeStatus struct {
	Resumable bool
	Reason    string
}

func NewRunResumeService(store core.SessionStore) *RunResumeService {
	return &RunResumeService{
		store:    store,
		inflight: make(map[string]struct{}),
	}
}

func (s *RunResumeService) WithResume(resumeRun func(context.Context, string, map[string]any, core.StreamSink) (*runtime.Result, error)) *RunResumeService {
	s.resumeRun = resumeRun
	return s
}

func (s *RunResumeService) Resume(ctx context.Context, runID string) (*runtime.Result, error) {
	if s == nil || s.resumeRun == nil {
		return nil, errors.New("resume runner is nil")
	}
	targets, err := s.InferResumeTargets(ctx, runID)
	if err != nil {
		return nil, err
	}
	return s.resumeRun(ctx, runID, targets, nil)
}

// ResumeIfReady resumes runID in the background once it is interrupted and
// every pending action named by its latest interrupt has been decided. It is
// called after every decision, whenever a run settles as interrupted, and by
// ResumeReadyRuns, so whichever happens last starts the resume; the in-flight
// set and the store's interrupted→running claim keep a run from resuming twice.
func (s *RunResumeService) ResumeIfReady(ctx context.Context, runID string) error {
	ready, err := s.readyToResume(ctx, runID)
	if err != nil || !ready {
		return err
	}
	if !s.claim(runID) {
		return nil
	}
	go s.resumeClaimed(context.WithoutCancel(ctx), runID)
	return nil
}

// ResumeReadyRuns calls ResumeIfReady for every interrupted run. serve runs
// it at startup and periodically, so a decided run whose resume was lost
// (process exit, a failed trigger) still continues.
func (s *RunResumeService) ResumeReadyRuns(ctx context.Context) error {
	runs, err := s.store.ListInterruptedRuns(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, run := range runs {
		if err := s.ResumeIfReady(ctx, run.RunID); err != nil {
			errs = append(errs, fmt.Errorf("run %s: %w", run.RunID, err))
		}
	}
	return errors.Join(errs...)
}

func (s *RunResumeService) readyToResume(ctx context.Context, runID string) (bool, error) {
	run, err := s.store.LoadRun(ctx, runID)
	if err != nil {
		return false, err
	}
	if run.Status != core.RunStatusInterrupted {
		return false, nil
	}
	events, err := s.store.LoadEvents(ctx, runID)
	if err != nil {
		return false, err
	}
	contexts, err := latestRootInterruptContexts(events)
	if err != nil {
		return false, fmt.Errorf("run %s: %w", runID, err)
	}
	for _, interrupt := range contexts {
		actionID := interruptInfoField(interrupt.Info, "action_id")
		if actionID == "" {
			continue
		}
		action, err := s.store.LoadPendingAction(ctx, actionID)
		if err != nil {
			return false, err
		}
		if action.Status == core.PendingActionStatusPending {
			return false, nil
		}
	}
	return true, nil
}

func (s *RunResumeService) claim(runID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, busy := s.inflight[runID]; busy {
		return false
	}
	s.inflight[runID] = struct{}{}
	return true
}

func (s *RunResumeService) release(runID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inflight, runID)
}

func (s *RunResumeService) resumeClaimed(ctx context.Context, runID string) {
	result, err := s.Resume(ctx, runID)
	if err != nil {
		s.failRun(ctx, runID, err)
		s.release(runID)
		return
	}
	s.release(runID)
	if result != nil && result.Status == core.RunStatusInterrupted {
		if err := s.ResumeIfReady(ctx, runID); err != nil {
			reportClientBackgroundError(ctx, runID, err)
		}
	}
}

// failRun records a resume failure on a run that the executor did not get to
// finish, so the run does not stay interrupted with no visible error. A
// resume refused while Acorn stops left the run interrupted, and the next
// start resumes it.
func (s *RunResumeService) failRun(ctx context.Context, runID string, cause error) {
	if errors.Is(cause, core.ErrShuttingDown) {
		return
	}
	run, err := s.store.LoadRun(ctx, runID)
	if err != nil {
		reportClientBackgroundError(ctx, runID, errors.Join(cause, err))
		return
	}
	if run.Status == core.RunStatusSucceeded || run.Status == core.RunStatusFailed {
		reportClientBackgroundError(ctx, runID, cause)
		return
	}
	// Another resumer (or process) already claimed the run.
	if errors.Is(cause, core.ErrRunNotInterrupted) && run.Status == core.RunStatusRunning {
		return
	}
	if _, err := s.store.AppendEvent(ctx, runID, "run.failed", map[string]any{"error": cause.Error()}); err != nil {
		reportClientBackgroundError(ctx, runID, errors.Join(cause, err))
		return
	}
	if err := s.store.FinishRun(ctx, runID, core.RunStatusFailed, "", cause.Error()); err != nil {
		reportClientBackgroundError(ctx, runID, errors.Join(cause, err))
	}
}

func (s *RunResumeService) InferResumeTargets(ctx context.Context, runID string) (map[string]any, error) {
	run, items, err := s.loadRunEvents(ctx, runID)
	if err != nil {
		return nil, err
	}
	status := buildResumeStatus(runID, run, items)
	if !status.Resumable {
		return nil, fmt.Errorf("%w: %s", core.ErrRunNotInterrupted, status.Reason)
	}
	contexts, err := latestRootInterruptContexts(items)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", core.ErrRunNotInterrupted, err)
	}
	targets := make(map[string]any, len(contexts))
	for _, ctxItem := range contexts {
		target, err := s.resumeTargetsForContext(ctx, runID, ctxItem)
		if err != nil {
			return nil, err
		}
		maps.Copy(targets, target)
	}
	return targets, nil
}

func (s *RunResumeService) resumeTargetsForContext(ctx context.Context, runID string, interrupt resumeInterruptContext) (map[string]any, error) {
	switch kind := interruptInfoKind(interrupt.Info); kind {
	case "":
		return defaultTargets(interrupt.ID), nil
	case string(core.PendingActionKindOperatorQuestion), string(core.PendingActionKindToolApproval):
		return s.decidedActionTargets(ctx, runID, interrupt, core.PendingActionKind(kind))
	default:
		return nil, fmt.Errorf("run %s interrupt %s has unsupported kind %q", runID, interrupt.ID, kind)
	}
}

// decidedActionTargets resumes an interrupt from the owner's decision on the
// pending action of the given kind that the interrupt registered.
func (s *RunResumeService) decidedActionTargets(ctx context.Context, runID string, interrupt resumeInterruptContext, kind core.PendingActionKind) (map[string]any, error) {
	actionID := interruptInfoField(interrupt.Info, "action_id")
	if actionID == "" {
		return nil, fmt.Errorf("run %s interrupt %s %s is missing action_id", runID, interrupt.ID, kind)
	}
	record, err := s.store.LoadPendingAction(ctx, actionID)
	if err != nil {
		return nil, err
	}
	if record.RunID != runID {
		return nil, fmt.Errorf("run %s interrupt %s %s action %s belongs to run %s", runID, interrupt.ID, kind, actionID, record.RunID)
	}
	if record.Kind != kind {
		return nil, fmt.Errorf("run %s interrupt %s action %s has kind %q", runID, interrupt.ID, actionID, record.Kind)
	}
	if record.Status == core.PendingActionStatusPending {
		return nil, fmt.Errorf("run %s interrupt %s %s action %s is still pending", runID, interrupt.ID, kind, actionID)
	}
	if record.Status != core.PendingActionStatusApproved && record.Status != core.PendingActionStatusRejected {
		return nil, fmt.Errorf("run %s interrupt %s %s action %s has unsupported status %q", runID, interrupt.ID, kind, actionID, record.Status)
	}
	var decision map[string]any
	if err := json.Unmarshal([]byte(record.DecisionJSON), &decision); err != nil {
		return nil, fmt.Errorf("run %s interrupt %s %s action %s has invalid decision_json: %w", runID, interrupt.ID, kind, actionID, err)
	}
	decision["action_id"] = actionID
	return map[string]any{interrupt.ID: decision}, nil
}

func interruptInfoKind(raw any) string {
	return interruptInfoField(raw, "kind")
}

func interruptInfoField(raw any, key string) string {
	info, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	value, ok := info[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func (s *RunResumeService) loadRunEvents(ctx context.Context, runID string) (*core.RunRecord, []core.EventRecord, error) {
	if s == nil || s.store == nil {
		return nil, nil, fmt.Errorf("run resume store is nil")
	}
	run, err := s.store.LoadRun(ctx, runID)
	if err != nil {
		return nil, nil, err
	}
	items, err := s.store.LoadEvents(ctx, runID)
	if err != nil {
		return nil, nil, err
	}
	return run, items, nil
}

func buildResumeStatus(runID string, run *core.RunRecord, items []core.EventRecord) *resumeStatus {
	status := &resumeStatus{}
	if run == nil {
		status.Reason = fmt.Sprintf("run %s is unavailable", runID)
		return status
	}
	switch run.Status {
	case core.RunStatusInterrupted:
		contexts, err := latestRootInterruptContexts(items)
		if err != nil {
			status.Reason = fmt.Sprintf("run %s is interrupted but missing resumable interrupt data: %v", runID, err)
			return status
		}
		status.Resumable = true
		status.Reason = fmt.Sprintf("run %s is interrupted and waiting on %d root actions", runID, len(contexts))
		return status
	case core.RunStatusFailed:
		status.Reason = fmt.Sprintf("run %s failed and cannot be resumed; inspect run detail or start a new client run", runID)
	case core.RunStatusSucceeded:
		status.Reason = fmt.Sprintf("run %s completed and does not need resume", runID)
	case core.RunStatusRunning:
		status.Reason = fmt.Sprintf("run %s is still running and has no persisted interrupt to resume", runID)
	default:
		status.Reason = fmt.Sprintf("run %s is not resumable from status %s", runID, run.Status)
	}
	return status
}

func defaultTargets(interruptID string) map[string]any {
	return map[string]any{
		interruptID: map[string]any{},
	}
}

type resumeInterruptContext struct {
	ID          string `json:"id,omitempty"`
	Address     string `json:"address,omitempty"`
	Info        any    `json:"info,omitempty"`
	IsRootCause bool   `json:"is_root_cause,omitempty"`
}

type runInterruptedPayload struct {
	Interrupt *struct {
		Contexts []resumeInterruptContext `json:"contexts,omitempty"`
	} `json:"interrupt,omitempty"`
}

func latestRootInterruptContexts(raw []core.EventRecord) ([]resumeInterruptContext, error) {
	for _, record := range slices.Backward(raw) {
		if record.Kind != "run.interrupted" {
			continue
		}
		var payload runInterruptedPayload
		data, err := json.Marshal(record.Payload)
		if err != nil {
			return nil, fmt.Errorf("project event %s seq %d payload: %w", record.Kind, record.Sequence, err)
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, fmt.Errorf("project event %s seq %d payload object: %w", record.Kind, record.Sequence, err)
		}
		if payload.Interrupt == nil {
			return nil, errors.New("run.interrupted payload missing interrupt")
		}
		contexts := make([]resumeInterruptContext, 0, len(payload.Interrupt.Contexts))
		for _, ctx := range payload.Interrupt.Contexts {
			if !ctx.IsRootCause {
				continue
			}
			id := strings.TrimSpace(ctx.ID)
			if id == "" {
				return nil, errors.New("interrupt context id is empty")
			}
			ctx.ID = id
			ctx.Info = core.CompactInterruptInfo(ctx.Info)
			contexts = append(contexts, ctx)
		}
		if len(contexts) == 0 {
			return nil, errors.New("run.interrupted has no root interrupt contexts")
		}
		return contexts, nil
	}
	return nil, errors.New("run has no interrupt event to resume")
}
