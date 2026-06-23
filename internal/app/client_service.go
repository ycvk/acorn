package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/ycvk/acorn/internal/clientevents"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/store"
)

type ClientService struct {
	store         containerAppStore
	newExecutor   func(context.Context) (executorHandle, error)
	controller    *runtime.RunController
	workspaceRoot string
	eventPoll     time.Duration
	newThreadID   func() string
	newRunID      func() string
	reportError   func(context.Context, string, error)
}

func BuildClientService(store containerAppStore, newExecutor func(context.Context) (executorHandle, error), controller *runtime.RunController, workspaceRoot string) *ClientService {
	return &ClientService{
		store:         store,
		newExecutor:   newExecutor,
		controller:    controller,
		workspaceRoot: workspaceRoot,
		eventPoll:     100 * time.Millisecond,
		newThreadID:   newThreadID,
		newRunID:      newRunID,
		reportError:   reportClientBackgroundError,
	}
}

func newThreadID() string {
	return fmt.Sprintf("session_%d", time.Now().UTC().UnixNano())
}

func newRunID() string {
	return fmt.Sprintf("run_%d", time.Now().UTC().UnixNano())
}

func (s *ClientService) GetRun(ctx context.Context, runID string) (*Run, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("client store is nil")
	}
	record, err := s.store.LoadRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	run, err := projectRun(*record)
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *ClientService) RunIsTerminal(ctx context.Context, runID string) (bool, error) {
	if s == nil || s.store == nil {
		return false, errors.New("client store is nil")
	}
	record, err := s.store.LoadRun(ctx, runID)
	if err != nil {
		return false, err
	}
	switch record.Status {
	case domain.RunStatusRunning:
		return false, nil
	case domain.RunStatusSucceeded, domain.RunStatusInterrupted, domain.RunStatusFailed:
		return true, nil
	default:
		return false, projectionError("unknown run status %q", record.Status)
	}
}

func (s *ClientService) InterruptRun(ctx context.Context, runID string) error {
	_ = ctx
	if s == nil || s.controller == nil {
		return errors.New("run controller is nil")
	}
	return s.controller.Interrupt(runID)
}

func (s *ClientService) CreateRun(ctx context.Context, threadID, skillID, input string) (*Run, error) {
	if s == nil || s.store == nil || s.newExecutor == nil || s.newRunID == nil {
		return nil, errors.New("client service is not initialized")
	}
	threadID = strings.TrimSpace(threadID)
	skillID = strings.TrimSpace(skillID)
	if _, err := s.store.LoadSession(ctx, threadID); err != nil {
		return nil, err
	}
	// Resolve the user message this run binds to, then bind by its exact id (see
	// req.BoundMessageID below). Single-step (input set) records a fresh message
	// and binds that id — race-free, no separate POST /messages needed. The
	// two-step flow (empty input, message posted via POST /messages) reads the
	// latest unbound message and binds by its id; concurrent two-step creates on
	// one thread bind the same id and the second fails loud (RowsAffected=0 -> run
	// rolled back), never silently mis-binding.
	var message *domain.SessionMessageRecord
	var err error
	if strings.TrimSpace(input) != "" {
		message, err = s.createUserMessage(ctx, threadID, input)
		if err != nil {
			return nil, err
		}
	} else {
		message, err = s.store.LoadLatestUnboundUserMessage(ctx, threadID)
		if err != nil {
			if errors.Is(err, store.ErrSessionMessageNotFound) {
				return nil, fmt.Errorf("%w: thread %s", ErrClientNoPendingMessage, threadID)
			}
			return nil, err
		}
	}
	history, err := s.store.ListSessionMessages(ctx, threadID, chatHistoryLimit)
	if err != nil {
		return nil, err
	}
	exec, err := s.newExecutor(ctx)
	if err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(s.newRunID())
	if runID == "" {
		return nil, errors.New("client run id is empty")
	}
	started := newRunStartSignal()
	req := domain.ExecuteRequest{
		RunID:          runID,
		SessionID:      threadID,
		TurnIndex:      message.TurnIndex,
		Input:          message.Content,
		BoundMessageID: message.ID,
		SkillID:        skillID,
		Messages:       buildChatMessages(history),
	}
	runCtx := context.WithoutCancel(ctx)
	go s.executeRun(runCtx, exec, req, started)

	select {
	case <-started.Started():
		return s.GetRun(ctx, runID)
	case err := <-started.Failed():
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func projectRun(record domain.RunRecord) (Run, error) {
	status, err := projectRunStatus(record.Status)
	if err != nil {
		return Run{}, err
	}
	run := Run{
		ID:        record.RunID,
		ThreadID:  record.SessionID,
		Status:    status,
		Mode:      "direct",
		CreatedAt: record.CreatedAt,
	}
	if record.Status != domain.RunStatusRunning {
		run.CompletedAt = record.UpdatedAt
	}
	return run, nil
}

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
func (s *ClientService) createUserMessage(ctx context.Context, threadID, content string) (*domain.SessionMessageRecord, error) {
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
			return nil, projectionError("message %d content_parts[%d]: %v", record.ID, index, err)
		}
	}
	return parts, nil
}

type clientRunStartSignal struct {
	started     chan struct{}
	failed      chan error
	closeOnce   sync.Once
	failureOnce sync.Once
	hasStarted  atomic.Bool
}

func newRunStartSignal() *clientRunStartSignal {
	return &clientRunStartSignal{
		started: make(chan struct{}),
		failed:  make(chan error, 1),
	}
}

func (s *clientRunStartSignal) Started() <-chan struct{} {
	return s.started
}

func (s *clientRunStartSignal) Failed() <-chan error {
	return s.failed
}

func (s *clientRunStartSignal) RunStarted() {
	s.hasStarted.Store(true)
	s.closeOnce.Do(func() { close(s.started) })
}

func (s *clientRunStartSignal) MarkFailed(err error) bool {
	if err == nil || s.hasStarted.Load() {
		return false
	}
	s.failureOnce.Do(func() { s.failed <- err })
	return true
}

func reportClientBackgroundError(ctx context.Context, runID string, err error) {
	slog.Default().ErrorContext(ctx, "client background run failure was not persisted", "run_id", runID, "error", err)
}

func (s *ClientService) executeRun(ctx context.Context, exec executorHandle, req domain.ExecuteRequest, started *clientRunStartSignal) {
	err := exec.ExecuteMessages(ctx, req, started)
	if err != nil {
		if started.MarkFailed(err) {
			return
		}
		if persistErr := s.recordStartedRunFailure(ctx, req.RunID, err); persistErr != nil {
			s.reportBackgroundRunFailure(ctx, req.RunID, err, persistErr)
		}
		return
	}
}

func (s *ClientService) reportBackgroundRunFailure(ctx context.Context, runID string, cause, persistErr error) {
	err := errors.Join(
		fmt.Errorf("client executor failed after run start: %w", cause),
		fmt.Errorf("record started client run failure: %w", persistErr),
	)
	report := reportClientBackgroundError
	if s != nil && s.reportError != nil {
		report = s.reportError
	}
	report(ctx, runID, err)
}

func (s *ClientService) recordStartedRunFailure(ctx context.Context, runID string, cause error) error {
	if s == nil || s.store == nil {
		return errors.New("client store is nil")
	}
	if strings.TrimSpace(runID) == "" || cause == nil {
		return nil
	}
	record, err := s.store.LoadRun(ctx, runID)
	if err != nil {
		return err
	}
	if record.Status != domain.RunStatusRunning {
		return nil
	}
	if err := s.store.FinishRunContext(ctx, runID, domain.RunStatusFailed, "", cause.Error()); err != nil {
		return fmt.Errorf("mark client run failed after background error: %w", err)
	}
	if _, err := s.store.AppendEventContext(ctx, runID, "run.failed", map[string]any{"error": cause.Error()}); err != nil {
		return fmt.Errorf("append client run failed event after background error: %w", err)
	}
	return nil
}

func (s *ClientService) LoadRunEventsAfter(ctx context.Context, runID string, afterSeq int64) (*clientevents.RunEventBatch, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("client store is nil")
	}
	if _, err := s.store.LoadRun(ctx, runID); err != nil {
		return nil, err
	}
	records, err := s.store.LoadEventsAfter(ctx, runID, afterSeq)
	if err != nil {
		return nil, fmt.Errorf("%w: load persisted run events: %v", ErrClientProjectionFailed, err)
	}
	events := make([]clientevents.RunEvent, 0, len(records))
	cursorSeq := afterSeq
	for _, record := range records {
		if record.Sequence > cursorSeq {
			cursorSeq = record.Sequence
		}
		if !clientevents.IsLiveRunEventKind(record.Kind) {
			continue
		}
		event, err := clientevents.ProjectRunEvent(record)
		if err != nil {
			return nil, fmt.Errorf("%w: project persisted run event: %v", ErrClientProjectionFailed, err)
		}
		events = append(events, event)
	}
	return &clientevents.RunEventBatch{
		Events:    events,
		CursorSeq: cursorSeq,
	}, nil
}

func (s *ClientService) LoadRunEventsForDetail(ctx context.Context, runID string) (*clientevents.RunEventDetail, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("client store is nil")
	}
	if _, err := s.store.LoadRun(ctx, runID); err != nil {
		return nil, err
	}
	records, err := s.store.LoadEvents(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("%w: load persisted run events: %v", ErrClientProjectionFailed, err)
	}
	events := make([]clientevents.RunEvent, 0, len(records))
	for _, record := range records {
		if !clientevents.IsLiveRunEventKind(record.Kind) {
			continue
		}
		event, err := clientevents.ProjectRunEvent(record)
		if err != nil {
			return nil, fmt.Errorf("%w: project persisted run event: %v", ErrClientProjectionFailed, err)
		}
		events = append(events, event)
	}
	return &clientevents.RunEventDetail{
		Events: events,
	}, nil
}

func (s *ClientService) EventPollInterval() time.Duration {
	if s == nil || s.eventPoll <= 0 {
		return 100 * time.Millisecond
	}
	return s.eventPoll
}
