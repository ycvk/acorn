package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

type pendingActionServiceStore struct {
	stub        *pendingActionHandlerStub
	record      core.PendingActionRecord
	summaries   []PendingActionSummary
	detail      *PendingActionDetail
	err         error
	actionID    string
	getActionID string
	decision    PendingActionDecisionInput
	listLimit   int
	decisionMap map[string]PendingActionDecisionInput
}

func newPendingActionTestService(stub *pendingActionHandlerStub) *PendingActionService {
	if stub == nil {
		stub = &pendingActionHandlerStub{}
	}
	record := stub.record
	if strings.TrimSpace(record.ActionID) == "" {
		record.ActionID = "action_1"
	}
	if strings.TrimSpace(record.RunID) == "" {
		record.RunID = "run_1"
	}
	if record.Kind == "" {
		record.Kind = core.PendingActionKindElicitation
	}
	if record.Status == "" {
		record.Status = core.PendingActionStatusPending
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	}
	store := &pendingActionServiceStore{
		stub:      stub,
		record:    record,
		summaries: append([]PendingActionSummary(nil), stub.summaries...),
		detail:    stub.detail,
		err:       stub.err,
		decisionMap: map[string]PendingActionDecisionInput{
			`{"action":"accept"}`:  {Decision: "accept"},
			`{"action":"decline"}`: {Decision: "decline"},
			`{"action":"maybe"}`:   {Decision: "maybe"},
		},
	}
	return NewPendingActionService(store)
}

func (s *pendingActionServiceStore) loadRunForThread(runID, threadID string) core.RunRecord {
	return core.RunRecord{
		RunID:     runID,
		SessionID: threadID,
		Status:    core.RunStatusRunning,
		Input:     "",
		CreatedAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
	}
}

func pendingActionRecordFromSummary(item PendingActionSummary) core.PendingActionRecord {
	return core.PendingActionRecord{
		ActionID: item.ActionID,
		RunID:    item.RunID,
		Kind:     core.PendingActionKind(item.Kind),
		Subject:  item.Title,
		PayloadJSON: func() string {
			if strings.TrimSpace(item.Body) == "" {
				return ""
			}
			return fmt.Sprintf(`{"message":%q}`, item.Body)
		}(),
		Status:    core.PendingActionStatus(item.Status),
		CreatedAt: item.CreatedAt,
	}
}

func (s *pendingActionServiceStore) ListPendingActions(_ context.Context, limit int) ([]core.PendingActionRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.listLimit = limit
	if s.stub != nil {
		s.stub.listLimit = limit
	}
	out := make([]core.PendingActionRecord, 0, len(s.summaries))
	for _, item := range s.summaries {
		out = append(out, pendingActionRecordFromSummary(item))
	}
	return out, nil
}

func (s *pendingActionServiceStore) LoadPendingAction(_ context.Context, actionID string) (*core.PendingActionRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.getActionID = actionID
	if s.stub != nil {
		s.stub.getActionID = actionID
	}
	if strings.TrimSpace(actionID) == "" {
		return nil, core.ErrPendingActionNotFound
	}
	if s.detail != nil {
		record := pendingActionRecordFromSummary(s.detail.PendingActionSummary)
		record.Reason = s.detail.Reason
		if len(s.detail.Payload) > 0 {
			record.PayloadJSON = fmt.Sprintf(`{"message":%q}`, s.detail.Payload["message"])
		}
		return &record, nil
	}
	record := s.record
	if strings.TrimSpace(record.ActionID) == "" {
		record = core.PendingActionRecord{
			ActionID:    actionID,
			RunID:       "run_1",
			Kind:        core.PendingActionKindElicitation,
			Status:      core.PendingActionStatusPending,
			CreatedAt:   time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
			PayloadJSON: `{"message":"Approval required"}`,
		}
	}
	return &record, nil
}

func (s *pendingActionServiceStore) DecidePendingAction(_ context.Context, actionID string, status core.PendingActionStatus, decisionJSON string) (*core.PendingActionRecord, error) {
	s.actionID = actionID
	if s.stub != nil {
		s.stub.actionID = actionID
	}
	if item, ok := s.decisionMap[decisionJSON]; ok {
		s.decision = item
		if s.stub != nil {
			s.stub.decision = item
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	record := s.record
	record.ActionID = actionID
	record.Status = status
	record.DecisionJSON = decisionJSON
	return &record, nil
}

func (s *pendingActionServiceStore) LoadRun(_ context.Context, runID string) (*core.RunRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.detail != nil {
		record := s.loadRunForThread(runID, s.detail.ThreadID)
		return &record, nil
	}
	if len(s.summaries) > 0 {
		record := s.loadRunForThread(runID, s.summaries[0].ThreadID)
		return &record, nil
	}
	record := s.loadRunForThread(runID, "thread_1")
	return &record, nil
}

func (s *pendingActionServiceStore) LoadEvents(context.Context, string) ([]core.EventRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	return nil, nil
}

func (s *pendingActionServiceStore) LoadEventsAfter(context.Context, string, int64) ([]core.EventRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	return nil, nil
}

func (s *pendingActionServiceStore) AppendEvent(context.Context, string, string, any) (core.EventRecord, error) {
	if s.err != nil {
		return core.EventRecord{}, s.err
	}
	return core.EventRecord{Sequence: 1}, nil
}

func (s *pendingActionServiceStore) unsupportedStoreCall() error {
	if s.err != nil {
		return s.err
	}
	return errUnexpectedClientStoreCall
}

func (s *pendingActionServiceStore) CreateSession(context.Context, string, string) (*core.SessionRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) LoadSession(context.Context, string) (*core.SessionRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) ListSessions(context.Context, int) ([]core.SessionRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) LoadLatestRunForSession(context.Context, string) (*core.RunRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) LoadLatestRunsForSessions(context.Context, []string) (map[string]*core.RunRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) UpdateSessionTitle(context.Context, string, string) error {
	return s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) UpdateSessionTitleIfEmpty(context.Context, string, string) error {
	return s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) DeleteSession(context.Context, string) error {
	return s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) ListSessionMessages(context.Context, string, int) ([]core.SessionMessageRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) NextSessionMessageTurnIndex(context.Context, string) (int, error) {
	return 0, s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) AppendSessionMessage(context.Context, string, int, string, string, string) (*core.SessionMessageRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) CreateFreshSessionTurn(context.Context, string, string, string) (int, error) {
	return 0, s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) LoadLatestUnboundUserMessage(context.Context, string) (*core.SessionMessageRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) BindUserMessageRunIDByID(context.Context, int64, string) error {
	return s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) BindLatestUserMessageRunID(context.Context, string, int, string) error {
	return s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) SyncAssistantMessageForRun(context.Context, string) error {
	return s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) SyncAssistantMessageForRunStatus(context.Context, string, core.RunStatus) error {
	return s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) CreateRun(context.Context, core.RunCreateParams) error {
	return s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) FinishRun(context.Context, string, core.RunStatus, string, string) error {
	return s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) MarkInterrupted(context.Context, string, string) error {
	return s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) UpdateRunOutput(context.Context, string, string) error {
	return s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) ListActiveRuns(context.Context, int) ([]core.RunRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) ListRecentTerminalRuns(context.Context, int) ([]core.RunRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) CreatePendingAction(context.Context, core.PendingActionInput) (*core.PendingActionRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) SavePairingCode(context.Context, *core.PairingCode) error {
	return s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) ConsumePairingCode(context.Context, string, time.Time) (*core.PairingCode, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) SaveDevice(context.Context, *core.Device) error {
	return s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) LoadDeviceByTokenHash(context.Context, string) (*core.Device, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) ListDevices(context.Context) ([]core.Device, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) TouchDevice(context.Context, string, time.Time) error {
	return s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) RevokeDevice(context.Context, string, time.Time) error {
	return s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) WriteArtifact(context.Context, core.ArtifactWriteRequest) (core.ArtifactRecord, error) {
	return core.ArtifactRecord{}, s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) ReadArtifactRange(context.Context, core.ArtifactReadRangeRequest) (core.ArtifactReadRangeResult, error) {
	return core.ArtifactReadRangeResult{}, s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) ListByRun(context.Context, string) ([]core.ArtifactRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) ListBySession(context.Context, string) ([]core.ArtifactRecord, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) GetSessionSummary(context.Context, string) (*core.SessionSummary, error) {
	return nil, s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) UpsertSessionSummary(context.Context, core.SessionSummary) error {
	return s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) SaveOAuthToken(context.Context, core.OAuthToken) error {
	return s.unsupportedStoreCall()
}
func (s *pendingActionServiceStore) GetOAuthToken(context.Context, string) (*core.OAuthToken, error) {
	return nil, s.unsupportedStoreCall()
}
