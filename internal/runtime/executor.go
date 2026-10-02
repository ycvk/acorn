package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
)

type Result struct {
	RunID       string         `json:"run_id"`
	Status      core.RunStatus `json:"status"`
	Output      string         `json:"output,omitempty"`
	Error       string         `json:"error,omitempty"`
	Interrupted map[string]any `json:"interrupted,omitempty"`
}

type Executor struct {
	store        core.SessionStore
	runRuntime   *RunnerFactory
	controller   *RunController
	newChatModel func(ctx context.Context) (einomodel.BaseChatModel, error)
}

func NewExecutorWithRunRuntimeAndController(cfg *config.Config, store core.SessionStore, runRuntime *RunnerFactory, controller *RunController) (*Executor, error) {
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
		return nil, fmt.Errorf("%w: %v", core.ErrExecutionNotReady, err)
	}
	exec := &Executor{
		store:        store,
		runRuntime:   runRuntime,
		controller:   controller,
		newChatModel: runRuntime.NewChatModel,
	}
	return exec, nil
}

func resolveRunID(req core.ExecuteRequest) string {
	if id := strings.TrimSpace(req.RunID); id != "" {
		return id
	}
	return core.NewRunID()
}

func (e *Executor) prepareExecuteRequest(ctx context.Context, req core.ExecuteRequest) (core.ExecuteRequest, error) {
	if strings.TrimSpace(req.SessionID) != "" {
		return req, nil
	}
	req.SessionID = core.NewSessionID()
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

func (e *Executor) ExecuteMessages(ctx context.Context, req core.ExecuteRequest, sink core.StreamSink) (*Result, error) {
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
	execCtx := core.WithWake(buildExecutionContext(runCtxBase, runID, req.SessionID), req.Wake)
	iter := active.Runner.Run(execCtx, req.Messages, adk.WithCheckPointID(runID))
	return e.consume(ctx, runID, req.SessionID, req.Input, iter, sink, active.ChatModel)
}

func (e *Executor) createBoundRun(ctx context.Context, runID string, req core.ExecuteRequest) error {
	if err := e.store.CreateRun(ctx, core.RunCreateParams{
		RunID:     runID,
		SessionID: req.SessionID,
		TurnIndex: req.TurnIndex,
		Input:     req.Input,
	}); err != nil {
		return err
	}
	if req.SessionID == "" {
		return nil
	}
	if req.BoundMessageID > 0 {
		return e.store.BindUserMessageRunIDByID(ctx, req.BoundMessageID, runID)
	}
	return e.store.BindLatestUserMessageRunID(ctx, req.SessionID, req.TurnIndex, runID)
}

func (e *Executor) buildExecuteRunner(runCtxBase context.Context, req core.ExecuteRequest, runID string, sink core.StreamSink) (*ActiveRunner, error) {
	return e.runRuntime.New(runCtxBase, RunnerBuildRequest{
		SessionID: req.SessionID,
		RunID:     runID,
		Input:     req.Input,
		SkillID:   req.SkillID,
		Sink:      sink,
	})
}

func (e *Executor) newManagedRunContext(ctx context.Context, runID string) (context.Context, func()) {
	runTimeout := time.Duration(e.runRuntime.Config().Runtime.RunTimeoutSeconds) * time.Second
	if runTimeout <= 0 {
		runTimeout = 15 * time.Minute
	}
	runCtxBase, cancel := context.WithTimeout(ctx, runTimeout)
	unregister := e.controller.Register(runID, cancel)
	return runCtxBase, func() {
		unregister()
		cancel()
	}
}

// resumeWake is what the present shows when a run continues after the owner
// decided its pending actions.
const resumeWake = "resumed after the owner's decision"

func buildExecutionContext(runCtxBase context.Context, runID, sessionID string) context.Context {
	return core.WithSessionID(core.WithRunID(runCtxBase, runID), sessionID)
}

func (e *Executor) ResumeWithTargets(ctx context.Context, runID string, targets map[string]any, sink core.StreamSink) (*Result, error) {
	run, err := e.store.LoadRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if err := e.store.ResumeInterruptedRun(ctx, runID); err != nil {
		return nil, err
	}
	runCtxBase, cleanup := e.newManagedRunContext(ctx, runID)
	defer cleanup()
	if err := e.emitRunResumeRequested(ctx, runID, targets, sink); err != nil {
		return nil, err
	}
	return e.executeResume(ctx, runCtxBase, *run, runID, targets, sink)
}

func (e *Executor) executeResume(ctx context.Context, runCtxBase context.Context, run core.RunRecord, runID string, targets map[string]any, sink core.StreamSink) (*Result, error) {
	active, err := e.runRuntime.New(runCtxBase, RunnerBuildRequest{
		SessionID: run.SessionID,
		RunID:     runID,
	})
	if err != nil {
		return nil, err
	}
	defer active.Close()
	execCtx := core.WithWake(buildExecutionContext(runCtxBase, runID, run.SessionID), resumeWake)
	iter, err := active.Runner.ResumeWithParams(execCtx, runID, &adk.ResumeParams{Targets: targets})
	if err != nil {
		return nil, fmt.Errorf("resume run %s: %w", runID, err)
	}
	result, err := e.consume(ctx, runID, run.SessionID, run.Input, iter, sink, active.ChatModel)
	if err != nil {
		return nil, err
	}
	if err := e.store.SyncAssistantMessageForRun(ctx, runID); err != nil {
		return nil, err
	}
	return result, nil
}

type RunState struct {
	lastOutput       string
	interrupt        map[string]any
	failure          error
	emittedRunFailed bool
}

func (e *Executor) consume(ctx context.Context, runID, sessionID, input string, iter *adk.AsyncIterator[*adk.AgentEvent], sink core.StreamSink, chatModel einomodel.BaseChatModel) (*Result, error) {
	state, err := e.collectRunState(ctx, runID, iter, sink, chatModel)
	if err != nil {
		return nil, err
	}
	return e.finishCollectedRun(ctx, runID, sessionID, input, state, sink)
}

func (e *Executor) collectRunState(ctx context.Context, runID string, iter *adk.AsyncIterator[*adk.AgentEvent], sink core.StreamSink, chatModel einomodel.BaseChatModel) (RunState, error) {
	state := RunState{}
	projector := newAgentEventProjector(runID, chatModel)
	emit := func(item core.StreamItem) error {
		item.RunID = runID
		if _, err := AppendStreamItem(ctx, e.store, sink, item); err != nil {
			return err
		}
		state.applyStreamItem(item)
		return nil
	}
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if err := projector.project(event, emit); err != nil {
			return RunState{}, err
		}
	}
	if projector.streamErr != nil && state.failure == nil {
		if err := emit(core.StreamItem{Kind: core.StreamKindRunFailed, CreatedAt: time.Now().UTC(), Payload: map[string]any{
			"error": projector.streamErr.Error(),
		}}); err != nil {
			return RunState{}, err
		}
	}
	return state, nil
}

func (s *RunState) applyStreamItem(item core.StreamItem) {
	if delta := core.ItemGetAssistantDelta(item); delta != nil {
		s.lastOutput += delta.Delta
	}
	if msg := core.ItemGetMessage(item); msg != nil && msg.Content != "" {
		s.lastOutput = msg.Content
	}
	if interrupt := core.ItemGetInterrupt(item); interrupt != nil {
		s.interrupt = core.InterruptPayloadFromStream(interrupt)
	}
	if item.Kind == core.StreamKindRunFailed && core.ItemGetError(item) != "" {
		s.failure = errors.New(core.ItemGetError(item))
		s.emittedRunFailed = true
	}
}

func (e *Executor) emitLifecyclePayload(ctx context.Context, runID string, sink core.StreamSink, kind core.StreamItemKind, payload map[string]any) error {
	_, err := AppendStreamItem(ctx, e.store, sink, core.StreamItem{
		RunID:     runID,
		Kind:      kind,
		CreatedAt: time.Now().UTC(),
		Payload:   payload,
	})
	return err
}

func (e *Executor) emitRunStarted(ctx context.Context, runID, input string, sink core.StreamSink) error {
	return e.emitLifecyclePayload(ctx, runID, sink, core.StreamKindRunStarted, map[string]any{"input": input})
}

func (e *Executor) emitRunResumeRequested(ctx context.Context, runID string, targets map[string]any, sink core.StreamSink) error {
	return e.emitLifecyclePayload(ctx, runID, sink, core.StreamKindRunResumeRequested, map[string]any{"targets": targets})
}

func (e *Executor) emitRunCompleted(ctx context.Context, runID, output string, sink core.StreamSink) error {
	return e.emitLifecyclePayload(ctx, runID, sink, core.StreamKindRunCompleted, map[string]any{"message": &core.StreamMessage{
		Role:    string(schema.Assistant),
		Content: output,
	}})
}

func (e *Executor) emitRunFailed(ctx context.Context, runID string, sink core.StreamSink, message string) error {
	return e.emitLifecyclePayload(ctx, runID, sink, core.StreamKindRunFailed, map[string]any{"error": message})
}

func (e *Executor) failRunSetup(ctx context.Context, runID string, setupErr error, sink core.StreamSink) error {
	if strings.TrimSpace(runID) == "" || setupErr == nil {
		return setupErr
	}
	durableCtx := core.DurableContext(ctx)
	if err := e.emitRunFailed(durableCtx, runID, sink, setupErr.Error()); err != nil {
		return err
	}
	return e.store.FinishRun(durableCtx, runID, core.RunStatusFailed, "", setupErr.Error())
}

func (e *Executor) failSetupOrErr(ctx context.Context, runID string, setupErr error, sink core.StreamSink) error {
	if failErr := e.failRunSetup(ctx, runID, setupErr, sink); failErr != nil {
		return failErr
	}
	return setupErr
}

func (e *Executor) finishCollectedRun(ctx context.Context, runID, sessionID, input string, state RunState, sink core.StreamSink) (*Result, error) {
	switch {
	case state.failure != nil:
		return e.finishFailedRun(ctx, runID, sessionID, input, state, sink)
	case state.interrupt != nil:
		return e.finishInterruptedRun(ctx, runID, state)
	default:
		return e.finishSucceededRun(ctx, runID, sessionID, input, state, sink)
	}
}

func (e *Executor) finishFailedRun(ctx context.Context, runID, sessionID, input string, state RunState, sink core.StreamSink) (*Result, error) {
	durableCtx := core.DurableContext(ctx)
	if !state.emittedRunFailed && state.failure != nil {
		if err := e.emitRunFailed(durableCtx, runID, sink, state.failure.Error()); err != nil {
			return nil, err
		}
	}
	if err := e.store.FinishRun(durableCtx, runID, core.RunStatusFailed, state.lastOutput, state.failure.Error()); err != nil {
		return nil, err
	}
	if err := e.store.DeleteCheckpoint(durableCtx, runID); err != nil {
		return nil, err
	}
	if err := e.store.SyncAssistantMessageForRunStatus(durableCtx, runID, core.RunStatusFailed); err != nil {
		slog.Error("sync assistant message after run completion", "run_id", runID, "err", err)
	}
	// Append history after the run is marked complete.
	return &Result{
		RunID:  runID,
		Status: core.RunStatusFailed,
		Output: state.lastOutput,
		Error:  state.failure.Error(),
	}, nil
}

func (e *Executor) finishInterruptedRun(ctx context.Context, runID string, state RunState) (*Result, error) {
	durableCtx := core.DurableContext(ctx)
	if err := e.store.MarkInterrupted(durableCtx, runID, state.lastOutput); err != nil {
		return nil, err
	}
	return &Result{
		RunID:       runID,
		Status:      core.RunStatusInterrupted,
		Output:      state.lastOutput,
		Interrupted: state.interrupt,
	}, nil
}

func (e *Executor) finishSucceededRun(ctx context.Context, runID, sessionID, input string, state RunState, sink core.StreamSink) (*Result, error) {
	durableCtx := core.DurableContext(ctx)
	if err := e.store.UpdateRunOutput(durableCtx, runID, state.lastOutput); err != nil {
		return nil, err
	}
	if err := e.emitRunCompleted(durableCtx, runID, state.lastOutput, sink); err != nil {
		return nil, err
	}
	if err := e.store.FinishRun(durableCtx, runID, core.RunStatusSucceeded, state.lastOutput, ""); err != nil {
		return nil, err
	}
	if err := e.store.DeleteCheckpoint(durableCtx, runID); err != nil {
		return nil, err
	}
	if err := e.store.SyncAssistantMessageForRunStatus(durableCtx, runID, core.RunStatusSucceeded); err != nil {
		slog.Error("sync assistant message after run completion", "run_id", runID, "err", err)
	}
	// Append history + count toward review after the run is marked complete,
	// so this work does not delay the completion event or status update.
	return &Result{
		RunID:  runID,
		Status: core.RunStatusSucceeded,
		Output: state.lastOutput,
	}, nil
}
