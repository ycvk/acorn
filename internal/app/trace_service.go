package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/artifacts"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/providerusage"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/runtimehistory"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/workspace"
)

type TraceService struct {
	store traceStore
}

type ResumeStatus struct {
	RunID        string           `json:"run_id,omitempty"`
	Status       events.RunStatus `json:"status,omitempty"`
	Resumable    bool             `json:"resumable"`
	InterruptIDs []string         `json:"interrupt_ids,omitempty"`
	Reason       string           `json:"reason,omitempty"`
}

func NewTraceService(store traceStore) *TraceService {
	return &TraceService{store: store}
}

func (s *TraceService) Trace(ctx context.Context, runID string) (*runtime.Trace, error) {
	run, items, err := s.loadRunEvents(ctx, runID)
	if err != nil {
		return nil, err
	}
	trace := runtime.BuildTrace(run, items)
	if trace == nil {
		return nil, nil
	}
	return trace, nil
}

func (s *TraceService) ResumeStatus(ctx context.Context, runID string) (*ResumeStatus, error) {
	run, items, err := s.loadRunEvents(ctx, runID)
	if err != nil {
		return nil, err
	}
	status := buildResumeStatus(runID, run, items)
	if status == nil || run == nil || run.Status != events.RunStatusInterrupted {
		return status, nil
	}
	targets, inferReason := s.inferResumeTargetsOrReason(ctx, runID)
	if inferReason != "" {
		status.Resumable = false
		status.Reason = inferReason
		return status, nil
	}
	status.Resumable = true
	status.Reason = fmt.Sprintf("run %s is interrupted and resumable via %d pending actions", runID, len(targets))
	return status, nil
}

func (s *TraceService) inferResumeTargetsOrReason(ctx context.Context, runID string) (map[string]any, string) {
	targets, err := s.InferResumeTargets(ctx, runID)
	if err != nil {
		return nil, err.Error()
	}
	return targets, ""
}

func (s *TraceService) InferResumeTargets(ctx context.Context, runID string) (map[string]any, error) {
	run, items, err := s.loadRunEvents(ctx, runID)
	if err != nil {
		return nil, err
	}
	status := buildResumeStatus(runID, run, items)
	if !status.Resumable {
		return nil, fmt.Errorf("%w: %s", runtime.ErrRunNotInterrupted, status.Reason)
	}
	contexts, err := runtime.LatestRootInterruptContexts(items)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", runtime.ErrRunNotInterrupted, err)
	}
	targets := make(map[string]any, len(contexts))
	for _, ctxItem := range contexts {
		target, err := s.resumeTargetsForContext(ctx, runID, ctxItem)
		if err != nil {
			return nil, err
		}
		for interruptID, payload := range target {
			targets[interruptID] = payload
		}
	}
	return targets, nil
}

func (s *TraceService) resumeTargetsForContext(ctx context.Context, runID string, interrupt runtime.StreamInterruptContext) (map[string]any, error) {
	switch kind := interruptInfoKind(interrupt.Info); kind {
	case "", "run_command_pause":
		return defaultTargets(interrupt.ID), nil
	case "operator_question":
		return s.operatorQuestionTargets(ctx, runID, interrupt)
	default:
		return nil, fmt.Errorf("run %s interrupt %s has unsupported kind %q", runID, interrupt.ID, kind)
	}
}

func (s *TraceService) operatorQuestionTargets(ctx context.Context, runID string, interrupt runtime.StreamInterruptContext) (map[string]any, error) {
	actionID := interruptInfoField(interrupt.Info, "action_id")
	if actionID == "" {
		return nil, fmt.Errorf("run %s interrupt %s operator_question is missing action_id", runID, interrupt.ID)
	}
	record, err := s.store.LoadPendingAction(ctx, actionID)
	if err != nil {
		return nil, err
	}
	if record.RunID != runID {
		return nil, fmt.Errorf("run %s interrupt %s operator_question action %s belongs to run %s", runID, interrupt.ID, actionID, record.RunID)
	}
	if record.Kind != events.PendingActionKindOperatorQuestion {
		return nil, fmt.Errorf("run %s interrupt %s action %s has kind %q", runID, interrupt.ID, actionID, record.Kind)
	}
	if record.Status == events.PendingActionStatusPending {
		return nil, fmt.Errorf("run %s interrupt %s operator_question action %s is still pending", runID, interrupt.ID, actionID)
	}
	if record.Status != events.PendingActionStatusApproved && record.Status != events.PendingActionStatusRejected {
		return nil, fmt.Errorf("run %s interrupt %s operator_question action %s has unsupported status %q", runID, interrupt.ID, actionID, record.Status)
	}
	var decision map[string]any
	if err := json.Unmarshal([]byte(record.DecisionJSON), &decision); err != nil {
		return nil, fmt.Errorf("run %s interrupt %s operator_question action %s has invalid decision_json: %w", runID, interrupt.ID, actionID, err)
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

func (s *TraceService) loadRunEvents(ctx context.Context, runID string) (*events.RunRecord, []events.EventRecord, error) {
	if s == nil || s.store == nil {
		return nil, nil, fmt.Errorf("trace store is nil")
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

func buildResumeStatus(runID string, run *events.RunRecord, items []events.EventRecord) *ResumeStatus {
	status := &ResumeStatus{RunID: runID}
	if run == nil {
		status.Reason = fmt.Sprintf("run %s is unavailable", runID)
		return status
	}
	status.Status = run.Status

	switch run.Status {
	case events.RunStatusInterrupted:
		interruptIDs, err := runtime.LatestRootInterruptIDs(items)
		if err != nil {
			status.Reason = fmt.Sprintf("run %s is interrupted but missing resumable interrupt data: %v", runID, err)
			return status
		}
		status.Resumable = true
		status.InterruptIDs = append(status.InterruptIDs, interruptIDs...)
		status.Reason = fmt.Sprintf("run %s is interrupted and waiting on %d root actions", runID, len(interruptIDs))
		return status
	case events.RunStatusFailed:
		status.Reason = fmt.Sprintf("run %s failed and cannot be resumed; inspect run detail or start a new client run", runID)
	case events.RunStatusSucceeded:
		status.Reason = fmt.Sprintf("run %s completed and does not need resume", runID)
	case events.RunStatusRunning:
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

type RuntimeWorkbenchService struct {
	cfg   RuntimeWorkbenchConfig
	store runtimeWorkbenchStore
	plans runtimeWorkbenchPlanStore
	trace *TraceService
}

type RuntimeWorkbenchConfig struct {
	Workspace *workspace.Workspace
}

type runtimeWorkbenchStore interface {
	LoadSession(ctx context.Context, sessionID string) (*events.SessionRecord, error)
	LoadLatestRunForSession(ctx context.Context, sessionID string) (*events.RunRecord, error)
	LoadEvents(ctx context.Context, runID string) ([]events.EventRecord, error)
	LoadRunDecision(ctx context.Context, runID string) (*decision.Record, error)
	GetSessionSummary(ctx context.Context, sessionID string) (*runtimehistory.SessionSummary, error)
	ListByRun(ctx context.Context, runID string) ([]store.ToolResultRecord, error)
	ListArtifactsByRun(ctx context.Context, runID string) ([]artifacts.Record, error)
	ListProviderUsagesByRun(ctx context.Context, runID string) ([]providerusage.Record, error)
}

type runtimeWorkbenchPlanStore interface {
	LoadRuntimePlan(ctx context.Context, sessionID string) (*runtime.Plan, error)
}
