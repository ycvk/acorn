package app

import (
	"context"
	"errors"
	"strings"
)

func (s *ClientService) ListThreads(ctx context.Context, limit int) ([]Thread, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("client store is nil")
	}
	sessions, err := s.store.ListSessions(ctx, limit)
	if err != nil {
		return nil, err
	}
	sessionIDs := make([]string, 0, len(sessions))
	for _, session := range sessions {
		sessionIDs = append(sessionIDs, session.SessionID)
	}
	latestRuns, err := s.store.LoadLatestRunsForSessions(ctx, sessionIDs)
	if err != nil {
		return nil, err
	}
	items := make([]Thread, 0, len(sessions))
	for _, session := range sessions {
		thread, err := s.projectThread(session, latestRuns[session.SessionID])
		if err != nil {
			return nil, err
		}
		items = append(items, thread)
	}
	return items, nil
}

func (s *ClientService) CreateThread(ctx context.Context, title string) (*Thread, error) {
	if s == nil || s.store == nil || s.newThreadID == nil {
		return nil, errors.New("client service is not initialized")
	}
	session, err := s.store.CreateSession(ctx, s.newThreadID(), strings.TrimSpace(title))
	if err != nil {
		return nil, err
	}
	thread, err := s.projectThread(*session, nil)
	if err != nil {
		return nil, err
	}
	return &thread, nil
}

func (s *ClientService) GetThread(ctx context.Context, threadID string) (*Thread, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("client store is nil")
	}
	session, err := s.store.LoadSession(ctx, threadID)
	if err != nil {
		return nil, err
	}
	latestRun, err := s.store.LoadLatestRunForSession(ctx, threadID)
	if err != nil {
		return nil, err
	}
	thread, err := s.projectThread(*session, latestRun)
	if err != nil {
		return nil, err
	}
	return &thread, nil
}

func (s *ClientService) UpdateThread(ctx context.Context, threadID, title string) (*Thread, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("client store is nil")
	}
	if err := s.store.UpdateSessionTitle(ctx, threadID, title); err != nil {
		return nil, err
	}
	return s.GetThread(ctx, threadID)
}

func (s *ClientService) DeleteThread(ctx context.Context, threadID string) error {
	if s == nil || s.store == nil {
		return errors.New("client store is nil")
	}
	return s.store.DeleteSession(ctx, threadID)
}

func (s *ClientService) ListMessages(ctx context.Context, threadID string, limit int) ([]Message, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("client store is nil")
	}
	if _, err := s.store.LoadSession(ctx, threadID); err != nil {
		return nil, err
	}
	records, err := s.store.ListSessionMessages(ctx, threadID, limit)
	if err != nil {
		return nil, err
	}
	messages := make([]Message, 0, len(records))
	for _, record := range records {
		message, err := projectMessage(record)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, nil
}

func (s *ClientService) CreateMessage(ctx context.Context, threadID, content string) (*Message, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("client store is nil")
	}
	if _, err := s.store.LoadSession(ctx, threadID); err != nil {
		return nil, err
	}
	turnIndex, err := s.store.NextSessionMessageTurnIndex(ctx, threadID)
	if err != nil {
		return nil, err
	}
	record, err := s.store.AppendSessionMessage(threadID, turnIndex, "user", strings.TrimSpace(content), "")
	if err != nil {
		return nil, err
	}
	message, err := projectMessage(*record)
	if err != nil {
		return nil, err
	}
	return &message, nil
}
