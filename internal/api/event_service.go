package api

import (
	"context"
	"errors"
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
	store     eventStore
	eventPoll time.Duration
}

// NewEventService constructs an EventService backed by the given store.
func NewEventService(store eventStore) *EventService {
	return &EventService{
		store:     store,
		eventPoll: 100 * time.Millisecond,
	}
}

func (s *EventService) LoadRunEventsAfter(ctx context.Context, runID string, afterSeq int64) (*core.RunEventBatch, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("client store is nil")
	}
	if _, err := s.store.LoadRun(ctx, runID); err != nil {
		return nil, err
	}
	records, err := s.store.LoadEventsAfter(ctx, runID, afterSeq)
	if err != nil {
		return nil, fmt.Errorf("%w: load persisted run events: %v", ErrClientProjectionFailed, err)
	}
	events := make([]core.RunEvent, 0, len(records))
	cursorSeq := afterSeq
	for _, record := range records {
		if record.Sequence > cursorSeq {
			cursorSeq = record.Sequence
		}
		if !IsLiveRunEventKind(record.Kind) {
			continue
		}
		event, err := ProjectRunEvent(record)
		if err != nil {
			return nil, fmt.Errorf("%w: project persisted run event: %v", ErrClientProjectionFailed, err)
		}
		events = append(events, event)
	}
	return &core.RunEventBatch{
		Events:    events,
		CursorSeq: cursorSeq,
	}, nil
}

func (s *EventService) LoadRunEventsForDetail(ctx context.Context, runID string) (*core.RunEventDetail, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("client store is nil")
	}
	if _, err := s.store.LoadRun(ctx, runID); err != nil {
		return nil, err
	}
	records, err := s.store.LoadEvents(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("%w: load persisted run events: %v", ErrClientProjectionFailed, err)
	}
	events := make([]core.RunEvent, 0, len(records))
	for _, record := range records {
		if !IsLiveRunEventKind(record.Kind) {
			continue
		}
		event, err := ProjectRunEvent(record)
		if err != nil {
			return nil, fmt.Errorf("%w: project persisted run event: %v", ErrClientProjectionFailed, err)
		}
		events = append(events, event)
	}
	return &core.RunEventDetail{
		Events: events,
	}, nil
}

func (s *EventService) EventPollInterval() time.Duration {
	if s == nil || s.eventPoll <= 0 {
		return 100 * time.Millisecond
	}
	return s.eventPoll
}

func (s *EventService) ListRunArtifacts(ctx context.Context, runID string) ([]ArtifactSummary, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("client store is nil")
	}
	if _, err := s.store.LoadRun(ctx, runID); err != nil {
		return nil, err
	}
	records, err := s.store.ListByRun(ctx, runID)
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
