package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/runtime"
)

type RunResumeService struct {
	store     runResumeStore
	resumeRun func(context.Context, string, map[string]any, core.StreamSink) (*runtime.Result, error)
}

type ResumeStatus struct {
	RunID        string         `json:"run_id,omitempty"`
	Status       core.RunStatus `json:"status,omitempty"`
	Resumable    bool           `json:"resumable"`
	InterruptIDs []string       `json:"interrupt_ids,omitempty"`
	Reason       string         `json:"reason,omitempty"`
}

type RunResult struct {
	RunID       string         `json:"run_id"`
	Status      string         `json:"status"`
	Output      string         `json:"output,omitempty"`
	Error       string         `json:"error,omitempty"`
	Interrupted map[string]any `json:"interrupted,omitempty"`
}

func NewRunResumeService(store runResumeStore) *RunResumeService {
	return &RunResumeService{store: store}
}

func (s *RunResumeService) WithResume(resumeRun func(context.Context, string, map[string]any, core.StreamSink) (*runtime.Result, error)) *RunResumeService {
	s.resumeRun = resumeRun
	return s
}

func (s *RunResumeService) Resume(ctx context.Context, runID string) (*RunResult, error) {
	if s == nil || s.resumeRun == nil {
		return nil, errors.New("resume runner is nil")
	}
	targets, err := s.InferResumeTargets(ctx, runID)
	if err != nil {
		return nil, err
	}
	result, err := s.resumeRun(ctx, runID, targets, nil)
	if err != nil {
		return nil, err
	}
	return runResultFromRuntime(result)
}

func runResultFromRuntime(result *runtime.Result) (*RunResult, error) {
	if result == nil {
		return nil, nil
	}
	status, err := projectRunStatus(result.Status)
	if err != nil {
		return nil, err
	}
	return &RunResult{
		RunID:       result.RunID,
		Status:      status,
		Output:      result.Output,
		Error:       result.Error,
		Interrupted: cloneMap(result.Interrupted),
	}, nil
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	out := make(map[string]any, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}

func (s *RunResumeService) ResumeStatus(ctx context.Context, runID string) (*ResumeStatus, error) {
	run, items, err := s.loadRunEvents(ctx, runID)
	if err != nil {
		return nil, err
	}
	status := buildResumeStatus(runID, run, items)
	if status == nil || run == nil || run.Status != core.RunStatusInterrupted {
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

func (s *RunResumeService) inferResumeTargetsOrReason(ctx context.Context, runID string) (map[string]any, string) {
	targets, err := s.InferResumeTargets(ctx, runID)
	if err != nil {
		return nil, err.Error()
	}
	return targets, ""
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
		return nil, fmt.Errorf("%w: %v", core.ErrRunNotInterrupted, err)
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

func (s *RunResumeService) resumeTargetsForContext(ctx context.Context, runID string, interrupt resumeInterruptContext) (map[string]any, error) {
	switch kind := interruptInfoKind(interrupt.Info); kind {
	case "", "run_command_pause":
		return defaultTargets(interrupt.ID), nil
	case "operator_question":
		return s.operatorQuestionTargets(ctx, runID, interrupt)
	default:
		return nil, fmt.Errorf("run %s interrupt %s has unsupported kind %q", runID, interrupt.ID, kind)
	}
}

func (s *RunResumeService) operatorQuestionTargets(ctx context.Context, runID string, interrupt resumeInterruptContext) (map[string]any, error) {
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
	if record.Kind != core.PendingActionKindOperatorQuestion {
		return nil, fmt.Errorf("run %s interrupt %s action %s has kind %q", runID, interrupt.ID, actionID, record.Kind)
	}
	if record.Status == core.PendingActionStatusPending {
		return nil, fmt.Errorf("run %s interrupt %s operator_question action %s is still pending", runID, interrupt.ID, actionID)
	}
	if record.Status != core.PendingActionStatusApproved && record.Status != core.PendingActionStatusRejected {
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

func buildResumeStatus(runID string, run *core.RunRecord, items []core.EventRecord) *ResumeStatus {
	status := &ResumeStatus{RunID: runID}
	if run == nil {
		status.Reason = fmt.Sprintf("run %s is unavailable", runID)
		return status
	}
	status.Status = run.Status

	switch run.Status {
	case core.RunStatusInterrupted:
		interruptIDs, err := latestRootInterruptIDs(items)
		if err != nil {
			status.Reason = fmt.Sprintf("run %s is interrupted but missing resumable interrupt data: %v", runID, err)
			return status
		}
		status.Resumable = true
		status.InterruptIDs = append(status.InterruptIDs, interruptIDs...)
		status.Reason = fmt.Sprintf("run %s is interrupted and waiting on %d root actions", runID, len(interruptIDs))
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
	for i := len(raw) - 1; i >= 0; i-- {
		record := raw[i]
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
			ctx.Info = compactResumeInterruptInfo(ctx.Info)
			contexts = append(contexts, ctx)
		}
		if len(contexts) == 0 {
			return nil, errors.New("run.interrupted has no root interrupt contexts")
		}
		return contexts, nil
	}
	return nil, errors.New("run has no interrupt event to resume")
}

func latestRootInterruptIDs(raw []core.EventRecord) ([]string, error) {
	contexts, err := latestRootInterruptContexts(raw)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(contexts))
	for _, ctx := range contexts {
		ids = append(ids, strings.TrimSpace(ctx.ID))
	}
	return ids, nil
}

func compactResumeInterruptInfo(value any) any {
	data, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]any)
	for _, key := range []string{"kind", "message", "question", "action_id", "command", "command_name", "command_args", "cwd", "url", "tool_name", "interrupt_id", "arguments_json", "reason", "rule"} {
		if current, exists := data[key]; exists {
			out[key] = current
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
