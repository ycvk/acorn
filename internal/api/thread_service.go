package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ycvk/acorn/internal/core"
)

// ThreadService owns thread and message CRUD: it reads/writes session records,
// derives thread state from the latest run, and projects stored messages into
// client-facing DTOs.
type ThreadService struct {
	store         core.SessionStore
	workspaceRoot string
	newThreadID   func() string
}

// NewThreadService constructs a ThreadService backed by the given store.
func NewThreadService(store core.SessionStore, workspaceRoot string) *ThreadService {
	return &ThreadService{
		store:         store,
		workspaceRoot: workspaceRoot,
		newThreadID:   newThreadID,
	}
}

func newThreadID() string {
	return core.NewSessionID()
}

const generatedThreadTitleMaxRunes = 64

func (s *ThreadService) ListThreads(ctx context.Context, limit int) ([]Thread, error) {
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

func (s *ThreadService) CreateThread(ctx context.Context, title string) (*Thread, error) {
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

func (s *ThreadService) GetThread(ctx context.Context, threadID string) (*Thread, error) {
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

func (s *ThreadService) UpdateThread(ctx context.Context, threadID, title string) (*Thread, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("client store is nil")
	}
	if err := s.store.UpdateSessionTitle(ctx, threadID, title); err != nil {
		return nil, err
	}
	return s.GetThread(ctx, threadID)
}

func (s *ThreadService) DeleteThread(ctx context.Context, threadID string) error {
	if s == nil || s.store == nil {
		return errors.New("client store is nil")
	}
	return s.store.DeleteSession(ctx, threadID)
}

func (s *ThreadService) threadTitleFromRecentUserMessage(ctx context.Context, threadID string) (string, error) {
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

func (s *ThreadService) projectThread(record core.SessionRecord, latestRun *core.RunRecord) (Thread, error) {
	thread := Thread{
		ID:            record.SessionID,
		Title:         record.Title,
		WorkspaceRoot: s.workspaceRoot,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
		State:         string(core.SessionStateNew),
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

func projectThreadState(status core.RunStatus) (string, error) {
	switch status {
	case core.RunStatusRunning:
		return string(core.SessionStateRunning), nil
	case core.RunStatusSucceeded:
		return string(core.SessionStateCompleted), nil
	case core.RunStatusFailed:
		return string(core.SessionStateFailed), nil
	case core.RunStatusInterrupted:
		return string(core.SessionStateInterrupted), nil
	default:
		return "", projectionError("unknown run status %q", status)
	}
}

func (s *ThreadService) ListMessages(ctx context.Context, threadID string, limit int) ([]Message, error) {
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

func (s *ThreadService) CreateMessage(ctx context.Context, threadID, content string) (*Message, error) {
	record, err := s.createInputMessage(ctx, threadID, "user", content)
	if err != nil {
		return nil, err
	}
	message, err := projectMessage(*record)
	if err != nil {
		return nil, err
	}
	return &message, nil
}

// createInputMessage records the input of a run, the owner's message (role
// user) or a commitment wake (role wake), and returns the stored record
// (including its id and turn index) so the run can bind to that exact id.
func (s *ThreadService) createInputMessage(ctx context.Context, threadID, role, content string) (*core.SessionMessageRecord, error) {
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
	trimmed := strings.TrimSpace(content)
	record, err := s.store.AppendSessionMessage(ctx, threadID, turnIndex, role, trimmed, "")
	if err != nil {
		return nil, err
	}
	if err := s.store.UpdateSessionTitleIfEmpty(ctx, threadID, generatedThreadTitle(trimmed)); err != nil {
		return nil, err
	}
	return record, nil
}

func projectMessage(record core.SessionMessageRecord) (Message, error) {
	switch record.Role {
	case "user", core.MessageRoleWake, "assistant", "system", "tool":
	default:
		return Message{}, projectionError("message %d has unsupported role %q", record.ID, record.Role)
	}
	return Message{
		ID:       fmt.Sprintf("%d", record.ID),
		ThreadID: record.SessionID,
		Role:     record.Role,
		Content: MessageContent{
			Type: "text",
			Text: record.Content,
		},
		CreatedAt: record.CreatedAt,
		RunID:     record.RunID,
	}, nil
}

// Thread is a user-facing thread DTO.
type Thread struct {
	ID            string
	Title         string
	WorkspaceRoot string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	LatestRunID   string
	State         string
}

// Message is a user-facing message DTO.
type Message struct {
	ID        string
	ThreadID  string
	Role      string
	Content   MessageContent
	CreatedAt time.Time
	RunID     string
}

// MessageContent holds the content of a message.
type MessageContent struct {
	Type string
	Text string
}
