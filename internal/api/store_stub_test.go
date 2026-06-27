package api

import (
	"context"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// unimplementedStore is a test base that satisfies core.SessionStore,
// core.IdentityStore, and core.ArtifactService by returning
// errUnexpectedClientStoreCall from every method. Test stubs embed it and
// override only the methods they care about, eliminating ~120 lines of
// boilerplate unsupportedStoreCall fillers across four stubs.
type unimplementedStore struct{}

func (unimplementedStore) CreateSession(context.Context, string, string) (*core.SessionRecord, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) LoadSession(context.Context, string) (*core.SessionRecord, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) ListSessions(context.Context, int) ([]core.SessionRecord, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) LoadLatestRunForSession(context.Context, string) (*core.RunRecord, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) LoadLatestRunsForSessions(context.Context, []string) (map[string]*core.RunRecord, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) UpdateSessionTitle(context.Context, string, string) error {
	return errUnexpectedClientStoreCall
}
func (unimplementedStore) UpdateSessionTitleIfEmpty(context.Context, string, string) error {
	return errUnexpectedClientStoreCall
}
func (unimplementedStore) DeleteSession(context.Context, string) error {
	return errUnexpectedClientStoreCall
}
func (unimplementedStore) ListSessionMessages(context.Context, string, int) ([]core.SessionMessageRecord, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) NextSessionMessageTurnIndex(context.Context, string) (int, error) {
	return 0, errUnexpectedClientStoreCall
}
func (unimplementedStore) AppendSessionMessage(context.Context, string, int, string, string, string) (*core.SessionMessageRecord, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) CreateFreshSessionTurn(context.Context, string, string, string) (int, error) {
	return 0, errUnexpectedClientStoreCall
}
func (unimplementedStore) LoadLatestUnboundUserMessage(context.Context, string) (*core.SessionMessageRecord, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) BindUserMessageRunIDByID(context.Context, int64, string) error {
	return errUnexpectedClientStoreCall
}
func (unimplementedStore) BindLatestUserMessageRunID(context.Context, string, int, string) error {
	return errUnexpectedClientStoreCall
}
func (unimplementedStore) SyncAssistantMessageForRun(context.Context, string) error {
	return errUnexpectedClientStoreCall
}
func (unimplementedStore) SyncAssistantMessageForRunStatus(context.Context, string, core.RunStatus) error {
	return errUnexpectedClientStoreCall
}
func (unimplementedStore) CreateRun(context.Context, core.RunCreateParams) error {
	return errUnexpectedClientStoreCall
}
func (unimplementedStore) LoadRun(context.Context, string) (*core.RunRecord, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) SearchRuns(context.Context, string, int) ([]core.RunRecord, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) FinishRun(context.Context, string, core.RunStatus, string, string) error {
	return errUnexpectedClientStoreCall
}
func (unimplementedStore) MarkInterrupted(context.Context, string, string) error {
	return errUnexpectedClientStoreCall
}
func (unimplementedStore) UpdateRunOutput(context.Context, string, string) error {
	return errUnexpectedClientStoreCall
}
func (unimplementedStore) ListActiveRuns(context.Context, int) ([]core.RunRecord, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) ListRecentTerminalRuns(context.Context, int) ([]core.RunRecord, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) AppendEvent(context.Context, string, string, any) (core.EventRecord, error) {
	return core.EventRecord{}, errUnexpectedClientStoreCall
}
func (unimplementedStore) LoadEvents(context.Context, string) ([]core.EventRecord, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) LoadEventsAfter(context.Context, string, int64) ([]core.EventRecord, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) CreatePendingAction(context.Context, core.PendingActionInput) (*core.PendingActionRecord, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) ListPendingActions(context.Context, int) ([]core.PendingActionRecord, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) LoadPendingAction(context.Context, string) (*core.PendingActionRecord, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) DecidePendingAction(context.Context, string, core.PendingActionStatus, string) (*core.PendingActionRecord, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) SavePairingCode(context.Context, *core.PairingCode) error {
	return errUnexpectedClientStoreCall
}
func (unimplementedStore) ConsumePairingCode(context.Context, string, time.Time) (*core.PairingCode, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) SaveDevice(context.Context, *core.Device) error {
	return errUnexpectedClientStoreCall
}
func (unimplementedStore) LoadDeviceByTokenHash(context.Context, string) (*core.Device, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) ListDevices(context.Context) ([]core.Device, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) TouchDevice(context.Context, string, time.Time) error {
	return errUnexpectedClientStoreCall
}
func (unimplementedStore) RevokeDevice(context.Context, string, time.Time) error {
	return errUnexpectedClientStoreCall
}
func (unimplementedStore) WriteArtifact(context.Context, core.ArtifactWriteRequest) (core.ArtifactRecord, error) {
	return core.ArtifactRecord{}, errUnexpectedClientStoreCall
}
func (unimplementedStore) ReadArtifactRange(context.Context, core.ArtifactReadRangeRequest) (core.ArtifactReadRangeResult, error) {
	return core.ArtifactReadRangeResult{}, errUnexpectedClientStoreCall
}
func (unimplementedStore) ListByRun(context.Context, string) ([]core.ArtifactRecord, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) ListBySession(context.Context, string) ([]core.ArtifactRecord, error) {
	return nil, errUnexpectedClientStoreCall
}
func (unimplementedStore) SaveOAuthToken(context.Context, core.OAuthToken) error {
	return errUnexpectedClientStoreCall
}
func (unimplementedStore) GetOAuthToken(context.Context, string) (*core.OAuthToken, error) {
	return nil, errUnexpectedClientStoreCall
}
