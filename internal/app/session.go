package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/orchestrationmode"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/runtimehistory"
	"github.com/ycvk/acorn/internal/store"
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

type SessionListItem struct {
	Session         events.SessionRecord `json:"session"`
	LatestRunID     string               `json:"latest_run_id,omitempty"`
	LatestRunStatus events.RunStatus     `json:"latest_run_status,omitempty"`
	State           runtime.SessionState `json:"state,omitempty"`
	Resumable       bool                 `json:"resumable"`
	SummarySnippet  string               `json:"summary_snippet,omitempty"`
	SummaryStatus   string               `json:"summary_status,omitempty"`
	SummaryUpdated  *time.Time           `json:"summary_updated_at,omitempty"`
}

type SessionDetail struct {
	Session             events.SessionRecord           `json:"session"`
	LatestRunID         string                         `json:"latest_run_id,omitempty"`
	LatestRunStatus     events.RunStatus               `json:"latest_run_status,omitempty"`
	State               runtime.SessionState           `json:"state,omitempty"`
	Resumable           bool                           `json:"resumable"`
	ResumeReason        string                         `json:"resume_reason,omitempty"`
	MemoryContextBudget int                            `json:"memory_context_budget,omitempty"`
	TraceSummary        *runtime.TraceSummary          `json:"trace_summary,omitempty"`
	SelectedSkill       *runtime.SelectedSkill         `json:"selected_skill,omitempty"`
	LatestDecision      *decision.Record               `json:"latest_decision,omitempty"`
	InterruptIDs        []string                       `json:"interrupt_ids,omitempty"`
	SessionSummary      *runtimehistory.SessionSummary `json:"session_summary,omitempty"`
}

type SessionStateService struct {
	cfg   *config.Config
	store sessionStateStore
	trace *TraceService
}

func NewSessionStateService(cfg *config.Config, store sessionStateStore, trace *TraceService) *SessionStateService {
	return &SessionStateService{
		cfg:   cfg,
		store: store,
		trace: trace,
	}
}

func (s *SessionStateService) LoadSession(ctx context.Context, sessionID string) (*SessionDetail, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("session state store is nil")
	}

	session, err := s.store.LoadSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	latestRun, err := s.store.LoadLatestRunForSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	detail, err := buildSessionDetail(*session, latestRun, ctx, s.store, s.trace)
	if err != nil {
		return nil, err
	}
	detail.MemoryContextBudget = s.cfg.Memory.Search.MemoryContextTokenBudget
	summary, summaryErr := s.store.GetSessionSummary(ctx, sessionID)
	if summaryErr != nil {
		return nil, fmt.Errorf("load session summary for %s: %w", sessionID, summaryErr)
	}
	detail.SessionSummary = summary
	return &detail, nil
}

func buildSessionDetail(session events.SessionRecord, latestRun *events.RunRecord, ctx context.Context, store sessionStateStore, traceSvc *TraceService) (SessionDetail, error) {
	detail := SessionDetail{
		Session: session,
	}
	if latestRun != nil {
		detail.LatestRunID = latestRun.RunID
		detail.LatestRunStatus = latestRun.Status
	}

	detail.State = runtime.DeriveSessionState(latestRun, false)

	if latestRun == nil {
		return detail, nil
	}

	if latestRun.Status == events.RunStatusInterrupted {
		if traceSvc == nil {
			return SessionDetail{}, fmt.Errorf("load resume status for run %s: trace service is nil", latestRun.RunID)
		}
		resumeStatus, err := traceSvc.ResumeStatus(ctx, latestRun.RunID)
		if err != nil {
			return SessionDetail{}, fmt.Errorf("load resume status for run %s: %w", latestRun.RunID, err)
		} else if resumeStatus == nil {
			return SessionDetail{}, fmt.Errorf("load resume status for run %s: resume status is nil", latestRun.RunID)
		}
		detail.Resumable = resumeStatus.Resumable
		detail.ResumeReason = resumeStatus.Reason
		detail.InterruptIDs = resumeStatus.InterruptIDs
	}
	if detail.ResumeReason == "" {
		detail.ResumeReason = defaultResumeReason(latestRun)
	}

	if store == nil {
		return SessionDetail{}, fmt.Errorf("load events for run %s: session state store is nil", latestRun.RunID)
	}
	raw, loadErr := store.LoadEvents(ctx, latestRun.RunID)
	if loadErr == nil && len(raw) > 0 {
		detail.TraceSummary = runtime.BuildTraceSummary(raw)
		detail.SelectedSkill = runtime.SelectedSkillFromEvents(raw)
	} else if loadErr != nil {
		return SessionDetail{}, fmt.Errorf("load events for run %s: %w", latestRun.RunID, loadErr)
	}
	if decisionRecord, decisionErr := store.LoadRunDecision(ctx, latestRun.RunID); decisionErr == nil {
		detail.LatestDecision = decisionRecord
	} else {
		return SessionDetail{}, fmt.Errorf("load decision for run %s: %w", latestRun.RunID, decisionErr)
	}

	return detail, nil
}

func defaultResumeReason(run *events.RunRecord) string {
	if run == nil {
		return ""
	}
	switch run.Status {
	case events.RunStatusSucceeded:
		return fmt.Sprintf("run %s completed and does not need resume", run.RunID)
	case events.RunStatusFailed:
		return fmt.Sprintf("run %s failed; inspect run detail or start a new client run", run.RunID)
	case events.RunStatusRunning:
		return fmt.Sprintf("run %s is still running", run.RunID)
	case events.RunStatusInterrupted:
		return fmt.Sprintf("run %s is interrupted", run.RunID)
	default:
		return ""
	}
}

const chatHistoryLimit = 12

type ChatStore interface {
	PrepareChatTurn(ctx context.Context, sessionID, input, title string, historyLimit int) (int, []events.SessionMessageRecord, error)
	SyncAssistantMessageForRun(ctx context.Context, runID string) error
}

type ChatService struct {
	store       ChatStore
	newExecutor func(context.Context) (executorHandle, error)
}

func NewChatService(store ChatStore, newExecutor func(context.Context) (executorHandle, error)) *ChatService {
	return &ChatService{store: store, newExecutor: newExecutor}
}

func (s *ChatService) Send(ctx context.Context, sessionID, input, skillID string, sink runtime.StreamSink) (*runtime.Result, int, error) {
	if s == nil || s.store == nil {
		return nil, 0, errors.New("chat store is nil")
	}
	if s.newExecutor == nil {
		return nil, 0, errors.New("chat executor factory is nil")
	}
	turnIndex, history, err := s.store.PrepareChatTurn(ctx, sessionID, input, deriveSessionTitle(input), chatHistoryLimit)
	if err != nil {
		return nil, 0, err
	}
	exec, err := s.newExecutor(ctx)
	if err != nil {
		return nil, 0, err
	}
	result, err := exec.ExecuteMessages(ctx, runtime.ExecuteRequest{
		SessionID: sessionID,
		TurnIndex: turnIndex,
		Input:     input,
		SkillID:   skillID,
		Messages:  buildChatMessages(history),
	}, sink)
	if err != nil {
		return nil, 0, err
	}
	if err := s.store.SyncAssistantMessageForRun(ctx, result.RunID); err != nil {
		return nil, 0, err
	}
	return result, turnIndex, nil
}

func buildChatMessages(items []events.SessionMessageRecord) []adk.Message {
	messages := make([]adk.Message, 0, len(items))
	for _, item := range items {
		switch item.Role {
		case "user":
			messages = append(messages, schema.UserMessage(item.Content))
		case "assistant":
			messages = append(messages, schema.AssistantMessage(item.Content, nil))
		}
	}
	return messages
}

func deriveSessionTitle(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= 48 {
		return trimmed
	}
	return string(runes[:48]) + "..."
}

var (
	ErrClientProjectionFailed = errors.New("client projection failed")
	ErrClientNoPendingMessage = errors.New("client pending user message not found")
	ErrClientInvalidRunMode   = errors.New("client run mode is invalid")
)

// ClientService owns the client-facing thread/message/run/event orchestration.
// It deliberately stays separate from ChatService because /v1 splits message
// creation, run creation, and event replay into distinct HTTP calls.
type ClientService struct {
	store         clientStore
	newExecutor   func(context.Context) (executorHandle, error)
	workspaceRoot string
	eventPoll     time.Duration
	newThreadID   func() string
	newRunID      func() string
}

func BuildClientService(store clientStore, newExecutor func(context.Context) (executorHandle, error), workspaceRoot string) *ClientService {
	return &ClientService{
		store:         store,
		newExecutor:   newExecutor,
		workspaceRoot: workspaceRoot,
		eventPoll:     100 * time.Millisecond,
		newThreadID:   newThreadID,
		newRunID:      newRunID,
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

func (s *ClientService) LoadRunEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]RunEvent, error) {
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
	events := make([]RunEvent, 0, len(records))
	for _, record := range records {
		event, err := projectRunEvent(record)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func (s *ClientService) LoadRunEventsForDetail(ctx context.Context, runID string) (*RunEventDetail, error) {
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
	events := make([]RunEvent, 0, len(records))
	unsupported := make([]UnsupportedRunEvent, 0)
	for _, record := range records {
		event, err := projectRunEvent(record)
		if err != nil {
			unsupported = append(unsupported, projectUnsupportedRunEvent(record))
			continue
		}
		events = append(events, event)
	}
	return &RunEventDetail{
		Events:      events,
		Unsupported: unsupported,
		Trace:       runtime.BuildTraceSummary(records),
	}, nil
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
	case events.RunStatusRunning:
		return false, nil
	case events.RunStatusSucceeded, events.RunStatusInterrupted, events.RunStatusFailed:
		return true, nil
	default:
		return false, projectionError("unknown run status %q", record.Status)
	}
}

func (s *ClientService) EventPollInterval() time.Duration {
	if s == nil || s.eventPoll <= 0 {
		return 100 * time.Millisecond
	}
	return s.eventPoll
}

func (s *ClientService) CreateRun(ctx context.Context, threadID, skillID, mode string) (*Run, error) {
	if s == nil || s.store == nil || s.newExecutor == nil || s.newRunID == nil {
		return nil, errors.New("client service is not initialized")
	}
	threadID = strings.TrimSpace(threadID)
	skillID = strings.TrimSpace(skillID)
	orchestrationMode, err := parseClientRunMode(mode)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.LoadSession(ctx, threadID); err != nil {
		return nil, err
	}
	message, err := s.store.LoadLatestUnboundUserMessage(ctx, threadID)
	if err != nil {
		if errors.Is(err, store.ErrSessionMessageNotFound) {
			return nil, fmt.Errorf("%w: thread %s", ErrClientNoPendingMessage, threadID)
		}
		return nil, err
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
	req := runtime.ExecuteRequest{
		RunID:             runID,
		SessionID:         threadID,
		TurnIndex:         message.TurnIndex,
		Input:             message.Content,
		SkillID:           skillID,
		Messages:          buildChatMessages(history),
		OrchestrationMode: orchestrationMode,
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

func parseClientRunMode(raw string) (orchestrationmode.Mode, error) {
	mode := orchestrationmode.Mode(strings.TrimSpace(raw))
	switch mode {
	case "":
		return "", nil
	case orchestrationmode.DirectResponse, orchestrationmode.PlanExecute:
		return mode, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrClientInvalidRunMode, raw)
	}
}

func (s *ClientService) executeRun(ctx context.Context, exec executorHandle, req runtime.ExecuteRequest, started *clientRunStartSignal) {
	result, err := exec.ExecuteMessages(ctx, req, started.Sink)
	if err != nil {
		if started.MarkFailed(err) {
			return
		}
		if persistErr := s.recordStartedRunFailure(ctx, req.RunID, err); persistErr != nil {
			panic(persistErr)
		}
		return
	}
	if result == nil {
		err := errors.New("client executor returned nil result")
		if started.MarkFailed(err) {
			return
		}
		if persistErr := s.recordStartedRunFailure(ctx, req.RunID, err); persistErr != nil {
			panic(persistErr)
		}
		return
	}
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
	if record.Status != events.RunStatusRunning {
		return nil
	}
	if err := s.store.FinishRunContext(ctx, runID, events.RunStatusFailed, "", cause.Error()); err != nil {
		return fmt.Errorf("mark client run failed after background error: %w", err)
	}
	if _, err := s.store.AppendEventContext(ctx, runID, "run.failed", map[string]any{"error": cause.Error()}); err != nil {
		return fmt.Errorf("append client run failed event after background error: %w", err)
	}
	return nil
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
	trimmed := strings.TrimSpace(content)
	record, err := s.store.AppendSessionMessage(threadID, turnIndex, "user", trimmed, "")
	if err != nil {
		return nil, err
	}
	if err := s.store.UpdateSessionTitleIfEmpty(ctx, threadID, generatedThreadTitle(trimmed)); err != nil {
		return nil, err
	}
	message, err := projectMessage(*record)
	if err != nil {
		return nil, err
	}
	return &message, nil
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

func (s *ClientService) projectThread(record events.SessionRecord, latestRun *events.RunRecord) (Thread, error) {
	thread := Thread{
		ID:            record.SessionID,
		Title:         record.Title,
		WorkspaceRoot: s.workspaceRoot,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
		State:         string(runtime.SessionStateNew),
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

func projectMessage(record events.SessionMessageRecord) (Message, error) {
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

func projectMessageParts(record events.SessionMessageRecord) ([]MessagePart, error) {
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
		case "", string(events.PendingActionStatusPending), string(events.PendingActionStatusApproved), string(events.PendingActionStatusRejected), string(events.PendingActionStatusResolved):
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

func projectRun(record events.RunRecord) (Run, error) {
	status, err := projectRunStatus(record.Status)
	if err != nil {
		return Run{}, err
	}
	mode, err := projectRunMode(record.OrchestrationMode)
	if err != nil {
		return Run{}, err
	}
	run := Run{
		ID:        record.RunID,
		ThreadID:  record.SessionID,
		Status:    status,
		Mode:      mode,
		CreatedAt: record.CreatedAt,
	}
	if record.Status != events.RunStatusRunning {
		run.CompletedAt = record.UpdatedAt
	}
	return run, nil
}

func projectRunEvent(record events.EventRecord) (RunEvent, error) {
	payload, ok := record.Payload.(map[string]any)
	if !ok {
		return RunEvent{}, projectionError("run event payload must be object: run_id=%s sequence=%d kind=%s", record.RunID, record.Sequence, record.Kind)
	}
	data, err := projectRunEventData(record.Kind, payload)
	if err != nil {
		return RunEvent{}, err
	}
	return RunEvent{
		EventID: fmt.Sprintf("%s:%d", record.RunID, record.Sequence),
		RunID:   record.RunID,
		Seq:     record.Sequence,
		TS:      record.CreatedAt,
		Type:    record.Kind,
		Data:    data,
	}, nil
}

func projectUnsupportedRunEvent(record events.EventRecord) UnsupportedRunEvent {
	payload, ok := record.Payload.(map[string]any)
	if !ok {
		return UnsupportedRunEvent{
			EventID: fmt.Sprintf("%s:%d", record.RunID, record.Sequence),
			RunID:   record.RunID,
			Seq:     record.Sequence,
			TS:      record.CreatedAt,
			Type:    record.Kind,
			Raw:     nil,
			Reason:  fmt.Sprintf("payload for %q is not an object", record.Kind),
		}
	}
	raw := cloneMap(payload)
	if _, err := projectRunEventData(record.Kind, payload); err != nil {
		return UnsupportedRunEvent{
			EventID: fmt.Sprintf("%s:%d", record.RunID, record.Sequence),
			RunID:   record.RunID,
			Seq:     record.Sequence,
			TS:      record.CreatedAt,
			Type:    record.Kind,
			Raw:     raw,
			Reason:  err.Error(),
		}
	}
	return UnsupportedRunEvent{
		EventID: fmt.Sprintf("%s:%d", record.RunID, record.Sequence),
		RunID:   record.RunID,
		Seq:     record.Sequence,
		TS:      record.CreatedAt,
		Type:    record.Kind,
		Raw:     raw,
		Reason:  fmt.Sprintf("event %q is not part of the client live contract", record.Kind),
	}
}

func projectRunEventData(kind string, payload map[string]any) (any, error) {
	switch kind {
	case "run.started":
		return RunStartedData{Input: topLevelString(payload, "input")}, nil
	case "assistant.delta":
		value, ok := objectField(payload, "assistant_delta")
		if !ok {
			return nil, projectionError("assistant.delta payload missing assistant_delta object")
		}
		return AssistantDeltaData{AssistantDelta: value}, nil
	case "agent.message":
		value, ok := objectField(payload, "message")
		if !ok {
			return nil, projectionError("agent.message payload missing message object")
		}
		return AgentMessageData{Message: value}, nil
	case "tool.call.started":
		return ToolCallStartedData{ToolCall: projectToolCallPayload(kind, payload)}, nil
	case "tool.call.progress":
		return ToolCallProgressData{ToolCall: projectToolCallPayload(kind, payload)}, nil
	case "tool.call.succeeded":
		return ToolCallSucceededData{ToolCall: projectToolCallPayload(kind, payload)}, nil
	case "tool.call.failed":
		return ToolCallFailedData{ToolCall: projectToolCallPayload(kind, payload)}, nil
	case "tool.call.interrupted":
		return ToolCallInterruptedData{ToolCall: projectToolCallPayload(kind, payload)}, nil
	case "run.completed":
		value, _ := objectField(payload, "message")
		return RunCompletedData{Message: value}, nil
	case "run.failed":
		return RunFailedData{Error: topLevelString(payload, "error")}, nil
	case "run.interrupted":
		value, _ := objectField(payload, "interrupt")
		return RunInterruptedData{Interrupt: value}, nil
	case "run.resume_requested":
		value, _ := objectField(payload, "targets")
		return RunResumeRequestedData{Targets: value}, nil
	case "elicitation.pending":
		return ElicitationPendingData{
			ActionID:        topLevelString(payload, "action_id"),
			Message:         topLevelString(payload, "message"),
			RequestedSchema: payload["requested_schema"],
		}, nil
	case "elicitation.decided":
		return ElicitationDecidedData{
			ActionID:        topLevelString(payload, "action_id"),
			Message:         topLevelString(payload, "message"),
			RequestedSchema: payload["requested_schema"],
		}, nil
	case "operator_question.pending":
		return projectOperatorQuestionData(payload), nil
	case "operator_question.decided":
		return projectOperatorQuestionData(payload), nil
	case "decision_selected":
		return projectDecisionSelectedData(payload), nil
	case "decision_blocked":
		data := projectDecisionSelectedData(payload)
		return DecisionBlockedData(data), nil
	case "skill.discovered", "skill.selected", "skill.loaded", "skill.failed":
		value, _ := objectField(payload, "skill")
		return SkillData{Skill: value}, nil
	case "skill.lifecycle":
		value, ok := objectField(payload, "skill_lifecycle")
		if !ok {
			return nil, projectionError("skill.lifecycle payload missing skill_lifecycle object")
		}
		return SkillLifecycleData{SkillLifecycle: value}, nil
	case "procedure.activation":
		value, ok := objectField(payload, "procedure_activation")
		if !ok {
			return nil, projectionError("procedure.activation payload missing procedure_activation object")
		}
		return ProcedureActivationData{ProcedureActivation: value}, nil
	case "memory.prepared":
		value, ok := objectField(payload, "memory_prepared")
		if !ok {
			return nil, projectionError("memory.prepared payload missing memory_prepared object")
		}
		return MemoryPreparedData{MemoryPrepared: value}, nil
	case "context.pressure":
		value, ok := objectField(payload, "context_pressure")
		if !ok {
			return nil, projectionError("context.pressure payload missing context_pressure object")
		}
		return ContextPressureData{ContextPressure: value}, nil
	case "context.compressed":
		value, ok := objectField(payload, "context_compressed")
		if !ok {
			return nil, projectionError("context.compressed payload missing context_compressed object")
		}
		return ContextCompressedData{ContextCompressed: value}, nil
	case "crystallization.failed":
		return CrystallizationFailedData{
			RunID: topLevelString(payload, "run_id"),
			Error: topLevelString(payload, "error"),
		}, nil
	case "crystallization.verdict":
		return CrystallizationVerdictData{
			RunID:     topLevelString(payload, "run_id"),
			Verdict:   topLevelString(payload, "verdict"),
			SkillID:   topLevelString(payload, "skill_id"),
			Reason:    topLevelString(payload, "reason"),
			SimilarTo: topLevelString(payload, "similar_to"),
		}, nil
	case "plan.created", "plan.updated":
		value, _ := objectField(payload, "plan")
		return PlanData{Plan: value}, nil
	case "plan.cleared":
		return PlanClearedData{PlanID: topLevelString(payload, "plan_id")}, nil
	case "step.started", "step.completed", "step.failed":
		plan, _ := objectField(payload, "plan")
		step, _ := objectField(payload, "step")
		return PlanStepData{
			PlanID:    topLevelString(payload, "plan_id"),
			SessionID: topLevelString(payload, "session_id"),
			Plan:      plan,
			Step:      step,
			UpdatedAt: topLevelString(payload, "updated_at"),
			Error:     topLevelString(payload, "error"),
		}, nil
	case "subagent.started", "subagent.completed", "subagent.failed":
		return SubagentData{
			SubRunID:          topLevelString(payload, "sub_run_id"),
			ParentID:          topLevelString(payload, "parent_id"),
			SessionID:         topLevelString(payload, "session_id"),
			Depth:             topLevelInt(payload, "depth"),
			Task:              topLevelString(payload, "task"),
			Summary:           topLevelString(payload, "summary"),
			FinalStatus:       topLevelString(payload, "final_status"),
			AcceptanceStatus:  topLevelString(payload, "acceptance_status"),
			AcceptanceReasons: stringArrayField(payload, "acceptance_reasons"),
			OrchestrationMode: topLevelString(payload, "orchestration_mode"),
			ParentStepID:      topLevelString(payload, "parent_step_id"),
			Error:             topLevelString(payload, "error"),
		}, nil
	default:
		return nil, projectionError("unsupported live run event kind %q", kind)
	}
}

func projectOperatorQuestionData(payload map[string]any) OperatorQuestionData {
	return OperatorQuestionData{
		ActionID:         topLevelString(payload, "action_id"),
		Question:         topLevelString(payload, "question"),
		Options:          pendingActionOptionsFromAny(payload["options"]),
		AllowFreeform:    topLevelBool(payload, "allow_freeform"),
		Decision:         topLevelString(payload, "decision"),
		SelectedOptionID: topLevelString(payload, "selected_option_id"),
		Answer:           topLevelString(payload, "answer"),
	}
}

func projectDecisionSelectedData(payload map[string]any) DecisionSelectedData {
	return DecisionSelectedData{
		Action:              topLevelString(payload, "action"),
		Intent:              topLevelString(payload, "intent"),
		SelectedSkillID:     topLevelString(payload, "selected_skill_id"),
		DecisionReason:      topLevelString(payload, "decision_reason"),
		DecisionProfileHash: topLevelString(payload, "decision_profile_hash"),
		ExplicitSkillID:     topLevelString(payload, "explicit_skill_id"),
	}
}

func pendingActionOptionsFromAny(raw any) []events.PendingActionOption {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]events.PendingActionOption, 0, len(items))
	for _, item := range items {
		option, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, events.PendingActionOption{
			ID:          topLevelString(option, "id"),
			Label:       topLevelString(option, "label"),
			Description: topLevelString(option, "description"),
		})
	}
	return out
}

func topLevelBool(payload map[string]any, key string) bool {
	value, ok := payload[key].(bool)
	return ok && value
}

func projectToolCallPayload(kind string, payload map[string]any) map[string]any {
	if value, ok := objectField(payload, "tool_call"); ok {
		return value
	}
	out := cloneMap(payload)
	if _, hasName := out["name"]; !hasName {
		if toolName := topLevelString(payload, "tool_name"); toolName != "" {
			out["name"] = toolName
		}
	}
	return out
}

func projectThreadState(status events.RunStatus) (string, error) {
	switch status {
	case events.RunStatusRunning:
		return string(runtime.SessionStateRunning), nil
	case events.RunStatusSucceeded:
		return string(runtime.SessionStateCompleted), nil
	case events.RunStatusInterrupted:
		return string(runtime.SessionStateInterrupted), nil
	case events.RunStatusFailed:
		return string(runtime.SessionStateFailed), nil
	default:
		return "", projectionError("unknown run status %q", status)
	}
}

func projectRunStatus(status events.RunStatus) (string, error) {
	switch status {
	case events.RunStatusRunning:
		return "running", nil
	case events.RunStatusSucceeded:
		return "completed", nil
	case events.RunStatusInterrupted:
		return "interrupted", nil
	case events.RunStatusFailed:
		return "failed", nil
	default:
		return "", projectionError("unknown run status %q", status)
	}
}

func projectRunMode(mode orchestrationmode.Mode) (string, error) {
	switch mode {
	case orchestrationmode.DirectResponse:
		return "direct", nil
	case orchestrationmode.SingleAgent:
		return "agent", nil
	case orchestrationmode.PlanExecute:
		return "plan_execute", nil
	default:
		return "", projectionError("unknown run mode %q", mode)
	}
}

func topLevelString(payload map[string]any, key string) string {
	value, ok := payload[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func topLevelInt(payload map[string]any, key string) int {
	switch value := payload[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func objectField(payload map[string]any, key string) (map[string]any, bool) {
	value, ok := payload[key].(map[string]any)
	if !ok {
		return nil, false
	}
	return cloneMap(value), true
}

func stringArrayField(payload map[string]any, key string) []string {
	values, ok := payload[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		item, ok := value.(string)
		if ok {
			out = append(out, strings.TrimSpace(item))
		}
	}
	return out
}

func cloneMap(payload map[string]any) map[string]any {
	out := make(map[string]any, len(payload))
	for key, value := range payload {
		out[key] = value
	}
	return out
}

func projectionError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrClientProjectionFailed, fmt.Sprintf(format, args...))
}

type Thread struct {
	ID            string
	Title         string
	WorkspaceRoot string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	LatestRunID   string
	State         string
}

type MessageContent struct {
	Type  string
	Text  string
	Parts []MessagePart
}

type Message struct {
	ID        string
	ThreadID  string
	Role      string
	Content   MessageContent
	CreatedAt time.Time
	RunID     string
}

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

type DisclosureItem struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Detail  string `json:"detail,omitempty"`
	Tone    string `json:"tone"`
	SkillID string `json:"skill_id,omitempty"`
}

type DecisionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type MessageAction struct {
	Kind  string `json:"kind"`
	RunID string `json:"run_id"`
	Label string `json:"label"`
}

type Run struct {
	ID          string
	ThreadID    string
	Status      string
	Mode        string
	CreatedAt   time.Time
	CompletedAt time.Time
}

type RunStartedData struct {
	Input string `json:"input,omitempty"`
}

type AssistantDeltaData struct {
	AssistantDelta map[string]any `json:"assistant_delta"`
}

type AgentMessageData struct {
	Message map[string]any `json:"message"`
}

type ToolCallStartedData struct {
	ToolCall map[string]any `json:"tool_call"`
}

type ToolCallProgressData struct {
	ToolCall map[string]any `json:"tool_call"`
}

type ToolCallSucceededData struct {
	ToolCall map[string]any `json:"tool_call"`
}

type ToolCallFailedData struct {
	ToolCall map[string]any `json:"tool_call"`
}

type ToolCallInterruptedData struct {
	ToolCall map[string]any `json:"tool_call"`
}

type RunCompletedData struct {
	Message map[string]any `json:"message,omitempty"`
}

type RunFailedData struct {
	Error string `json:"error,omitempty"`
}

type RunInterruptedData struct {
	Interrupt map[string]any `json:"interrupt,omitempty"`
}

type RunResumeRequestedData struct {
	Targets map[string]any `json:"targets,omitempty"`
}

type ElicitationPendingData struct {
	ActionID        string `json:"action_id"`
	Message         string `json:"message,omitempty"`
	RequestedSchema any    `json:"requested_schema,omitempty"`
}

type ElicitationDecidedData = ElicitationPendingData

type OperatorQuestionData struct {
	ActionID         string                       `json:"action_id"`
	Question         string                       `json:"question,omitempty"`
	Options          []events.PendingActionOption `json:"options,omitempty"`
	AllowFreeform    bool                         `json:"allow_freeform,omitempty"`
	Decision         string                       `json:"decision,omitempty"`
	SelectedOptionID string                       `json:"selected_option_id,omitempty"`
	Answer           string                       `json:"answer,omitempty"`
}

type OperatorQuestionPendingData = OperatorQuestionData
type OperatorQuestionDecidedData = OperatorQuestionData

type DecisionSelectedData struct {
	Action              string `json:"action,omitempty"`
	Intent              string `json:"intent,omitempty"`
	SelectedSkillID     string `json:"selected_skill_id,omitempty"`
	DecisionReason      string `json:"decision_reason,omitempty"`
	DecisionProfileHash string `json:"decision_profile_hash,omitempty"`
	ExplicitSkillID     string `json:"explicit_skill_id,omitempty"`
}

type DecisionBlockedData = DecisionSelectedData

type SkillData struct {
	Skill map[string]any `json:"skill,omitempty"`
}

type SkillLifecycleData struct {
	SkillLifecycle map[string]any `json:"skill_lifecycle"`
}

type ProcedureActivationData struct {
	ProcedureActivation map[string]any `json:"procedure_activation"`
}

type MemoryPreparedData struct {
	MemoryPrepared map[string]any `json:"memory_prepared"`
}

type ContextPressureData struct {
	ContextPressure map[string]any `json:"context_pressure"`
}

type CrystallizationVerdictData struct {
	RunID     string `json:"run_id"`
	Verdict   string `json:"verdict"`
	SkillID   string `json:"skill_id,omitempty"`
	Reason    string `json:"reason,omitempty"`
	SimilarTo string `json:"similar_to,omitempty"`
}

type CrystallizationFailedData struct {
	RunID string `json:"run_id"`
	Error string `json:"error,omitempty"`
}

type ContextCompressedData struct {
	ContextCompressed map[string]any `json:"context_compressed"`
}

type PlanData struct {
	Plan map[string]any `json:"plan,omitempty"`
}

type PlanClearedData struct {
	PlanID string `json:"plan_id,omitempty"`
}

type PlanStepData struct {
	PlanID    string         `json:"plan_id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Plan      map[string]any `json:"plan,omitempty"`
	Step      map[string]any `json:"step,omitempty"`
	UpdatedAt string         `json:"updated_at,omitempty"`
	Error     string         `json:"error,omitempty"`
}

type SubagentData struct {
	SubRunID          string   `json:"sub_run_id,omitempty"`
	ParentID          string   `json:"parent_id,omitempty"`
	SessionID         string   `json:"session_id,omitempty"`
	Depth             int      `json:"depth,omitempty"`
	Task              string   `json:"task,omitempty"`
	Summary           string   `json:"summary,omitempty"`
	FinalStatus       string   `json:"final_status,omitempty"`
	AcceptanceStatus  string   `json:"acceptance_status,omitempty"`
	AcceptanceReasons []string `json:"acceptance_reasons,omitempty"`
	OrchestrationMode string   `json:"orchestration_mode,omitempty"`
	ParentStepID      string   `json:"parent_step_id,omitempty"`
	Error             string   `json:"error,omitempty"`
}

type RunEvent struct {
	EventID string
	RunID   string
	Seq     int64
	TS      time.Time
	Type    string
	Data    any
}

type UnsupportedRunEvent struct {
	EventID string
	RunID   string
	Seq     int64
	TS      time.Time
	Type    string
	Raw     map[string]any
	Reason  string
}

type RunEventDetail struct {
	Events      []RunEvent
	Unsupported []UnsupportedRunEvent
	Trace       *runtime.TraceSummary
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

func (s *clientRunStartSignal) Sink(item runtime.StreamItem) error {
	if item.Kind == runtime.StreamKindRunStarted {
		s.hasStarted.Store(true)
		s.closeOnce.Do(func() { close(s.started) })
	}
	return nil
}

func (s *clientRunStartSignal) MarkFailed(err error) bool {
	if err == nil || s.hasStarted.Load() {
		return false
	}
	s.failureOnce.Do(func() { s.failed <- err })
	return true
}
