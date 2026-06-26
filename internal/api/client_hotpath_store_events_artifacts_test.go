package api

import (
	"context"

	"github.com/ycvk/acorn/internal/core"
)

func (s *clientHandlerStore) AppendEvent(_ context.Context, runID, kind string, payload any) (core.EventRecord, error) {
	_, err := s.stubOrErr()
	if err != nil {
		return core.EventRecord{}, err
	}
	return core.EventRecord{
		Sequence: int64(len(s.stub.events) + 1),
		RunID:    runID,
		Kind:     kind,
		Payload:  payload,
	}, nil
}

func (s *clientHandlerStore) LoadEvents(_ context.Context, _ string) ([]core.EventRecord, error) {
	stub, err := s.stubOrErr()
	if err != nil {
		return nil, err
	}
	out := make([]core.EventRecord, 0, len(stub.events))
	for _, item := range stub.events {
		out = append(out, eventRecordFromRunEvent(item))
	}
	return out, nil
}

func (s *clientHandlerStore) LoadEventsAfter(_ context.Context, _ string, afterSeq int64) ([]core.EventRecord, error) {
	stub, err := s.stubOrErr()
	if err != nil {
		return nil, err
	}
	stub.lastAfterSeq = afterSeq
	stub.loadEventCalls++
	if len(stub.eventBatches) > 0 {
		batch := stub.eventBatches[0]
		stub.eventBatches = stub.eventBatches[1:]
		if batch == nil {
			return nil, nil
		}
		out := make([]core.EventRecord, 0, len(batch.Events))
		for _, item := range batch.Events {
			out = append(out, eventRecordFromRunEvent(item))
		}
		return out, nil
	}
	out := make([]core.EventRecord, 0, len(stub.events))
	for _, item := range stub.events {
		if item.Seq > afterSeq {
			out = append(out, eventRecordFromRunEvent(item))
		}
	}
	return out, nil
}

func (s *clientHandlerStore) ListByRun(_ context.Context, _ string) ([]core.ArtifactRecord, error) {
	stub, err := s.stubOrErr()
	if err != nil {
		return nil, err
	}
	out := make([]core.ArtifactRecord, 0, len(stub.artifacts))
	for _, item := range stub.artifacts {
		out = append(out, artifactRecordFromSummary(item))
	}
	return out, nil
}
