package app

import (
	"context"

	"github.com/ycvk/acorn/internal/events"
)

type SessionService struct {
	store sessionStore
}

func NewSessionService(store sessionStore) *SessionService {
	return &SessionService{store: store}
}

func (s *SessionService) CreateSession(ctx context.Context, sessionID, title string) (*events.SessionRecord, error) {
	return s.store.CreateSession(ctx, sessionID, title)
}

func (s *SessionService) ListSessionMessages(ctx context.Context, sessionID string, limit int) ([]events.SessionMessageRecord, error) {
	return s.store.ListSessionMessages(ctx, sessionID, limit)
}
