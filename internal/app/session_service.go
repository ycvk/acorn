package app

import (
	"context"
	"time"

	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/runtimehistory"
)

type SessionService struct {
	store sessionStore
}

func NewSessionService(store sessionStore) *SessionService {
	return &SessionService{store: store}
}

func (s *SessionService) CreateSession(ctx context.Context, sessionID, title string) (*events.SessionRecord, error) {
	return s.store.CreateSession(ctx, sessionID, title)
}

func (s *SessionService) ListSessionMessages(ctx context.Context, sessionID string, limit int) ([]events.SessionMessageRecord, error) {
	return s.store.ListSessionMessages(ctx, sessionID, limit)
}

type SessionListItem struct {
	Session         events.SessionRecord `json:"session"`
	LatestRunID     string               `json:"latest_run_id,omitempty"`
	LatestRunStatus events.RunStatus     `json:"latest_run_status,omitempty"`
	State           runtime.SessionState `json:"state,omitempty"`
	Resumable       bool                 `json:"resumable"`
	SummarySnippet  string               `json:"summary_snippet,omitempty"`
	SummaryStatus   string               `json:"summary_status,omitempty"`
	SummaryUpdated  *time.Time           `json:"summary_updated_at,omitempty"`
}

type SessionDetail struct {
	Session             events.SessionRecord           `json:"session"`
	LatestRunID         string                         `json:"latest_run_id,omitempty"`
	LatestRunStatus     events.RunStatus               `json:"latest_run_status,omitempty"`
	State               runtime.SessionState           `json:"state,omitempty"`
	Resumable           bool                           `json:"resumable"`
	ResumeReason        string                         `json:"resume_reason,omitempty"`
	MemoryContextBudget int                            `json:"memory_context_budget,omitempty"`
	TraceSummary        *runtime.TraceSummary          `json:"trace_summary,omitempty"`
	SelectedSkill       *runtime.SelectedSkill         `json:"selected_skill,omitempty"`
	LatestDecision      *decision.Record               `json:"latest_decision,omitempty"`
	InterruptIDs        []string                       `json:"interrupt_ids,omitempty"`
	SessionSummary      *runtimehistory.SessionSummary `json:"session_summary,omitempty"`
}
