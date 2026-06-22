package app

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/ycvk/acorn/internal/clientevents"
	"github.com/ycvk/acorn/internal/domain"
)

const generatedThreadTitleMaxRunes = 64

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
		if strings.TrimSpace(session.Title) == "" {
			title, err := s.threadTitleFromRecentUserMessage(ctx, session.SessionID)
			if err != nil {
				return nil, err
			}
			session.Title = title
		}
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
	if strings.TrimSpace(session.Title) == "" {
		title, err := s.threadTitleFromRecentUserMessage(ctx, threadID)
		if err != nil {
			return nil, err
		}
		session.Title = title
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

func (s *ClientService) threadTitleFromRecentUserMessage(ctx context.Context, threadID string) (string, error) {
	records, err := s.store.ListSessionMessages(ctx, threadID, 20)
	if err != nil {
		return "", err
	}
	for _, record := range records {
		if record.Role != "user" {
			continue
		}
		title := generatedThreadTitle(record.Content)
		if title != "" {
			return title, nil
		}
	}
	return "", nil
}

func generatedThreadTitle(content string) string {
	compact := strings.Join(strings.Fields(content), " ")
	if compact == "" {
		return ""
	}
	if utf8.RuneCountInString(compact) <= generatedThreadTitleMaxRunes {
		return compact
	}
	runes := []rune(compact)
	return string(runes[:generatedThreadTitleMaxRunes]) + "..."
}

func (s *ClientService) projectThread(record domain.SessionRecord, latestRun *domain.RunRecord) (Thread, error) {
	thread := Thread{
		ID:            record.SessionID,
		Title:         record.Title,
		WorkspaceRoot: s.workspaceRoot,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
		State:         string(clientevents.SessionStateNew),
	}
	if latestRun == nil {
		return thread, nil
	}
	state, err := projectThreadState(latestRun.Status)
	if err != nil {
		return Thread{}, err
	}
	thread.LatestRunID = latestRun.RunID
	thread.State = state
	return thread, nil
}

func projectThreadState(status domain.RunStatus) (string, error) {
	switch status {
	case domain.RunStatusRunning:
		return string(clientevents.SessionStateRunning), nil
	case domain.RunStatusSucceeded:
		return string(clientevents.SessionStateCompleted), nil
	case domain.RunStatusFailed:
		return string(clientevents.SessionStateFailed), nil
	case domain.RunStatusInterrupted:
		return string(clientevents.SessionStateInterrupted), nil
	default:
		return "", projectionError("unknown run status %q", status)
	}
}
