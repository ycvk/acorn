package api

import (
	"context"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func (s *clientHandlerStore) ListActiveRuns(context.Context, int) ([]core.RunRecord, error) {
	return nil, errUnexpectedClientStoreCall
}

func (s *clientHandlerStore) ListRecentTerminalRuns(context.Context, int) ([]core.RunRecord, error) {
	return nil, errUnexpectedClientStoreCall
}

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

func (s *clientHandlerStore) WriteArtifact(context.Context, core.ArtifactWriteRequest) (core.ArtifactRecord, error) {
	return core.ArtifactRecord{}, errUnexpectedClientStoreCall
}

func (s *clientHandlerStore) ReadArtifactRange(context.Context, core.ArtifactReadRangeRequest) (core.ArtifactReadRangeResult, error) {
	return core.ArtifactReadRangeResult{}, errUnexpectedClientStoreCall
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

func (s *clientHandlerStore) ListBySession(context.Context, string) ([]core.ArtifactRecord, error) {
	return nil, errUnexpectedClientStoreCall
}

func (s *clientHandlerStore) GetSessionSummary(context.Context, string) (*core.SessionSummary, error) {
	return nil, errUnexpectedClientStoreCall
}

func (s *clientHandlerStore) UpsertSessionSummary(context.Context, core.SessionSummary) error {
	return errUnexpectedClientStoreCall
}

func (s *clientHandlerStore) SaveOAuthToken(context.Context, core.OAuthToken) error {
	return errUnexpectedClientStoreCall
}

func (s *clientHandlerStore) GetOAuthToken(context.Context, string) (*core.OAuthToken, error) {
	return nil, errUnexpectedClientStoreCall
}

func (s *clientHandlerStore) CreatePendingAction(context.Context, core.PendingActionInput) (*core.PendingActionRecord, error) {
	return nil, errUnexpectedClientStoreCall
}

func (s *clientHandlerStore) ListPendingActions(context.Context, int) ([]core.PendingActionRecord, error) {
	return nil, errUnexpectedClientStoreCall
}

func (s *clientHandlerStore) LoadPendingAction(context.Context, string) (*core.PendingActionRecord, error) {
	return nil, errUnexpectedClientStoreCall
}

func (s *clientHandlerStore) DecidePendingAction(context.Context, string, core.PendingActionStatus, string) (*core.PendingActionRecord, error) {
	return nil, errUnexpectedClientStoreCall
}

func (s *clientHandlerStore) SavePairingCode(context.Context, *core.PairingCode) error {
	return errUnexpectedClientStoreCall
}

func (s *clientHandlerStore) ConsumePairingCode(context.Context, string, time.Time) (*core.PairingCode, error) {
	return nil, errUnexpectedClientStoreCall
}

func (s *clientHandlerStore) SaveDevice(context.Context, *core.Device) error {
	return errUnexpectedClientStoreCall
}

func (s *clientHandlerStore) LoadDeviceByTokenHash(context.Context, string) (*core.Device, error) {
	return nil, errUnexpectedClientStoreCall
}

func (s *clientHandlerStore) ListDevices(context.Context) ([]core.Device, error) {
	return nil, errUnexpectedClientStoreCall
}

func (s *clientHandlerStore) TouchDevice(context.Context, string, time.Time) error {
	return errUnexpectedClientStoreCall
}

func (s *clientHandlerStore) RevokeDevice(context.Context, string, time.Time) error {
	return errUnexpectedClientStoreCall
}
