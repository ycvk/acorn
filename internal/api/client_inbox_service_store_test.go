package api

import (
	"context"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

type inboxServiceStore struct {
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

func (s *inboxServiceStore) unsupportedStoreCall() error {
	if s.err != nil {
		return s.err
	}
	return errUnexpectedClientStoreCall
}

func (s *inboxServiceStore) CreateSession(context.Context, string, string) (*core.SessionRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) ListSessions(context.Context, int) ([]core.SessionRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) LoadLatestRunForSession(context.Context, string) (*core.RunRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) LoadLatestRunsForSessions(context.Context, []string) (map[string]*core.RunRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) UpdateSessionTitle(context.Context, string, string) error {
	return s.unsupportedStoreCall()
}
func (s *inboxServiceStore) UpdateSessionTitleIfEmpty(context.Context, string, string) error {
	return s.unsupportedStoreCall()
}
func (s *inboxServiceStore) DeleteSession(context.Context, string) error {
	return s.unsupportedStoreCall()
}
func (s *inboxServiceStore) ListSessionMessages(context.Context, string, int) ([]core.SessionMessageRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) NextSessionMessageTurnIndex(context.Context, string) (int, error) {
	return 0, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) AppendSessionMessage(context.Context, string, int, string, string, string) (*core.SessionMessageRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) CreateFreshSessionTurn(context.Context, string, string, string) (int, error) {
	return 0, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) LoadLatestUnboundUserMessage(context.Context, string) (*core.SessionMessageRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) BindUserMessageRunIDByID(context.Context, int64, string) error {
	return s.unsupportedStoreCall()
}
func (s *inboxServiceStore) BindLatestUserMessageRunID(context.Context, string, int, string) error {
	return s.unsupportedStoreCall()
}
func (s *inboxServiceStore) SyncAssistantMessageForRun(context.Context, string) error {
	return s.unsupportedStoreCall()
}
func (s *inboxServiceStore) SyncAssistantMessageForRunStatus(context.Context, string, core.RunStatus) error {
	return s.unsupportedStoreCall()
}
func (s *inboxServiceStore) CreateRun(context.Context, core.RunCreateParams) error {
	return s.unsupportedStoreCall()
}
func (s *inboxServiceStore) FinishRun(context.Context, string, core.RunStatus, string, string) error {
	return s.unsupportedStoreCall()
}
func (s *inboxServiceStore) MarkInterrupted(context.Context, string, string) error {
	return s.unsupportedStoreCall()
}
func (s *inboxServiceStore) UpdateRunOutput(context.Context, string, string) error {
	return s.unsupportedStoreCall()
}
func (s *inboxServiceStore) AppendEvent(context.Context, string, string, any) (core.EventRecord, error) {
	return core.EventRecord{}, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) LoadEvents(context.Context, string) ([]core.EventRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) LoadEventsAfter(context.Context, string, int64) ([]core.EventRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) CreatePendingAction(context.Context, core.PendingActionInput) (*core.PendingActionRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) LoadPendingAction(context.Context, string) (*core.PendingActionRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) DecidePendingAction(context.Context, string, core.PendingActionStatus, string) (*core.PendingActionRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) SavePairingCode(context.Context, *core.PairingCode) error {
	return s.unsupportedStoreCall()
}
func (s *inboxServiceStore) ConsumePairingCode(context.Context, string, time.Time) (*core.PairingCode, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) SaveDevice(context.Context, *core.Device) error {
	return s.unsupportedStoreCall()
}
func (s *inboxServiceStore) LoadDeviceByTokenHash(context.Context, string) (*core.Device, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) ListDevices(context.Context) ([]core.Device, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) TouchDevice(context.Context, string, time.Time) error {
	return s.unsupportedStoreCall()
}
func (s *inboxServiceStore) RevokeDevice(context.Context, string, time.Time) error {
	return s.unsupportedStoreCall()
}
func (s *inboxServiceStore) WriteArtifact(context.Context, core.ArtifactWriteRequest) (core.ArtifactRecord, error) {
	return core.ArtifactRecord{}, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) ReadArtifactRange(context.Context, core.ArtifactReadRangeRequest) (core.ArtifactReadRangeResult, error) {
	return core.ArtifactReadRangeResult{}, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) ListByRun(context.Context, string) ([]core.ArtifactRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) ListBySession(context.Context, string) ([]core.ArtifactRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) GetSessionSummary(context.Context, string) (*core.SessionSummary, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *inboxServiceStore) UpsertSessionSummary(context.Context, core.SessionSummary) error {
	return s.unsupportedStoreCall()
}
func (s *inboxServiceStore) SaveOAuthToken(context.Context, core.OAuthToken) error {
	return s.unsupportedStoreCall()
}
func (s *inboxServiceStore) GetOAuthToken(context.Context, string) (*core.OAuthToken, error) {
	return nil, s.unsupportedStoreCall()
}
