package domain

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type SessionSummaryStore interface {
	GetSessionSummary(ctx context.Context, sessionID string) (*SessionSummary, error)
	UpsertSessionSummary(ctx context.Context, summary SessionSummary) error
}

type SessionSummaryService struct {
	store    SessionSummaryStore
	maxChars int
}

func NewSessionSummaryService(store SessionSummaryStore, maxChars int) *SessionSummaryService {
	if maxChars <= 0 {
		maxChars = 2000
	}
	return &SessionSummaryService{store: store, maxChars: maxChars}
}

func (s *SessionSummaryService) Get(ctx context.Context, sessionID string) (*SessionSummary, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("session summary store is nil")
	}
	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" {
		return nil, fmt.Errorf("session id is required")
	}
	return s.store.GetSessionSummary(ctx, trimmed)
}

func (s *SessionSummaryService) Update(ctx context.Context, sessionID, sourceRunID, runStatus, summary string) (*SessionSummary, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("session summary store is nil")
	}
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedSessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	trimmedSummary := strings.TrimSpace(summary)
	if trimmedSummary == "" {
		return nil, fmt.Errorf("summary is required")
	}
	if len(trimmedSummary) > s.maxChars {
		trimmedSummary = trimmedSummary[:s.maxChars]
	}
	record := SessionSummary{
		SessionID:   trimmedSessionID,
		SourceRunID: strings.TrimSpace(sourceRunID),
		RunStatus:   strings.TrimSpace(runStatus),
		Summary:     trimmedSummary,
		UpdatedAt:   time.Now().UTC(),
	}
	if err := s.store.UpsertSessionSummary(ctx, record); err != nil {
		return nil, err
	}
	return &record, nil
}

func FormatSessionSummaryForPrompt(summary *SessionSummary) string {
	if summary == nil || strings.TrimSpace(summary.Summary) == "" {
		return ""
	}
	lines := []string{
		"<session-summary>",
		"Previous session continuity summary. Treat this as durable recall context, not new user input.",
		"",
		"Latest session state:",
		strings.TrimSpace(summary.Summary),
	}
	if status := strings.TrimSpace(summary.RunStatus); status != "" {
		lines = append(lines, "", "Last run status: "+status)
	}
	if sourceRunID := strings.TrimSpace(summary.SourceRunID); sourceRunID != "" {
		lines = append(lines, "Source run: "+sourceRunID)
	}
	lines = append(lines, "</session-summary>")
	return strings.Join(lines, "\n")
}
