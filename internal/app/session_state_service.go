package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/runtime"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
)

type SessionStateService struct {
	cfg   *config.Config
	store sessionStateStore
	trace *TraceService
}

func NewSessionStateService(cfg *config.Config, store sessionStateStore, trace *TraceService) *SessionStateService {
	return &SessionStateService{
		cfg:   cfg,
		store: store,
		trace: trace,
	}
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
	detail := SessionDetail{
		Session: session,
	}
	if latestRun != nil {
		detail.LatestRunID = latestRun.RunID
		detail.LatestRunStatus = latestRun.Status
	}

	detail.State = runtimeapi.DeriveSessionState(latestRun, false)

	if latestRun == nil {
		return detail, nil
	}

	if latestRun.Status == events.RunStatusInterrupted {
		if traceSvc == nil {
			return SessionDetail{}, fmt.Errorf("load resume status for run %s: trace service is nil", latestRun.RunID)
		}
		resumeStatus, err := traceSvc.ResumeStatus(ctx, latestRun.RunID)
		if err != nil {
			return SessionDetail{}, fmt.Errorf("load resume status for run %s: %w", latestRun.RunID, err)
		} else if resumeStatus == nil {
			return SessionDetail{}, fmt.Errorf("load resume status for run %s: resume status is nil", latestRun.RunID)
		}
		detail.Resumable = resumeStatus.Resumable
		detail.ResumeReason = resumeStatus.Reason
		detail.InterruptIDs = resumeStatus.InterruptIDs
	}
	if detail.ResumeReason == "" {
		detail.ResumeReason = defaultResumeReason(latestRun)
	}

	if store == nil {
		return SessionDetail{}, fmt.Errorf("load events for run %s: session state store is nil", latestRun.RunID)
	}
	raw, loadErr := store.LoadEvents(ctx, latestRun.RunID)
	if loadErr == nil && len(raw) > 0 {
		detail.TraceSummary = runtime.BuildTraceSummary(raw)
		detail.SelectedSkill = runtime.SelectedSkillFromEvents(raw)
	} else if loadErr != nil {
		return SessionDetail{}, fmt.Errorf("load events for run %s: %w", latestRun.RunID, loadErr)
	}
	if decisionRecord, decisionErr := store.LoadRunDecision(ctx, latestRun.RunID); decisionErr == nil {
		detail.LatestDecision = decisionRecord
	} else {
		return SessionDetail{}, fmt.Errorf("load decision for run %s: %w", latestRun.RunID, decisionErr)
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
