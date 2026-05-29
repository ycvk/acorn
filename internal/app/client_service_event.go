package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/clientevents"
)

func (s *ClientService) LoadRunEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]clientevents.RunEvent, error) {
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
	events := make([]clientevents.RunEvent, 0, len(records))
	for _, record := range records {
		event, err := clientevents.ProjectRunEvent(record)
		if err != nil {
			return nil, fmt.Errorf("%w: project persisted run event: %v", ErrClientProjectionFailed, err)
		}
		events = append(events, event)
	}
	return events, nil
}

func (s *ClientService) LoadRunEventsForDetail(ctx context.Context, runID string) (*clientevents.RunEventDetail, error) {
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
	events := make([]clientevents.RunEvent, 0, len(records))
	unsupported := make([]clientevents.UnsupportedRunEvent, 0)
	for _, record := range records {
		event, err := clientevents.ProjectRunEvent(record)
		if err != nil {
			unsupported = append(unsupported, clientevents.ProjectUnsupportedRunEvent(record))
			continue
		}
		events = append(events, event)
	}
	return &clientevents.RunEventDetail{
		Events:      events,
		Unsupported: unsupported,
		Trace:       clientevents.BuildTraceSummary(records),
	}, nil
}

func (s *ClientService) EventPollInterval() time.Duration {
	if s == nil || s.eventPoll <= 0 {
		return 100 * time.Millisecond
	}
	return s.eventPoll
}
