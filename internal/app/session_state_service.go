package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/runtime"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
)

type SessionStateService struct {
	cfg   *config.Config
	store sessionStateStore
	trace *TraceService
}

type SessionDetail struct {
	Session             events.SessionRecord    `json:"session"`
	LatestRunID         string                  `json:"latest_run_id,omitempty"`
	LatestRunStatus     events.RunStatus        `json:"latest_run_status,omitempty"`
	State               runtimeapi.SessionState `json:"state,omitempty"`
	Resumable           bool                    `json:"resumable"`
	ResumeReason        string                  `json:"resume_reason,omitempty"`
	MemoryContextBudget int                     `json:"memory_context_budget,omitempty"`
	TraceSummary        *runtime.TraceSummary   `json:"trace_summary,omitempty"`
	SelectedSkill       *runtime.SelectedSkill  `json:"selected_skill,omitempty"`
	LatestDecision      *decision.Record        `json:"latest_decision,omitempty"`
	InterruptIDs        []string                `json:"interrupt_ids,omitempty"`
	SessionSummary      *model.SessionSummary   `json:"session_summary,omitempty"`
}

func NewSessionStateService(cfg *config.Config, store sessionStateStore, trace *TraceService) *SessionStateService {
	return &SessionStateService{
		cfg:   cfg,
		store: store,
		trace: trace,
	}
}

func (s *SessionStateService) CreateSession(ctx context.Context, sessionID, title string) (*events.SessionRecord, error) {
	return s.store.CreateSession(ctx, sessionID, title)
}

func (s *SessionStateService) ListSessionMessages(ctx context.Context, sessionID string, limit int) ([]events.SessionMessageRecord, error) {
	return s.store.ListSessionMessages(ctx, sessionID, limit)
}

func (s *SessionStateService) LoadSession(ctx context.Context, sessionID string) (*SessionDetail, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("session state store is nil")
	}

	session, err := s.store.LoadSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	latestRun, err := s.store.LoadLatestRunForSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	detail, err := buildSessionDetail(*session, latestRun, ctx, s.store, s.trace)
	if err != nil {
		return nil, err
	}
	detail.MemoryContextBudget = s.cfg.Memory.Search.MemoryContextTokenBudget
	summary, summaryErr := s.store.GetSessionSummary(ctx, sessionID)
	if summaryErr != nil {
		return nil, fmt.Errorf("load session summary for %s: %w", sessionID, summaryErr)
	}
	detail.SessionSummary = summary
	return &detail, nil
}

func buildSessionDetail(session events.SessionRecord, latestRun *events.RunRecord, ctx context.Context, store sessionStateStore, traceSvc *TraceService) (SessionDetail, error) {
	latestRunProjection, err := projectLatestRun(ctx, store, traceSvc, latestRun)
	if err != nil {
		return SessionDetail{}, err
	}
	detail := SessionDetail{
		Session:         session,
		LatestRunID:     latestRunProjection.LatestRunID,
		LatestRunStatus: latestRunProjection.LatestRunStatus,
		State:           latestRunProjection.State,
		Resumable:       latestRunProjection.Resumable,
		ResumeReason:    latestRunProjection.ResumeReason,
		TraceSummary:    latestRunProjection.TraceSummary,
		SelectedSkill:   latestRunProjection.SelectedSkill,
		LatestDecision:  latestRunProjection.LatestDecision,
		InterruptIDs:    latestRunProjection.InterruptIDs,
	}
	return detail, nil
}

func defaultResumeReason(run *events.RunRecord) string {
	if run == nil {
		return ""
	}
	switch run.Status {
	case events.RunStatusSucceeded:
		return fmt.Sprintf("run %s completed and does not need resume", run.RunID)
	case events.RunStatusFailed:
		return fmt.Sprintf("run %s failed; inspect run detail or start a new client run", run.RunID)
	case events.RunStatusRunning:
		return fmt.Sprintf("run %s is still running", run.RunID)
	case events.RunStatusInterrupted:
		return fmt.Sprintf("run %s is interrupted", run.RunID)
	default:
		return ""
	}
}
