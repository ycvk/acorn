package api

import (
	"context"

	"github.com/ycvk/acorn/internal/core"
)

type inboxServiceStore struct {
	unimplementedStore
	pendingActions     []core.PendingActionRecord
	activeRuns         []core.RunRecord
	recentTerminalRuns []core.RunRecord
	runByID            map[string]core.RunRecord
	sessionByID        map[string]core.SessionRecord
	capabilities       *CapabilitiesService
	err                error
}

func newInboxTestService(store *inboxServiceStore) *InboxService {
	if store == nil {
		store = &inboxServiceStore{}
	}
	if store.capabilities == nil {
		store.capabilities = NewCapabilitiesService(nil, nil, nil, nil)
	}
	return NewInboxService(store, store.capabilities)
}

func inboxRunRecordFromSummary(item RunSummary) core.RunRecord {
	status := core.RunStatusSucceeded
	switch item.Status {
	case "running":
		status = core.RunStatusRunning
	case "interrupted":
		status = core.RunStatusInterrupted
	case "failed":
		status = core.RunStatusFailed
	}
	return core.RunRecord{
		RunID:      item.RunID,
		SessionID:  item.ThreadID,
		Status:     status,
		Input:      item.Preview,
		CreatedAt:  item.CreatedAt,
		FinishedAt: item.UpdatedAt,
	}
}

func sessionRecordFromRunSummary(item RunSummary) core.SessionRecord {
	return core.SessionRecord{
		SessionID: item.ThreadID,
		Title:     item.ThreadTitle,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
}

func (s *inboxServiceStore) ListPendingActions(context.Context, int) ([]core.PendingActionRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]core.PendingActionRecord(nil), s.pendingActions...), nil
}

func (s *inboxServiceStore) ListActiveRuns(context.Context, int) ([]core.RunRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]core.RunRecord(nil), s.activeRuns...), nil
}

func (s *inboxServiceStore) ListRecentTerminalRuns(context.Context, int) ([]core.RunRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]core.RunRecord(nil), s.recentTerminalRuns...), nil
}

func (s *inboxServiceStore) LoadRun(_ context.Context, runID string) (*core.RunRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	if item, ok := s.runByID[runID]; ok {
		copied := item
		return &copied, nil
	}
	return nil, core.ErrRunNotFound
}

func (s *inboxServiceStore) LoadSession(_ context.Context, sessionID string) (*core.SessionRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	if item, ok := s.sessionByID[sessionID]; ok {
		copied := item
		return &copied, nil
	}
	return nil, core.ErrSessionNotFound
}
