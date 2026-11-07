package api

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

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

// EventService loads run events and artifacts for client-facing detail and
// streaming endpoints. It is the read-side projection of persisted run state.
type EventService struct {
	store     core.SessionStore
	artifacts core.ArtifactService
}

// eventPollInterval is how often a followed run's stream checks for new events.
const eventPollInterval = 100 * time.Millisecond

// NewEventService constructs an EventService backed by the given store.
func NewEventService(store core.SessionStore, artifacts core.ArtifactService) *EventService {
	return &EventService{
		store:     store,
		artifacts: artifacts,
	}
}

func (s *EventService) LoadRunEventsAfter(ctx context.Context, runID string, afterSeq int64) (*RunEventBatch, error) {
	if _, err := s.store.LoadRun(ctx, runID); err != nil {
		return nil, err
	}
	records, err := s.store.LoadEventsAfter(ctx, runID, afterSeq)
	if err != nil {
		return nil, fmt.Errorf("load run %s events: %w", runID, err)
	}
	events, err := projectLiveEvents(records)
	if err != nil {
		return nil, err
	}
	cursorSeq := afterSeq
	for _, record := range records {
		cursorSeq = max(cursorSeq, record.Sequence)
	}
	return &RunEventBatch{
		Events:    events,
		CursorSeq: cursorSeq,
	}, nil
}

func (s *EventService) LoadRunEventsForDetail(ctx context.Context, runID string) (*RunEventDetail, error) {
	if _, err := s.store.LoadRun(ctx, runID); err != nil {
		return nil, err
	}
	records, err := s.store.LoadEvents(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("load run %s events: %w", runID, err)
	}
	events, err := projectLiveEvents(records)
	if err != nil {
		return nil, err
	}
	return &RunEventDetail{
		Events: events,
	}, nil
}

// projectLiveEvents projects the records of the kinds clients are shown,
// leaving out the calls of the plan tool, whose plans they get instead.
func projectLiveEvents(records []core.EventRecord) ([]RunEvent, error) {
	events := make([]RunEvent, 0, len(records))
	for _, record := range records {
		if !IsLiveRunEventKind(record.Kind) || planToolCall(record) {
			continue
		}
		event, err := ProjectRunEvent(record)
		if err != nil {
			return nil, fmt.Errorf("%w: project persisted run event: %v", ErrClientProjectionFailed, err)
		}
		events = append(events, event)
	}
	return events, nil
}

func (s *EventService) ListRunArtifacts(ctx context.Context, runID string) ([]ArtifactSummary, error) {
	if _, err := s.store.LoadRun(ctx, runID); err != nil {
		return nil, err
	}
	records, err := s.artifacts.ListByRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("list run artifacts for %s: %w", runID, err)
	}
	return buildArtifactSummaries(records), nil
}

func buildArtifactSummaries(records []core.ArtifactRecord) []ArtifactSummary {
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
