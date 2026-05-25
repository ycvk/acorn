package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/web/runprojector"
)

func (s *ClientService) LoadRunEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]runprojector.RunEvent, error) {
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
	events := make([]runprojector.RunEvent, 0, len(records))
	for _, record := range records {
		event, err := runprojector.ProjectRunEvent(record)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func (s *ClientService) LoadRunEventsForDetail(ctx context.Context, runID string) (*runprojector.RunEventDetail, error) {
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
	events := make([]runprojector.RunEvent, 0, len(records))
	unsupported := make([]runprojector.UnsupportedRunEvent, 0)
	for _, record := range records {
		event, err := runprojector.ProjectRunEvent(record)
		if err != nil {
			unsupported = append(unsupported, runprojector.ProjectUnsupportedRunEvent(record))
			continue
		}
		events = append(events, event)
	}
	return &runprojector.RunEventDetail{
		Events:      events,
		Unsupported: unsupported,
		Trace:       runtime.BuildTraceSummary(records),
	}, nil
}

func (s *ClientService) EventPollInterval() time.Duration {
	if s == nil || s.eventPoll <= 0 {
		return 100 * time.Millisecond
	}
	return s.eventPoll
}
