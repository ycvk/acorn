package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/core"
)

type Result struct {
	RunID       string         `json:"run_id"`
	Status      core.RunStatus `json:"status"`
	Output      string         `json:"output,omitempty"`
	Error       string         `json:"error,omitempty"`
	Interrupted map[string]any `json:"interrupted,omitempty"`
	// Next is the run the store started on owner messages that waited for
	// this one to end; the caller executes it.
	Next *core.ExecuteRequest `json:"-"`
}

type Executor struct {
	store      core.SessionStore
	subRuns    core.SubRunStore
	runRuntime *RunnerFactory
	controller *RunController
}

func NewExecutorWithRunRuntimeAndController(store core.SessionStore, runRuntime *RunnerFactory, controller *RunController) (*Executor, error) {
	if store == nil {
		return nil, errors.New("store is required")
	}
	if runRuntime == nil {
		return nil, errors.New("run runtime is required")
	}
	if controller == nil {
		return nil, errors.New("run controller is required")
	}
	if err := runRuntime.deps.Config.ValidateExecutionReady(); err != nil {
		return nil, fmt.Errorf("%w: %w", core.ErrExecutionNotReady, err)
	}
	exec := &Executor{
		store:      store,
		subRuns:    runRuntime.deps.Store,
		runRuntime: runRuntime,
		controller: controller,
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
	title := compactText(req.Input, 48)
	turnIndex, err := e.store.CreateFreshSessionTurn(ctx, req.SessionID, title, req.Input)
	if err != nil {
		return req, err
	}
	req.TurnIndex = turnIndex
	return req, nil
}

// ExecuteMessages registers the run before it records anything, so a run
// refused while Acorn stops leaves no trace. A run the store created keeps
// its record running, and the next start fails it.
func (e *Executor) ExecuteMessages(ctx context.Context, req core.ExecuteRequest, sink core.StreamSink) (*Result, error) {
	runID := resolveRunID(req)
	runCtxBase, cleanup, err := e.newManagedRunContext(ctx, runID)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if !req.Created {
		if req, err = e.prepareExecuteRequest(ctx, req); err != nil {
			return nil, err
		}
		if err := e.createBoundRun(ctx, runID, req); err != nil {
			return nil, err
		}
	}
	if err := e.runRuntime.deps.MemoryStore.LinkRunInputSources(ctx, runID, req.SourceIDs); err != nil {
		return nil, e.failSetupOrErr(ctx, runID, err, sink)
	}
	if req.Commitment != nil {
		if err := e.runRuntime.deps.Commitments.StartCommitmentOccurrence(ctx, req.Commitment.Occurrence, runID, req.Commitment.Next, e.runRuntime.deps.Clock()); err != nil {
			return nil, e.failSetupOrErr(ctx, runID, err, sink)
		}
	}
	if err := e.emitRunStarted(ctx, runID, req.Input, sink); err != nil {
		return nil, err
	}
	runCtxBase = memoryScope(runCtxBase, runID, req.Autonomous)
	active, err := e.runRuntime.New(runCtxBase, RunnerBuildRequest{
		SessionID:  req.SessionID,
		RunID:      runID,
		Wake:       req.Wake,
		Standalone: req.Standalone,
		Autonomous: req.Autonomous,
		SubRuns:    e,
	})
	if err != nil {
		return nil, e.failSetupOrErr(ctx, runID, err, sink)
	}
	execCtx := core.WithWake(buildExecutionContext(runCtxBase, runID, req.SessionID), req.Wake)
	iter := active.Runner.Run(execCtx, active.Memory.messages(), adk.WithCheckPointID(runID))
	return e.consume(ctx, runCtxBase, execCtx, req.SessionID, runID, iter, sink, active)
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

func (e *Executor) newManagedRunContext(ctx context.Context, runID string) (context.Context, func(), error) {
	epoch, err := e.runRuntime.deps.MemoryStore.MemoryEpoch(ctx)
	if err != nil {
		return nil, nil, err
	}
	runCtxBase, stop := context.WithCancelCause(ctx)
	cancel := func() { stop(nil) }
	unregister, err := e.controller.Register(runID, stop)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchRunMemory(runCtxBase, e.runRuntime.deps.MemoryStore, runID, epoch, stop)
	}()
	return runCtxBase, func() { cancel(); <-done; unregister() }, nil
}

// resumeWake is what the present shows when a run continues after the owner
// decided its pending actions.
const resumeWake = "resumed after the owner's decision"

func buildExecutionContext(runCtxBase context.Context, runID, sessionID string) context.Context {
	return core.WithSessionID(core.WithRunID(runCtxBase, runID), sessionID)
}

// ResumeWithTargets registers the run before it claims it, so a resume
// refused while Acorn stops leaves the run interrupted for the next start.
func (e *Executor) ResumeWithTargets(ctx context.Context, runID string, targets map[string]any, sink core.StreamSink) (*Result, error) {
	run, err := e.store.LoadRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	runCtxBase, cleanup, err := e.newManagedRunContext(ctx, runID)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if err := e.store.ResumeInterruptedRun(ctx, runID); err != nil {
		return nil, err
	}
	if err := e.emitRunResumeRequested(ctx, runID, targets, sink); err != nil {
		return nil, err
	}
	return e.executeResume(ctx, runCtxBase, *run, runID, targets, sink)
}

func (e *Executor) executeResume(ctx context.Context, runCtxBase context.Context, run core.RunRecord, runID string, targets map[string]any, sink core.StreamSink) (*Result, error) {
	sources, err := e.runRuntime.deps.MemoryStore.RunMemorySources(ctx, runID)
	if err != nil {
		return nil, err
	}
	autonomous := true
	for _, source := range sources {
		if source.Kind == "message" && source.Speaker == "owner" {
			autonomous = false
			break
		}
	}
	runCtxBase = memoryScope(runCtxBase, runID, autonomous)
	active, err := e.runRuntime.New(runCtxBase, RunnerBuildRequest{
		SessionID:  run.SessionID,
		RunID:      runID,
		Autonomous: autonomous,
		SubRuns:    e,
	})
	if err != nil {
		return nil, err
	}
	execCtx := core.WithWake(buildExecutionContext(runCtxBase, runID, run.SessionID), resumeWake)
	iter, err := active.Runner.ResumeWithParams(execCtx, runID, &adk.ResumeParams{Targets: targets})
	if err != nil {
		return nil, fmt.Errorf("resume run %s: %w", runID, err)
	}
	result, err := e.consume(ctx, runCtxBase, execCtx, run.SessionID, runID, iter, sink, active)
	if err != nil {
		return nil, err
	}
	if err := e.store.SyncAssistantMessageForRun(ctx, runID); err != nil {
		return nil, err
	}
	return result, nil
}

type RunState struct {
	lastOutput string
	interrupt  map[string]any
	failure    error
}

// consume records the run's events and its end. ctx carries the writes; runCtx
// is the run's own context, whose cancellation cause explains a stopped run,
// and execCtx the one its agent runs in. When the agent ends while sub-runs
// of the run still go, the run waits for them and the agent goes on from
// its messages, so the run ends after every sub-run reported.
func (e *Executor) consume(ctx, runCtx, execCtx context.Context, sessionID, runID string, iter *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]], sink core.StreamSink, active *ActiveRunner) (*Result, error) {
	defer e.controller.forgetSubRunSignal(runID)
	state, err := e.collectUntilReported(ctx, runCtx, execCtx, sessionID, runID, iter, sink, active)
	if err != nil {
		return nil, err
	}
	return e.finishCollectedRun(ctx, runID, state, sink)
}

// collectUntilReported collects the run's events until it fails, waits for
// the owner, or ends with every sub-run reported.
func (e *Executor) collectUntilReported(ctx, runCtx, execCtx context.Context, sessionID, runID string, iter *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]], sink core.StreamSink, active *ActiveRunner) (RunState, error) {
	state, err := e.collectRunState(ctx, runCtx, runID, iter, sink, active)
	for err == nil && state.failure == nil && state.interrupt == nil {
		var more bool
		if more, err = e.awaitSubRuns(runCtx, runID, sessionID); err != nil {
			state.failure, err = err, nil
			break
		}
		if !more {
			break
		}
		var messages []*schema.AgenticMessage
		if messages, err = active.Reports.continuation(); err != nil {
			break
		}
		iter = active.Runner.Run(execCtx, messages, adk.WithCheckPointID(runID))
		state, err = e.collectRunState(ctx, runCtx, runID, iter, sink, active)
	}
	return state, err
}

func (e *Executor) collectRunState(ctx, runCtx context.Context, runID string, iter *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]], sink core.StreamSink, active *ActiveRunner) (RunState, error) {
	state := RunState{}
	projector := newAgentEventProjector(runID, active.FailedCalls)
	emit := func(item core.StreamItem) error {
		item.RunID = runID
		// A failure ends the run: finishFailedRun records it as the run's
		// terminal event.
		if item.Kind != core.StreamKindRunFailed {
			if _, err := AppendStreamItem(ctx, e.store, sink, item); err != nil {
				return err
			}
		}
		state.applyStreamItem(item)
		return nil
	}
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if err := projector.project(withStopCause(runCtx, event), emit); err != nil {
			return RunState{}, err
		}
	}
	if projector.streamErr != nil && state.failure == nil {
		state.failure = projector.streamErr
	}
	return state, nil
}

// withStopCause gives the error of a run cancelled with a cause, such as
// core.ErrShuttingDown, as that cause in place of the bare context error.
func withStopCause(runCtx context.Context, event *adk.TypedAgentEvent[*schema.AgenticMessage]) *adk.TypedAgentEvent[*schema.AgenticMessage] {
	if event.Err == nil || runCtx.Err() == nil {
		return event
	}
	cause := context.Cause(runCtx)
	if errors.Is(cause, runCtx.Err()) {
		return event
	}
	stopped := *event
	stopped.Err = cause
	return &stopped
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
	}
}

func (e *Executor) emitLifecyclePayload(ctx context.Context, runID string, sink core.StreamSink, kind core.StreamItemKind, payload map[string]any) error {
	_, err := AppendStreamItem(ctx, e.store, sink, core.StreamItem{
		RunID:   runID,
		Kind:    kind,
		Payload: payload,
	})
	return err
}

func (e *Executor) emitRunStarted(ctx context.Context, runID, input string, sink core.StreamSink) error {
	return e.emitLifecyclePayload(ctx, runID, sink, core.StreamKindRunStarted, map[string]any{"input": input})
}

func (e *Executor) emitRunResumeRequested(ctx context.Context, runID string, targets map[string]any, sink core.StreamSink) error {
	return e.emitLifecyclePayload(ctx, runID, sink, core.StreamKindRunResumeRequested, map[string]any{"targets": targets})
}

// finishRun records the run's end with its terminal event; with next it
// lets the store start the next run on owner messages that wait in the
// conversation. The terminal event reaches sink once it is stored.
func (e *Executor) finishRun(ctx context.Context, sink core.StreamSink, end core.RunEnd, item core.StreamItem, next bool) (*core.ExecuteRequest, error) {
	item.RunID = end.RunID
	kind, payload, err := ProjectStreamItemToEvent(item)
	if err != nil {
		return nil, err
	}
	end.Event = kind
	end.Payload = payload
	if next {
		end.Next = core.NewRunID()
	}
	event, start, err := e.store.FinishRun(ctx, end)
	if err != nil {
		return nil, err
	}
	if sink != nil {
		item.Sequence, item.CreatedAt = event.Sequence, event.CreatedAt
		if err := sink(item); err != nil {
			return nil, err
		}
	}
	if start == nil {
		return nil, nil
	}
	return &core.ExecuteRequest{RunID: start.RunID, SessionID: start.SessionID, TurnIndex: start.TurnIndex, Input: start.Input, Created: true}, nil
}

func runCompletedItem(output string) core.StreamItem {
	return core.StreamItem{Kind: core.StreamKindRunCompleted, Payload: map[string]any{"message": &core.StreamMessage{
		Role:    string(schema.Assistant),
		Content: output,
	}}}
}

func runFailedItem(message string) core.StreamItem {
	return core.StreamItem{Kind: core.StreamKindRunFailed, Payload: map[string]any{"error": message}}
}

// failRunSetup fails a run whose setup failed. Owner messages that wait for
// it start the next run from the queue sweep.
func (e *Executor) failRunSetup(ctx context.Context, runID string, setupErr error, sink core.StreamSink) error {
	durableCtx := context.WithoutCancel(ctx)
	_, err := e.finishRun(durableCtx, sink, core.RunEnd{RunID: runID, Status: core.RunStatusFailed, Error: setupErr.Error()}, runFailedItem(setupErr.Error()), false)
	return err
}

func (e *Executor) failSetupOrErr(ctx context.Context, runID string, setupErr error, sink core.StreamSink) error {
	if failErr := e.failRunSetup(ctx, runID, setupErr, sink); failErr != nil {
		return failErr
	}
	return setupErr
}

func (e *Executor) finishCollectedRun(ctx context.Context, runID string, state RunState, sink core.StreamSink) (*Result, error) {
	switch {
	case state.failure != nil:
		return e.finishFailedRun(ctx, runID, state, sink)
	case state.interrupt != nil:
		return e.finishInterruptedRun(ctx, runID, state)
	default:
		return e.finishSucceededRun(ctx, runID, state, sink)
	}
}

func (e *Executor) finishFailedRun(ctx context.Context, runID string, state RunState, sink core.StreamSink) (*Result, error) {
	durableCtx := context.WithoutCancel(ctx)
	if err := e.recordAssistantMessage(durableCtx, runID, state.lastOutput, core.RunStatusFailed); err != nil {
		return nil, err
	}
	end := core.RunEnd{RunID: runID, Status: core.RunStatusFailed, Output: state.lastOutput, Error: state.failure.Error()}
	next, err := e.finishRun(durableCtx, sink, end, runFailedItem(state.failure.Error()), true)
	if err != nil {
		return nil, err
	}
	if err := e.store.DeleteCheckpoint(durableCtx, runID); err != nil {
		return nil, err
	}
	return &Result{
		RunID:  runID,
		Status: core.RunStatusFailed,
		Output: state.lastOutput,
		Error:  state.failure.Error(),
		Next:   next,
	}, nil
}

// recordAssistantMessage stores the run's output and its assistant message
// before the run reports completion, so a client that reloads the thread on
// run.completed or a finished status finds the reply.
func (e *Executor) recordAssistantMessage(ctx context.Context, runID, output string, status core.RunStatus) error {
	if err := e.store.UpdateRunOutput(ctx, runID, output); err != nil {
		return err
	}
	if err := e.store.SyncAssistantMessageForRunStatus(ctx, runID, status); err != nil {
		return fmt.Errorf("record assistant message for %s: %w", runID, err)
	}
	return nil
}

func (e *Executor) finishInterruptedRun(ctx context.Context, runID string, state RunState) (*Result, error) {
	durableCtx := context.WithoutCancel(ctx)
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

func (e *Executor) finishSucceededRun(ctx context.Context, runID string, state RunState, sink core.StreamSink) (*Result, error) {
	durableCtx := context.WithoutCancel(ctx)
	if err := e.recordAssistantMessage(durableCtx, runID, state.lastOutput, core.RunStatusSucceeded); err != nil {
		return nil, err
	}
	end := core.RunEnd{RunID: runID, Status: core.RunStatusSucceeded, Output: state.lastOutput}
	next, err := e.finishRun(durableCtx, sink, end, runCompletedItem(state.lastOutput), true)
	if err != nil {
		return nil, err
	}
	if err := e.store.DeleteCheckpoint(durableCtx, runID); err != nil {
		return nil, err
	}
	return &Result{
		RunID:  runID,
		Status: core.RunStatusSucceeded,
		Output: state.lastOutput,
		Next:   next,
	}, nil
}
