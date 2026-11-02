package api

import (
	"context"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func (s *clientHandlerStore) CreateSession(_ context.Context, sessionID, title string) (*core.SessionRecord, error) {
	stub, err := s.stubOrErr()
	if err != nil {
		return nil, err
	}
	stub.createThreadTitle = title
	if strings.TrimSpace(stub.thread.ID) == "" {
		stub.thread.ID = sessionID
	}
	stub.thread.Title = strings.TrimSpace(title)
	record := s.sessionRecord()
	record.SessionID = strings.TrimSpace(stub.thread.ID)
	record.Title = strings.TrimSpace(title)
	return &record, nil
}

func (s *clientHandlerStore) LoadSession(_ context.Context, sessionID string) (*core.SessionRecord, error) {
	stub, err := s.stubOrErr()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(stub.thread.ID) == "" {
		return nil, core.ErrSessionNotFound
	}
	record := s.sessionRecord()
	if strings.TrimSpace(sessionID) != "" {
		record.SessionID = sessionID
	}
	return &record, nil
}

func (s *clientHandlerStore) ListSessions(context.Context, int) ([]core.SessionRecord, error) {
	stub, err := s.stubOrErr()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(stub.thread.ID) == "" {
		return nil, nil
	}
	return []core.SessionRecord{s.sessionRecord()}, nil
}

func (s *clientHandlerStore) LoadLatestRunForSession(_ context.Context, _ string) (*core.RunRecord, error) {
	stub, err := s.stubOrErr()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(stub.run.ID) == "" {
		return nil, nil
	}
	return s.runRecord(stub.run.ID), nil
}

func (s *clientHandlerStore) LoadLatestRunsForSessions(_ context.Context, sessionIDs []string) (map[string]*core.RunRecord, error) {
	stub, err := s.stubOrErr()
	if err != nil {
		return nil, err
	}
	out := make(map[string]*core.RunRecord, len(sessionIDs))
	if strings.TrimSpace(stub.run.ID) == "" {
		return out, nil
	}
	run := s.runRecord(stub.run.ID)
	for _, sessionID := range sessionIDs {
		if sessionID == run.SessionID {
			out[sessionID] = run
		}
	}
	return out, nil
}

func (s *clientHandlerStore) UpdateSessionTitle(_ context.Context, sessionID, title string) error {
	stub, err := s.stubOrErr()
	if err != nil {
		return err
	}
	stub.updateThreadID = sessionID
	stub.updateThreadTitle = title
	stub.thread.Title = strings.TrimSpace(title)
	return nil
}

func (s *clientHandlerStore) UpdateSessionTitleIfEmpty(_ context.Context, _ string, title string) error {
	stub, err := s.stubOrErr()
	if err != nil {
		return err
	}
	if strings.TrimSpace(stub.thread.Title) == "" {
		stub.thread.Title = strings.TrimSpace(title)
	}
	return nil
}

func (s *clientHandlerStore) DeleteThread(_ context.Context, sessionID string) error {
	stub, err := s.stubOrErr()
	if err != nil {
		return err
	}
	stub.deleteThreadID = sessionID
	return nil
}

func (s *clientHandlerStore) ListSessionMessages(_ context.Context, _ string, _ int) ([]core.SessionMessageRecord, error) {
	stub, err := s.stubOrErr()
	if err != nil {
		return nil, err
	}
	record := s.messageRecord()
	if record == nil {
		return nil, nil
	}
	if strings.TrimSpace(record.Role) == "" {
		record.Role = "user"
	}
	if strings.TrimSpace(record.SessionID) == "" {
		record.SessionID = strings.TrimSpace(stub.thread.ID)
	}
	return []core.SessionMessageRecord{*record}, nil
}

func (s *clientHandlerStore) NextSessionMessageTurnIndex(context.Context, string) (int, error) {
	if _, err := s.stubOrErr(); err != nil {
		return 0, err
	}
	return 1, nil
}

func (s *clientHandlerStore) AppendSessionMessage(_ context.Context, threadID string, turnIndex int, role, content, runID string) (*core.SessionMessageRecord, error) {
	stub, err := s.stubOrErr()
	if err != nil {
		return nil, err
	}
	stub.createMessageThreadID = threadID
	stub.createMessageContent = content
	stub.message = Message{
		ID:       "7",
		ThreadID: threadID,
		Role:     role,
		Content: MessageContent{
			Type: "text",
			Text: content,
		},
		CreatedAt: time.Date(2026, 5, 2, 10, 2, 0, 0, time.UTC),
		RunID:     runID,
	}
	return &core.SessionMessageRecord{
		ID:        7,
		SessionID: threadID,
		TurnIndex: turnIndex,
		Role:      role,
		Content:   content,
		RunID:     runID,
		CreatedAt: stub.message.CreatedAt,
	}, nil
}

func (s *clientHandlerStore) CreateFreshSessionTurn(context.Context, string, string, string) (int, error) {
	if _, err := s.stubOrErr(); err != nil {
		return 0, err
	}
	return 1, nil
}

func (s *clientHandlerStore) BindUserMessageRunIDByID(_ context.Context, _ int64, runID string) error {
	stub, err := s.stubOrErr()
	if err != nil {
		return err
	}
	stub.message.RunID = runID
	return nil
}

func (s *clientHandlerStore) BindLatestUserMessageRunID(_ context.Context, _ string, _ int, runID string) error {
	stub, err := s.stubOrErr()
	if err != nil {
		return err
	}
	stub.message.RunID = runID
	return nil
}

func (s *clientHandlerStore) SyncAssistantMessageForRun(context.Context, string) error {
	_, err := s.stubOrErr()
	return err
}

func (s *clientHandlerStore) SyncAssistantMessageForRunStatus(context.Context, string, core.RunStatus) error {
	_, err := s.stubOrErr()
	return err
}

func (s *clientHandlerStore) CreateRun(context.Context, core.RunCreateParams) error {
	_, err := s.stubOrErr()
	return err
}

func (s *clientHandlerStore) LoadRun(_ context.Context, runID string) (*core.RunRecord, error) {
	if _, err := s.stubOrErr(); err != nil {
		return nil, err
	}
	return s.runRecord(runID), nil
}

func (s *clientHandlerStore) RunWoken(context.Context, string) (bool, error) {
	_, err := s.stubOrErr()
	return false, err
}

func (s *clientHandlerStore) FinishRun(_ context.Context, end core.RunEnd) (core.EventRecord, *core.RunStart, error) {
	stub, err := s.stubOrErr()
	if err != nil {
		return core.EventRecord{}, nil, err
	}
	stub.run.ID = end.RunID
	switch end.Status {
	case core.RunStatusSucceeded:
		stub.run.Status = "completed"
	case core.RunStatusInterrupted:
		stub.run.Status = "interrupted"
	case core.RunStatusFailed:
		stub.run.Status = "failed"
	default:
		stub.run.Status = "running"
	}
	return core.EventRecord{RunID: end.RunID, Kind: end.Event, Payload: end.Payload}, nil, nil
}

// SendOwnerMessage starts a run on the message: the stub thread has no run
// going that it would wait for.
func (s *clientHandlerStore) SendOwnerMessage(_ context.Context, message core.OwnerMessage) (core.SentOwnerMessage, error) {
	stub, err := s.stubOrErr()
	if err != nil {
		return core.SentOwnerMessage{}, err
	}
	stub.createMessageThreadID = message.SessionID
	stub.createMessageContent = message.Content
	record := core.SessionMessageRecord{ID: 7, SessionID: message.SessionID, TurnIndex: 1, Role: "user", Content: message.Content, RunID: message.RunID, CreatedAt: time.Date(2026, 5, 2, 10, 2, 0, 0, time.UTC)}
	return core.SentOwnerMessage{RunID: message.RunID, Started: &core.RunStart{RunID: message.RunID, SessionID: message.SessionID, TurnIndex: 1, Input: message.Content, Messages: []core.SessionMessageRecord{record}}}, nil
}

func (s *clientHandlerStore) ListQueuedMessages(context.Context, string) ([]core.QueuedMessage, error) {
	_, err := s.stubOrErr()
	return nil, err
}

func (s *clientHandlerStore) MarkInterrupted(context.Context, string, string) error {
	_, err := s.stubOrErr()
	return err
}

func (s *clientHandlerStore) UpdateRunOutput(context.Context, string, string) error {
	_, err := s.stubOrErr()
	return err
}

func (s *clientHandlerStore) LoadLatestRepliesForSessions(_ context.Context, _ []string) (map[string]core.SessionMessageRecord, error) {
	if _, err := s.stubOrErr(); err != nil {
		return nil, err
	}
	return map[string]core.SessionMessageRecord{}, nil
}
