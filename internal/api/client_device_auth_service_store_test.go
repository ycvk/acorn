package api

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

type deviceAuthServiceStore struct {
	stub         *deviceAuthHandlerStub
	pairedDevice *core.Device
}

func newDeviceAuthTestService(stub *deviceAuthHandlerStub) *DeviceAuthService {
	if stub == nil {
		stub = &deviceAuthHandlerStub{}
	}
	return &DeviceAuthService{
		store: &deviceAuthServiceStore{stub: stub},
		now:   func() time.Time { return time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC) },
		rand:  bytes.NewReader(bytes.Repeat([]byte{0x11}, 128)),
	}
}

func (s *deviceAuthServiceStore) stubOrErr() (*deviceAuthHandlerStub, error) {
	if s == nil || s.stub == nil {
		return nil, errUnexpectedClientStoreCall
	}
	return s.stub, nil
}

func (s *deviceAuthServiceStore) SavePairingCode(context.Context, *core.PairingCode) error {
	return nil
}

func (s *deviceAuthServiceStore) ConsumePairingCode(_ context.Context, _ string, _ time.Time) (*core.PairingCode, error) {
	stub, err := s.stubOrErr()
	if err != nil {
		return nil, err
	}
	if stub.pairErr != nil {
		return nil, stub.pairErr
	}
	return &core.PairingCode{}, nil
}

func (s *deviceAuthServiceStore) SaveDevice(_ context.Context, device *core.Device) error {
	if s == nil || device == nil {
		return errUnexpectedClientStoreCall
	}
	deviceCopy := *device
	s.pairedDevice = &deviceCopy
	return nil
}

func (s *deviceAuthServiceStore) LoadDeviceByTokenHash(_ context.Context, _ string) (*core.Device, error) {
	stub, err := s.stubOrErr()
	if err != nil {
		return nil, err
	}
	if stub.authErr != nil {
		if errors.Is(stub.authErr, ErrDeviceRevoked) {
			now := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
			return &core.Device{DeviceID: "device_test", Name: "Test device", Platform: "test", RevokedAt: &now}, nil
		}
		return nil, core.ErrDeviceNotFound
	}
	if s.pairedDevice != nil {
		deviceCopy := *s.pairedDevice
		return &deviceCopy, nil
	}
	return &core.Device{
		DeviceID:   "device_test",
		Name:       "Test device",
		Platform:   "test",
		CreatedAt:  time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
		LastSeenAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
	}, nil
}

func (s *deviceAuthServiceStore) ListDevices(context.Context) ([]core.Device, error) {
	stub, err := s.stubOrErr()
	if err != nil {
		return nil, err
	}
	if stub.listErr != nil {
		return nil, stub.listErr
	}
	items := make([]core.Device, 0, len(stub.devices))
	for _, item := range stub.devices {
		items = append(items, core.Device{
			DeviceID:   item.DeviceID,
			Name:       item.Name,
			Platform:   item.Platform,
			CreatedAt:  item.CreatedAt,
			LastSeenAt: item.LastSeenAt,
			RevokedAt:  item.RevokedAt,
		})
	}
	if s.pairedDevice != nil {
		items = append(items, *s.pairedDevice)
	}
	return items, nil
}

func (s *deviceAuthServiceStore) TouchDevice(context.Context, string, time.Time) error {
	return nil
}

func (s *deviceAuthServiceStore) RevokeDevice(_ context.Context, deviceID string, _ time.Time) error {
	stub, err := s.stubOrErr()
	if err != nil {
		return err
	}
	if stub.revokeErr != nil {
		return stub.revokeErr
	}
	stub.revokedDeviceID = deviceID
	return nil
}

func (s *deviceAuthServiceStore) unsupportedStoreCall() error {
	return errUnexpectedClientStoreCall
}

func (s *deviceAuthServiceStore) CreateSession(context.Context, string, string) (*core.SessionRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) LoadSession(context.Context, string) (*core.SessionRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) ListSessions(context.Context, int) ([]core.SessionRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) LoadLatestRunForSession(context.Context, string) (*core.RunRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) LoadLatestRunsForSessions(context.Context, []string) (map[string]*core.RunRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) UpdateSessionTitle(context.Context, string, string) error {
	return s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) UpdateSessionTitleIfEmpty(context.Context, string, string) error {
	return s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) DeleteSession(context.Context, string) error {
	return s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) ListSessionMessages(context.Context, string, int) ([]core.SessionMessageRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) NextSessionMessageTurnIndex(context.Context, string) (int, error) {
	return 0, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) AppendSessionMessage(context.Context, string, int, string, string, string) (*core.SessionMessageRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) CreateFreshSessionTurn(context.Context, string, string, string) (int, error) {
	return 0, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) LoadLatestUnboundUserMessage(context.Context, string) (*core.SessionMessageRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) BindUserMessageRunIDByID(context.Context, int64, string) error {
	return s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) BindLatestUserMessageRunID(context.Context, string, int, string) error {
	return s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) SyncAssistantMessageForRun(context.Context, string) error {
	return s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) SyncAssistantMessageForRunStatus(context.Context, string, core.RunStatus) error {
	return s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) CreateRun(context.Context, core.RunCreateParams) error {
	return s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) LoadRun(context.Context, string) (*core.RunRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) FinishRun(context.Context, string, core.RunStatus, string, string) error {
	return s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) MarkInterrupted(context.Context, string, string) error {
	return s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) UpdateRunOutput(context.Context, string, string) error {
	return s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) ListActiveRuns(context.Context, int) ([]core.RunRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) ListRecentTerminalRuns(context.Context, int) ([]core.RunRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) AppendEvent(context.Context, string, string, any) (core.EventRecord, error) {
	return core.EventRecord{}, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) LoadEvents(context.Context, string) ([]core.EventRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) LoadEventsAfter(context.Context, string, int64) ([]core.EventRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) CreatePendingAction(context.Context, core.PendingActionInput) (*core.PendingActionRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) ListPendingActions(context.Context, int) ([]core.PendingActionRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) LoadPendingAction(context.Context, string) (*core.PendingActionRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) DecidePendingAction(context.Context, string, core.PendingActionStatus, string) (*core.PendingActionRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) WriteArtifact(context.Context, core.ArtifactWriteRequest) (core.ArtifactRecord, error) {
	return core.ArtifactRecord{}, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) ReadArtifactRange(context.Context, core.ArtifactReadRangeRequest) (core.ArtifactReadRangeResult, error) {
	return core.ArtifactReadRangeResult{}, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) ListByRun(context.Context, string) ([]core.ArtifactRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) ListBySession(context.Context, string) ([]core.ArtifactRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) GetSessionSummary(context.Context, string) (*core.SessionSummary, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) UpsertSessionSummary(context.Context, core.SessionSummary) error {
	return s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) SaveOAuthToken(context.Context, core.OAuthToken) error {
	return s.unsupportedStoreCall()
}
func (s *deviceAuthServiceStore) GetOAuthToken(context.Context, string) (*core.OAuthToken, error) {
	return nil, s.unsupportedStoreCall()
}
