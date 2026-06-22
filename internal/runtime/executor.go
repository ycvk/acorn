package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/runtime/eventstream"
	"github.com/ycvk/acorn/internal/store"
)

type Result struct {
	RunID       string           `json:"run_id"`
	Status      domain.RunStatus `json:"status"`
	Output      string           `json:"output,omitempty"`
	Error       string           `json:"error,omitempty"`
	Interrupted map[string]any   `json:"interrupted,omitempty"`
}

type Executor struct {
	store             ExecutorStore
	runRuntime        RunRuntime
	controller        *RunController
	newChatModel      func(ctx context.Context) (einomodel.BaseChatModel, error)
	sessionSummarySvc *domain.SessionSummaryService
}

func NewExecutorWithRunRuntimeAndController(cfg *config.Config, store ExecutorStore, runRuntime RunRuntime, controller *RunController) (*Executor, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	if store == nil {
		return nil, errors.New("store is required")
	}
	if runRuntime == nil {
		return nil, errors.New("run runtime is required")
	}
	if controller == nil {
		controller = NewRunController()
	}
	if err := cfg.ValidateExecutionReady(); err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrExecutionNotReady, err)
	}
	exec := &Executor{
		store:             store,
		runRuntime:        runRuntime,
		controller:        controller,
		sessionSummarySvc: runRuntime.SessionSummarySvc(),
		newChatModel:      runRuntime.NewChatModel,
	}
	return exec, nil
}
func (e *Executor) Run(ctx context.Context, input, skillID string, sink eventstream.StreamSink) (*Result, error) {
	sessionID := newSessionID()
	title, _ := compactText(input, 48)
	turnIndex, err := e.store.CreateFreshSessionTurn(ctx, sessionID, title, input)
	if err != nil {
		return nil, err
	}
	return e.ExecuteMessages(ctx, domain.ExecuteRequest{
		SessionID: sessionID,
		TurnIndex: turnIndex,
		Input:     input,
		SkillID:   skillID,
		Messages:  []adk.Message{schema.UserMessage(input)},
	}, sink)
}

func resolveRunID(req domain.ExecuteRequest) string {
	if id := strings.TrimSpace(req.RunID); id != "" {
		return id
	}
	return NewRunID()
}

func (e *Executor) prepareExecuteRequest(ctx context.Context, req domain.ExecuteRequest) (domain.ExecuteRequest, error) {
	if strings.TrimSpace(req.SessionID) != "" {
		return req, nil
	}
	req.SessionID = newSessionID()
	title, _ := compactText(req.Input, 48)
	turnIndex, err := e.store.CreateFreshSessionTurn(ctx, req.SessionID, title, req.Input)
	if err != nil {
		return req, err
	}
	req.TurnIndex = turnIndex
	if len(req.Messages) == 0 && strings.TrimSpace(req.Input) != "" {
		req.Messages = []adk.Message{schema.UserMessage(req.Input)}
	}
	return req, nil
}

func (e *Executor) ExecuteMessages(ctx context.Context, req domain.ExecuteRequest, sink eventstream.StreamSink) (*Result, error) {
	runID := resolveRunID(req)
	req, err := e.prepareExecuteRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := e.createBoundRun(ctx, runID, req); err != nil {
		return nil, err
	}
	runCtxBase, cleanup := e.newManagedRunContext(ctx, runID)
	defer cleanup()
	if err := e.emitRunStarted(ctx, runID, req.Input, sink); err != nil {
		return nil, err
	}
	active, err := e.buildExecuteRunner(runCtxBase, req, runID, sink)
	if err != nil {
		return nil, e.failSetupOrErr(ctx, runID, err, sink)
	}
	defer active.Close()
	messages, err := e.bootstrapContextSessionMessages(ctx, req, runID, active)
	if err != nil {
		return nil, e.failSetupOrErr(ctx, runID, err, sink)
	}
	iter := active.Runner.Run(e.executionContext(runCtxBase, runID, req, active, sink), messages, adk.WithCheckPointID(runID))
	return e.consume(ctx, runID, req.Input, iter, active.SelectedSkill, sink, active.ChatModel)
}

func (e *Executor) createBoundRun(ctx context.Context, runID string, req domain.ExecuteRequest) error {
	return e.store.CreateBoundRunWithParams(ctx, store.RunCreateParams{
		RunID:          runID,
		SessionID:      req.SessionID,
		TurnIndex:      req.TurnIndex,
		Input:          req.Input,
		BoundMessageID: req.BoundMessageID,
	})
}

func (e *Executor) buildExecuteRunner(runCtxBase context.Context, req domain.ExecuteRequest, runID string, sink eventstream.StreamSink) (*ActiveRunner, error) {
	return e.runRuntime.New(runCtxBase, RunnerBuildRequest{
		SessionID:        req.SessionID,
		RunID:            runID,
		Input:            req.Input,
		SkillID:          req.SkillID,
		AllowedToolNames: append([]string(nil), req.AllowedToolNames...),
		Sink:             sink,
	})
}

func (e *Executor) executionContext(runCtxBase context.Context, runID string, req domain.ExecuteRequest, active *ActiveRunner, sink eventstream.StreamSink) context.Context {
	executionCtx := buildExecutionContext(runCtxBase, runID, req.SessionID, req.TurnIndex, sink)
	if active.ContextSession != nil {
		executionCtx = contextplane.WithContextSession(executionCtx, active.ContextSession)
	}
	return executionCtx
}

func (e *Executor) newManagedRunContext(ctx context.Context, runID string) (context.Context, func()) {
	runTimeout := time.Duration(e.runRuntime.Config().Runtime.RunTimeoutSeconds) * time.Second
	if runTimeout <= 0 {
		runTimeout = 15 * time.Minute
	}
	runCtxBase, cancel := context.WithTimeout(ctx, runTimeout)
	if e.controller == nil {
		return runCtxBase, cancel
	}
	e.controller.Register(runID, cancel)
	return runCtxBase, func() {
		e.controller.Clear(runID)
		cancel()
	}
}

func buildExecutionContext(runCtxBase context.Context, runID, sessionID string, turnIndex int, sink eventstream.StreamSink) context.Context {
	runCtx := domain.WithRunID(runCtxBase, runID)
	runCtx = domain.WithSessionID(runCtx, sessionID)
	runCtx = domain.WithTurnIndex(runCtx, turnIndex)
	return eventstream.WithStreamSink(runCtx, sink)
}

func (e *Executor) ResumeWithTargets(ctx context.Context, runID string, targets map[string]any, sink eventstream.StreamSink) (*Result, error) {
	run, err := e.store.LoadRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != domain.RunStatusInterrupted {
		return nil, fmt.Errorf("%w: %s", domain.ErrRunNotInterrupted, runID)
	}
	runCtxBase, cleanup := e.newManagedRunContext(ctx, runID)
	defer cleanup()
	if err := e.emitRunResumeRequested(ctx, runID, targets, sink); err != nil {
		return nil, err
	}
	return e.executeResume(ctx, runCtxBase, *run, runID, targets, sink)
}

func (e *Executor) executeResume(ctx context.Context, runCtxBase context.Context, run domain.RunRecord, runID string, targets map[string]any, sink eventstream.StreamSink) (*Result, error) {
	active, err := e.runRuntime.New(runCtxBase, RunnerBuildRequest{
		SessionID: run.SessionID,
		RunID:     runID,
	})
	if err != nil {
		return nil, err
	}
	defer active.Close()
	if err := e.bootstrapResumeContextSession(ctx, run, runID, active); err != nil {
		return nil, fmt.Errorf("bootstrap resume context session: %w", err)
	}
	iter, err := e.resumeIter(runCtxBase, run, runID, active, targets, sink)
	if err != nil {
		return nil, err
	}
	result, err := e.consume(ctx, runID, run.Input, iter, active.SelectedSkill, sink, active.ChatModel)
	if err != nil {
		return nil, err
	}
	if err := e.store.SyncAssistantMessageForRun(ctx, runID); err != nil {
		return nil, err
	}
	return result, nil
}

func (e *Executor) resumeIter(runCtxBase context.Context, run domain.RunRecord, runID string, active *ActiveRunner, targets map[string]any, sink eventstream.StreamSink) (*adk.AsyncIterator[*adk.AgentEvent], error) {
	executionCtx := contextplane.WithContextSession(
		buildExecutionContext(runCtxBase, runID, run.SessionID, run.TurnIndex, sink), active.ContextSession)
	iter, err := active.Runner.ResumeWithParams(executionCtx, runID, &adk.ResumeParams{Targets: targets})
	if err != nil {
		return nil, fmt.Errorf("resume run %s: %w", runID, err)
	}
	return iter, nil
}

func (e *Executor) bootstrapResumeContextSession(ctx context.Context, run domain.RunRecord, runID string, active *ActiveRunner) error {
	if active.ContextSession != nil {
		return nil
	}
	messages := []adk.Message{}
	if strings.TrimSpace(run.Input) != "" {
		messages = []adk.Message{schema.UserMessage(run.Input)}
	}
	_, err := e.bootstrapContextSessionMessages(ctx, domain.ExecuteRequest{
		SessionID: run.SessionID,
		TurnIndex: run.TurnIndex,
		Input:     run.Input,
		Messages:  messages,
	}, runID, active)
	return err
}

type RunState struct {
	lastOutput       string
	interrupt        map[string]any
	failure          error
	emittedRunFailed bool
}

func (e *Executor) consume(ctx context.Context, runID, input string, iter *adk.AsyncIterator[*adk.AgentEvent], selectedSkill *SelectedSkill, sink eventstream.StreamSink, chatModel einomodel.BaseChatModel) (*Result, error) {
	state, err := e.collectRunState(ctx, runID, iter, sink, chatModel)
	if err != nil {
		return nil, err
	}
	return e.finishCollectedRun(ctx, runID, input, state, selectedSkill, sink)
}

func (e *Executor) collectRunState(ctx context.Context, runID string, iter *adk.AsyncIterator[*adk.AgentEvent], sink eventstream.StreamSink, chatModel einomodel.BaseChatModel) (RunState, error) {
	state := RunState{}
	for {
		event, ok := iter.Next()
		if !ok {
			return state, nil
		}
		if err := e.applyAgentEvent(ctx, runID, eventstream.StreamItemsFromAgentEvent(event, chatModel), sink, &state); err != nil {
			return RunState{}, err
		}
	}
}

func (e *Executor) applyAgentEvent(ctx context.Context, runID string, items []eventstream.StreamItem, sink eventstream.StreamSink, state *RunState) error {
	for _, item := range items {
		item.RunID = runID
		if _, err := eventstream.AppendStreamItem(ctx, e.store, sink, item); err != nil {
			return err
		}
		state.applyStreamItem(item)
	}
	return nil
}

func (s *RunState) applyStreamItem(item eventstream.StreamItem) {
	if delta := item.GetAssistantDelta(); delta != nil {
		s.lastOutput += delta.Delta
	}
	if msg := item.GetMessage(); msg != nil && msg.Content != "" {
		s.lastOutput = msg.Content
	}
	if interrupt := item.GetInterrupt(); interrupt != nil {
		s.interrupt = InterruptPayloadFromStream(interrupt)
	}
	if item.Kind == eventstream.StreamKindRunFailed && item.GetError() != "" {
		s.failure = errors.New(item.GetError())
		s.emittedRunFailed = true
	}
}
