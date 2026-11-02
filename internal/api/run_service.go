package api

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

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/views"
)

var ErrClientProjectionFailed = errors.New("client projection failed")

// projectionError wraps a format string into ErrClientProjectionFailed.
func projectionError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrClientProjectionFailed, fmt.Sprintf(format, args...))
}

func projectRunStatus(status core.RunStatus) (string, error) {
	switch status {
	case core.RunStatusRunning:
		return "running", nil
	case core.RunStatusSucceeded:
		return "completed", nil
	case core.RunStatusInterrupted:
		return "interrupted", nil
	case core.RunStatusFailed:
		return "failed", nil
	default:
		return "", projectionError("unknown run status %q", status)
	}
}

// Run is a user-facing run DTO.
type Run struct {
	ID          string
	ThreadID    string
	Status      string
	CreatedAt   time.Time
	CompletedAt time.Time
}

// RunService owns run lifecycle: sending the owner's messages, creating runs
// against a thread, probing run terminality, and interrupting in-flight runs.
// It drives the executor, and executes the run the store starts on owner
// messages that waited for a run to end.
type RunService struct {
	store      core.SessionStore
	threads    *ThreadService
	executeRun func(context.Context, core.ExecuteRequest, core.StreamSink) (*runtime.Result, error)
	controller *runtime.RunController
	resumer    runResumer
}

// NewRunService constructs a RunService backed by the given store, executor
// factory, and run controller.
func NewRunService(store core.SessionStore, threads *ThreadService, executeRun func(context.Context, core.ExecuteRequest, core.StreamSink) (*runtime.Result, error), controller *runtime.RunController) *RunService {
	return &RunService{
		store:      store,
		threads:    threads,
		executeRun: executeRun,
		controller: controller,
	}
}

// WithResumer makes runs that settle as interrupted resume as soon as their
// pending actions are already decided.
func (s *RunService) WithResumer(resumer runResumer) *RunService {
	s.resumer = resumer
	return s
}

func (s *RunService) GetRun(ctx context.Context, runID string) (*Run, error) {
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

// RunIsTerminal reports whether a run produces no further events. An
// interrupted run is waiting for the owner's decision and the server resumes
// it afterwards, so it is not terminal.
func (s *RunService) RunIsTerminal(ctx context.Context, runID string) (bool, error) {
	record, err := s.store.LoadRun(ctx, runID)
	if err != nil {
		return false, err
	}
	switch record.Status {
	case core.RunStatusRunning, core.RunStatusInterrupted:
		return false, nil
	case core.RunStatusSucceeded, core.RunStatusFailed:
		return true, nil
	default:
		return false, projectionError("unknown run status %q", record.Status)
	}
}

func (s *RunService) InterruptRun(ctx context.Context, runID string) error {
	return s.controller.Interrupt(runID)
}

// SentMessage is the run that takes an owner message: the run started on it,
// or the thread's run still going, which the message waits for (Queued).
type SentMessage struct {
	Run    Run
	Queued *QueuedMessage
}

// SendMessage sends the owner's message in the thread. While a run of the
// thread runs or waits for the owner, the message waits for it: the run takes
// it before its next model call, or the run that starts when it ends does.
// Otherwise a run starts on it, after the messages still waiting there.
// fileIDs are files uploaded to the thread that the message sends.
func (s *RunService) SendMessage(ctx context.Context, threadID, input string, fileIDs []string) (*SentMessage, error) {
	threadID = strings.TrimSpace(threadID)
	input = strings.TrimSpace(input)
	if len(fileIDs) > 0 {
		if err := s.threads.unsentFiles(ctx, threadID, fileIDs); err != nil {
			return nil, err
		}
	}
	sent, err := s.store.SendOwnerMessage(ctx, core.OwnerMessage{SessionID: threadID, Content: input, FileIDs: fileIDs, RunID: core.NewRunID()})
	if err != nil {
		return nil, err
	}
	if err := s.store.UpdateSessionTitleIfEmpty(ctx, threadID, generatedThreadTitle(input)); err != nil {
		return nil, err
	}
	if sent.Started != nil {
		run, err := s.launch(ctx, createdRunRequest(*sent.Started))
		if err != nil {
			return nil, err
		}
		return &SentMessage{Run: *run}, nil
	}
	run, err := s.GetRun(ctx, sent.RunID)
	if err != nil {
		return nil, err
	}
	files, err := s.threads.files.ThreadFiles(ctx, threadID)
	if err != nil {
		return nil, err
	}
	queued := queuedMessage(*sent.Queued, files)
	return &SentMessage{Run: *run, Queued: &queued}, nil
}

// ContinueRun executes in the background a run the store started on owner
// messages that waited in its thread.
func (s *RunService) ContinueRun(ctx context.Context, req core.ExecuteRequest) {
	go s.executeRunAsync(context.WithoutCancel(ctx), req, newRunStartSignal())
}

// StartQueuedRuns starts a run in every thread whose owner messages wait
// with none of its runs going, as after a restart or after a run that
// failed before it ran. serve sweeps it with the decided runs.
func (s *RunService) StartQueuedRuns(ctx context.Context) error {
	threads, err := s.store.ListQueuedSessions(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, threadID := range threads {
		start, err := s.store.StartQueuedRun(ctx, threadID, core.NewRunID())
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if start != nil {
			s.ContinueRun(ctx, createdRunRequest(*start))
		}
	}
	return errors.Join(errs...)
}

func createdRunRequest(start core.RunStart) core.ExecuteRequest {
	return core.ExecuteRequest{RunID: start.RunID, SessionID: start.SessionID, TurnIndex: start.TurnIndex, Input: start.Input, Created: true}
}

// CreateScheduledRun starts a scheduler input with explicit provenance and budget ownership.
func (s *RunService) CreateScheduledRun(ctx context.Context, threadID, input string, wake core.ScheduledWake) (*Run, error) {
	if strings.TrimSpace(wake.Reason) == "" {
		return nil, errors.New("scheduled run requires wake reason")
	}
	return s.createRun(ctx, threadID, input, wake.Reason, core.MessageRoleWake, &wake)
}

// captureWake describes what wakes a capture run.
const captureWake = "owner shared something"

// CreateCaptureRun starts a run in threadID on something the owner shared.
// Captures are the owner's doing and do not count as autonomous wakes.
func (s *RunService) CreateCaptureRun(ctx context.Context, threadID, input string) (*Run, error) {
	if strings.TrimSpace(input) == "" {
		return nil, errors.New("capture run requires input")
	}
	return s.createRun(ctx, threadID, input, captureWake, core.MessageRoleCapture, nil)
}

// createRun records input under role and starts the run on it. wake tells the
// agent what woke it.
func (s *RunService) createRun(ctx context.Context, threadID, input, wake, role string, scheduled *core.ScheduledWake) (*Run, error) {
	threadID = strings.TrimSpace(threadID)
	if strings.TrimSpace(input) == "" {
		return nil, fmt.Errorf("%s run in %s requires input", role, threadID)
	}
	message, err := s.threads.createInputMessage(ctx, threadID, role, input)
	if err != nil {
		return nil, err
	}
	return s.startRun(ctx, threadID, message, wake, scheduled)
}

// SendViewAction sends what the owner pressed or chose in a view of the
// thread as their message and starts a run on it. The action is resolved
// against the stored view, and each action of a view is sent once.
func (s *RunService) SendViewAction(ctx context.Context, threadID, viewID string, req views.ActionRequest) (*ViewActionResult, error) {
	view, err := s.threads.views.ThreadView(ctx, threadID, viewID)
	if err != nil {
		return nil, err
	}
	action, err := views.ResolveAction(view.Components, view.Data, req)
	if err != nil {
		return nil, err
	}
	carried, err := json.Marshal(action.Context)
	if err != nil {
		return nil, fmt.Errorf("view action context: %w", err)
	}
	write := core.ViewActionWrite{ViewID: view.ID, SessionID: threadID, Key: action.Key, ComponentID: action.ComponentID, Scope: action.Scope, Name: action.Name, Text: action.Text, Context: carried}
	for _, option := range action.Selected {
		write.Selected = append(write.Selected, core.ViewOption{ID: option.ID, Label: option.Label})
	}
	message, _, err := s.threads.views.SendViewAction(ctx, write)
	if err != nil {
		return nil, err
	}
	run, err := s.startRun(ctx, threadID, &message, "", nil)
	if err != nil {
		return nil, err
	}
	sent, err := s.threads.views.ThreadView(ctx, threadID, viewID)
	if err != nil {
		return nil, err
	}
	return &ViewActionResult{Run: *run, Text: message.Content, View: core.ViewDataOf(sent)}, nil
}

// ViewActionResult is the run a view action started, the owner's message it
// sent, and the view with the action marked as sent.
type ViewActionResult struct {
	Run  Run
	Text string
	View core.ViewData
}

// SendStoryMessage sends the owner's ask about a story on the briefing card
// as their message in the story's conversation, created the first time, and
// starts a run on it. The message and the conversation use the title the
// newest briefing gave the story, as the owner saw it on the card, else the
// story's own. The model sees the story as it stands now beside the message.
func (s *RunService) SendStoryMessage(ctx context.Context, storyID int64, intent core.StoryIntent) (*StoryMessageResult, error) {
	story, err := s.threads.stories.NewsStory(ctx, storyID)
	if err != nil {
		return nil, err
	}
	title, err := s.threads.stories.BriefingStoryTitle(ctx, storyID)
	if err != nil {
		return nil, err
	}
	if title == "" {
		title = story.Title
	}
	message, err := s.threads.stories.SendStoryMessage(ctx, core.StoryMessageWrite{
		Intent: intent, Text: storyMessageText(intent, title), Title: title, Story: core.SnapshotOf(story), NewThreadID: core.NewSessionID(),
	})
	if err != nil {
		return nil, err
	}
	run, err := s.startRun(ctx, message.SessionID, &message, "", nil)
	if err != nil {
		return nil, err
	}
	return &StoryMessageResult{ThreadID: message.SessionID, Run: *run, Text: message.Content}, nil
}

// StoryMessageResult is the story's conversation, the run the owner's
// message started and the message's text.
type StoryMessageResult struct {
	ThreadID string
	Run      Run
	Text     string
}

// storyMessageText is the owner's message for an ask about a story, as the
// owner sees it in the conversation.
func storyMessageText(intent core.StoryIntent, title string) string {
	if intent == core.StoryFollow {
		return "持续跟进：" + title
	}
	return "展开讲讲：" + title
}

// startRun starts a run bound to message.
func (s *RunService) startRun(ctx context.Context, threadID string, message *core.SessionMessageRecord, wake string, scheduled *core.ScheduledWake) (*Run, error) {
	req := core.ExecuteRequest{
		RunID:          core.NewRunID(),
		SessionID:      threadID,
		TurnIndex:      message.TurnIndex,
		Input:          message.Content,
		BoundMessageID: message.ID,
		Wake:           wake,
	}
	if scheduled != nil {
		req.Commitment = scheduled.Commitment
		req.SourceIDs = append([]string(nil), scheduled.SourceIDs...)
		req.Autonomous = scheduled.Autonomous
		req.Standalone = scheduled.Standalone
	}
	return s.launch(ctx, req)
}

// launch executes req in the background and returns its run once it has
// started.
func (s *RunService) launch(ctx context.Context, req core.ExecuteRequest) (*Run, error) {
	started := newRunStartSignal()
	go s.executeRunAsync(context.WithoutCancel(ctx), req, started)

	select {
	case <-started.Started():
		return s.GetRun(ctx, req.RunID)
	case err := <-started.Failed():
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func projectRun(record core.RunRecord) (Run, error) {
	status, err := projectRunStatus(record.Status)
	if err != nil {
		return Run{}, err
	}
	run := Run{
		ID:        record.RunID,
		ThreadID:  record.SessionID,
		Status:    status,
		CreatedAt: record.CreatedAt,
	}
	if record.Status != core.RunStatusRunning {
		run.CompletedAt = record.FinishedAt
	}
	return run, nil
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

func (s *RunService) executeRunAsync(ctx context.Context, req core.ExecuteRequest, started *clientRunStartSignal) {
	result, err := s.executeRun(ctx, req, runStartSignalSink(started))
	if err != nil {
		// A run the store created has its record whether or not it started.
		if started.MarkFailed(err) && !req.Created {
			return
		}
		if persistErr := s.recordStartedRunFailure(ctx, req.RunID, err); persistErr != nil {
			s.reportBackgroundRunFailure(ctx, req.RunID, err, persistErr)
		}
		return
	}
	if result.Status == core.RunStatusInterrupted && s.resumer != nil {
		if err := s.resumer.ResumeIfReady(ctx, req.RunID); err != nil {
			reportClientBackgroundError(ctx, req.RunID, err)
		}
	}
	if result.Next != nil {
		s.ContinueRun(ctx, *result.Next)
	}
}

// runStartSignalSink adapts a clientRunStartSignal into a core.StreamSink
// that fires RunStarted() when the RunStarted stream event is emitted.
func runStartSignalSink(started *clientRunStartSignal) core.StreamSink {
	if started == nil {
		return nil
	}
	return func(item core.StreamItem) error {
		if item.Kind == core.StreamKindRunStarted {
			started.RunStarted()
		}
		return nil
	}
}

func (s *RunService) reportBackgroundRunFailure(ctx context.Context, runID string, cause, persistErr error) {
	err := errors.Join(
		fmt.Errorf("client executor failed after run start: %w", cause),
		fmt.Errorf("record started client run failure: %w", persistErr),
	)
	reportClientBackgroundError(ctx, runID, err)
}

func (s *RunService) recordStartedRunFailure(ctx context.Context, runID string, cause error) error {
	if strings.TrimSpace(runID) == "" || cause == nil {
		return nil
	}
	record, err := s.store.LoadRun(ctx, runID)
	if err != nil {
		return err
	}
	if record.Status != core.RunStatusRunning {
		return nil
	}
	_, next, err := s.store.FinishRun(ctx, failedRunEnd(runID, cause))
	if err != nil {
		return fmt.Errorf("mark client run failed after background error: %w", err)
	}
	if next != nil {
		s.ContinueRun(ctx, createdRunRequest(*next))
	}
	return nil
}

// failedRunEnd fails runID with cause and starts the next run on owner
// messages that wait in its thread.
func failedRunEnd(runID string, cause error) core.RunEnd {
	return core.RunEnd{RunID: runID, Status: core.RunStatusFailed, Error: cause.Error(), Event: "run.failed", Payload: map[string]any{"error": cause.Error()}, Next: core.NewRunID()}
}
