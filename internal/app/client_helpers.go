package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/store"
)

var (
	ErrClientProjectionFailed = errors.New("client projection failed")
	ErrClientNoPendingMessage = errors.New("client pending user message not found")
)

// projectionError wraps a format string into ErrClientProjectionFailed.
func projectionError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrClientProjectionFailed, fmt.Sprintf(format, args...))
}

func projectRunStatus(status domain.RunStatus) (string, error) {
	switch status {
	case domain.RunStatusRunning:
		return "running", nil
	case domain.RunStatusSucceeded:
		return "completed", nil
	case domain.RunStatusInterrupted:
		return "interrupted", nil
	case domain.RunStatusFailed:
		return "failed", nil
	default:
		return "", projectionError("unknown run status %q", status)
	}
}

// Run is a user-facing run DTO.
type Run struct {
	ID          string
	ThreadID    string
	SkillID     string
	Status      string
	Mode        string
	CreatedAt   time.Time
	CompletedAt time.Time
}

// ArtifactSummary represents a stored run artifact exposed through client detail.
type ArtifactSummary struct {
	ArtifactID          string
	RunID               string
	SessionID           string
	SourceToolResultRef string
	Kind                string
	Title               string
	MIMEType            string
	SizeBytes           int64
	SHA256              string
	CreatedAt           time.Time
}

func (s *ClientService) ListRunArtifacts(ctx context.Context, runID string) ([]ArtifactSummary, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("client store is nil")
	}
	if _, err := s.store.LoadRun(ctx, runID); err != nil {
		return nil, err
	}
	records, err := s.store.ListArtifactsByRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("list run artifacts for %s: %w", runID, err)
	}
	return buildArtifactSummaries(records), nil
}

func buildArtifactSummaries(records []store.ArtifactRecord) []ArtifactSummary {
	if len(records) == 0 {
		return nil
	}
	items := make([]ArtifactSummary, 0, len(records))
	for _, record := range records {
		items = append(items, ArtifactSummary{
			ArtifactID:          record.ArtifactID,
			RunID:               record.RunID,
			SessionID:           record.SessionID,
			SourceToolResultRef: record.SourceToolResultRef,
			Kind:                string(record.Kind),
			Title:               record.Title,
			MIMEType:            record.MIMEType,
			SizeBytes:           record.SizeBytes,
			SHA256:              record.SHA256,
			CreatedAt:           record.CreatedAt,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ArtifactID < items[j].ArtifactID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items
}
