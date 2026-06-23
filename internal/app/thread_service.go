package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ycvk/acorn/internal/clientevents"
	"github.com/ycvk/acorn/internal/domain"
)

// ThreadService owns thread and message CRUD: it reads/writes session records,
// derives thread state from the latest run, and projects stored messages into
// client-facing DTOs.
type ThreadService struct {
	store         containerAppStore
	workspaceRoot string
	newThreadID   func() string
}

// NewThreadService constructs a ThreadService backed by the given store.
func NewThreadService(store containerAppStore, workspaceRoot string) *ThreadService {
	return &ThreadService{
		store:         store,
		workspaceRoot: workspaceRoot,
		newThreadID:   newThreadID,
	}
}

func newThreadID() string {
	return fmt.Sprintf("session_%d", time.Now().UTC().UnixNano())
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

func (s *ThreadService) projectThread(record domain.SessionRecord, latestRun *domain.RunRecord) (Thread, error) {
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
	record, err := s.createUserMessage(ctx, threadID, content)
	if err != nil {
		return nil, err
	}
	message, err := projectMessage(*record)
	if err != nil {
		return nil, err
	}
	return &message, nil
}

// createUserMessage records a pending user message and returns the stored record
// (including its id and turn index) so a run can bind to that exact message id.
func (s *ThreadService) createUserMessage(ctx context.Context, threadID, content string) (*domain.SessionMessageRecord, error) {
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
	record, err := s.store.AppendSessionMessage(ctx, threadID, turnIndex, "user", trimmed, "")
	if err != nil {
		return nil, err
	}
	if err := s.store.UpdateSessionTitleIfEmpty(ctx, threadID, generatedThreadTitle(trimmed)); err != nil {
		return nil, err
	}
	return record, nil
}

func projectMessage(record domain.SessionMessageRecord) (Message, error) {
	switch record.Role {
	case "user", "assistant", "system", "tool":
	default:
		return Message{}, projectionError("message %d has unsupported role %q", record.ID, record.Role)
	}
	parts, err := projectMessageParts(record)
	if err != nil {
		return Message{}, err
	}
	return Message{
		ID:       fmt.Sprintf("%d", record.ID),
		ThreadID: record.SessionID,
		Role:     record.Role,
		Content: MessageContent{
			Type:  "text",
			Text:  record.Content,
			Parts: parts,
		},
		CreatedAt: record.CreatedAt,
		RunID:     record.RunID,
	}, nil
}

func projectMessageParts(record domain.SessionMessageRecord) ([]MessagePart, error) {
	if len(record.ContentParts) == 0 {
		return nil, nil
	}
	var parts []MessagePart
	if err := json.Unmarshal(record.ContentParts, &parts); err != nil {
		return nil, projectionError("message %d has invalid content_parts: %v", record.ID, err)
	}
	for index, part := range parts {
		if err := validateMessagePart(part); err != nil {
			return nil, projectionError("message %d content parts[%d]: %v", record.ID, index, err)
		}
	}
	return parts, nil
}

func validateMessagePart(part MessagePart) error {
	switch part.Kind {
	case "text":
		if strings.TrimSpace(part.Text) == "" {
			return errors.New("text part requires text")
		}
	case "reasoning":
		if strings.TrimSpace(part.Reasoning) == "" {
			return errors.New("reasoning part requires reasoning")
		}
	case "work_status":
		switch part.Status {
		case "working", "interrupted", "failed":
		default:
			return fmt.Errorf("work_status part has unsupported status %q", part.Status)
		}
		if strings.TrimSpace(part.Title) == "" || strings.TrimSpace(part.Summary) == "" {
			return errors.New("work_status part requires title and summary")
		}
		if err := validateMessageAction(part.Action); err != nil {
			return fmt.Errorf("work_status part action: %w", err)
		}
	case "decision":
		if strings.TrimSpace(part.DecisionID) == "" || strings.TrimSpace(part.Question) == "" {
			return errors.New("decision part requires decision_id and question")
		}
		switch part.Status {
		case "", string(domain.PendingActionStatusPending), string(domain.PendingActionStatusApproved), string(domain.PendingActionStatusRejected), string(domain.PendingActionStatusResolved):
		default:
			return fmt.Errorf("decision part has unsupported status %q", part.Status)
		}
	case "result":
		if strings.TrimSpace(part.Title) == "" {
			return errors.New("result part requires title")
		}
	case "disclosure":
		if len(part.Items) == 0 {
			return errors.New("disclosure part requires items")
		}
		for index, item := range part.Items {
			if err := validateDisclosureItem(item); err != nil {
				return fmt.Errorf("disclosure part items[%d]: %w", index, err)
			}
		}
	case "technical_detail_link":
		if strings.TrimSpace(part.RunID) == "" && strings.TrimSpace(part.DetailRunID) == "" {
			return errors.New("technical_detail_link part requires run_id")
		}
	default:
		return fmt.Errorf("unsupported kind %q", part.Kind)
	}
	return nil
}

func validateDisclosureItem(item DisclosureItem) error {
	switch item.Kind {
	case "memory", "skill":
	default:
		return fmt.Errorf("unsupported kind %q", item.Kind)
	}
	if strings.TrimSpace(item.Label) == "" {
		return errors.New("label is required")
	}
	switch item.Tone {
	case "memory", "skill", "procedure", "neutral", "warning":
	default:
		return fmt.Errorf("unsupported tone %q", item.Tone)
	}
	if strings.TrimSpace(item.SkillID) != "" && item.Kind != "skill" {
		return errors.New("skill_id is only supported for skill disclosure items")
	}
	return nil
}

func validateMessageAction(action *MessageAction) error {
	if action == nil {
		return nil
	}
	switch action.Kind {
	case "resume_run":
	default:
		return fmt.Errorf("unsupported kind %q", action.Kind)
	}
	if strings.TrimSpace(action.RunID) == "" {
		return errors.New("run_id is required")
	}
	if strings.TrimSpace(action.Label) == "" {
		return errors.New("label is required")
	}
	return nil
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
	Type  string
	Text  string
	Parts []MessagePart
}

// MessagePart is a single part of a message content.
type MessagePart struct {
	Kind             string           `json:"kind"`
	Text             string           `json:"text,omitempty"`
	Reasoning        string           `json:"reasoning,omitempty"`
	Status           string           `json:"status,omitempty"`
	Title            string           `json:"title,omitempty"`
	Summary          string           `json:"summary,omitempty"`
	Changed          []string         `json:"changed,omitempty"`
	Verified         []string         `json:"verified,omitempty"`
	Risks            []string         `json:"risks,omitempty"`
	Items            []DisclosureItem `json:"items,omitempty"`
	DetailRunID      string           `json:"detail_run_id,omitempty"`
	RunID            string           `json:"run_id,omitempty"`
	Label            string           `json:"label,omitempty"`
	DecisionID       string           `json:"decision_id,omitempty"`
	Question         string           `json:"question,omitempty"`
	SelectedOptionID string           `json:"selected_option_id,omitempty"`
	Answer           string           `json:"answer,omitempty"`
	Options          []DecisionOption `json:"options,omitempty"`
	Action           *MessageAction   `json:"action,omitempty"`
}

// DisclosureItem is an item inside a disclosure message part.
type DisclosureItem struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Detail  string `json:"detail,omitempty"`
	Tone    string `json:"tone"`
	SkillID string `json:"skill_id,omitempty"`
}

// DecisionOption is an option inside a decision message part.
type DecisionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// MessageAction is an action associated with a message part.
type MessageAction struct {
	Kind  string `json:"kind"`
	RunID string `json:"run_id"`
	Label string `json:"label"`
}
