package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// SessionSummaryService wraps a SessionSummaryStore with validation and
// summary length limiting. Moved from core to keep core as a pure Layer 0
// (types + contracts only, no service structs).
type SessionSummaryService struct {
	store    core.SessionSummaryStore
	maxChars int
}

func NewSessionSummaryService(store core.SessionSummaryStore, maxChars int) *SessionSummaryService {
	if maxChars <= 0 {
		maxChars = 2000
	}
	return &SessionSummaryService{store: store, maxChars: maxChars}
}

func (s *SessionSummaryService) Get(ctx context.Context, sessionID string) (*core.SessionSummary, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("session summary store is nil")
	}
	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" {
		return nil, fmt.Errorf("session id is required")
	}
	return s.store.GetSessionSummary(ctx, trimmed)
}

func (s *SessionSummaryService) Update(ctx context.Context, sessionID, sourceRunID, runStatus, summary string) (*core.SessionSummary, error) {
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
	record := core.SessionSummary{
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
