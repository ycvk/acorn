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
