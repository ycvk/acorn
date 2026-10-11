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
	store       core.SessionStore
	newThreadID func() string
}

// NewThreadService constructs a ThreadService backed by the given store.
func NewThreadService(store core.SessionStore) *ThreadService {
	return &ThreadService{
		store:       store,
		newThreadID: newThreadID,
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
	replies, err := s.store.LoadLatestRepliesForSessions(ctx, sessionIDs)
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
		if reply, ok := replies[session.SessionID]; ok {
			thread.Preview = threadPreview(reply.Content)
			thread.PreviewRole = reply.Role
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
		ID:        record.SessionID,
		Title:     record.Title,
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
		State:     string(core.SessionStateNew),
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
	runIDs := make([]string, 0, len(records))
	for _, record := range records {
		if record.Role == "assistant" && record.RunID != "" {
			runIDs = append(runIDs, record.RunID)
		}
	}
	toolEvents, err := s.store.LoadToolCallEvents(ctx, runIDs)
	if err != nil {
		return nil, err
	}
	messages := make([]Message, 0, len(records))
	for _, record := range records {
		message, err := projectMessage(record)
		if err != nil {
			return nil, err
		}
		if record.Role == "assistant" {
			if message.Steps, err = toolSteps(toolEvents[record.RunID]); err != nil {
				return nil, err
			}
		}
		messages = append(messages, message)
	}
	return messages, nil
}

const threadPreviewRunes = 120

// threadPreview is the latest reply flattened to one short line.
func threadPreview(content string) string {
	return clip(strings.Join(strings.Fields(content), " "), threadPreviewRunes)
}

// toolSteps folds a run's tool.call.* events into the calls it made, in the
// order they started. Runs recorded before tool.call.started existed only
// have results, which become steps without a summary.
func toolSteps(events []core.EventRecord) ([]ToolStep, error) {
	steps := []ToolStep{}
	index := map[string]int{}
	for _, event := range events {
		payload, ok := event.Payload.(map[string]any)
		if !ok {
			return nil, projectionError("tool call event payload must be object: run_id=%s sequence=%d", event.RunID, event.Sequence)
		}
		data, err := ProjectRunEventData(event.Kind, payload)
		if err != nil {
			return nil, err
		}
		call, ok := data.(core.ToolCallData)
		if !ok {
			return nil, projectionError("event %q is not a tool call: run_id=%s sequence=%d", event.Kind, event.RunID, event.Sequence)
		}
		position, seen := index[call.ToolCallID]
		if !seen {
			index[call.ToolCallID] = len(steps)
			position = len(steps)
			steps = append(steps, ToolStep{ToolName: call.ToolName, Outcome: ToolStepRunning})
		}
		switch event.Kind {
		case "tool.call.started":
			steps[position].Summary = call.Summary
		case "tool.call.succeeded":
			steps[position].Outcome = ToolStepSucceeded
		case "tool.call.failed":
			steps[position].Outcome = ToolStepFailed
			steps[position].Error = call.Error
		}
	}
	return steps, nil
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
// user), a commitment wake (role wake) or a capture (role capture), and returns the stored record
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
	case "user", core.MessageRoleWake, core.MessageRoleCapture, "assistant", "system", "tool":
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
	ID          string
	Title       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	LatestRunID string
	State       string
	Preview     string
	PreviewRole string
}

// Message is a user-facing message DTO.
type Message struct {
	ID        string
	ThreadID  string
	Role      string
	Content   MessageContent
	CreatedAt time.Time
	RunID     string
	Steps     []ToolStep
}

// ToolStep is one tool call behind an assistant message.
type ToolStep struct {
	ToolName string
	Summary  string
	Outcome  string
	Error    string
}

const (
	ToolStepRunning   = "running"
	ToolStepSucceeded = "succeeded"
	ToolStepFailed    = "failed"
)

// MessageContent holds the content of a message.
type MessageContent struct {
	Type string
	Text string
}
