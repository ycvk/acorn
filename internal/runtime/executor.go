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
	"github.com/ycvk/acorn/internal/memory"
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
	reviewer     *Reviewer
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

// SetReviewer attaches a periodic memory reviewer. Optional: nil or
// NewReviewer returning nil disables review.
func (e *Executor) SetReviewer(r *Reviewer) {
	e.reviewer = r
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
	messages, err := e.bootstrapContextSessionMessages(ctx, req, runID, active)
	if err != nil {
		return nil, e.failSetupOrErr(ctx, runID, err, sink)
	}
	iter := active.Runner.Run(e.executionContext(runCtxBase, runID, req, active, sink), messages, adk.WithCheckPointID(runID))
	return e.consume(ctx, runID, req.Input, iter, active.SelectedSkill, sink, active.ChatModel)
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
		SessionID:        req.SessionID,
		RunID:            runID,
		Input:            req.Input,
		SkillID:          req.SkillID,
		AllowedToolNames: append([]string(nil), req.AllowedToolNames...),
		Sink:             sink,
	})
}

func (e *Executor) executionContext(runCtxBase context.Context, runID string, req core.ExecuteRequest, active *ActiveRunner, sink core.StreamSink) context.Context {
	executionCtx := buildExecutionContext(runCtxBase, runID, req.SessionID, req.TurnIndex, sink)
	if active.ContextSession != nil {
		executionCtx = WithSession(executionCtx, active.ContextSession)
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

func buildExecutionContext(runCtxBase context.Context, runID, sessionID string, turnIndex int, sink core.StreamSink) context.Context {
	runCtx := core.WithRunID(runCtxBase, runID)
	runCtx = core.WithSessionID(runCtx, sessionID)
	runCtx = core.WithTurnIndex(runCtx, turnIndex)
	return core.WithStreamSink(runCtx, sink)
}

func (e *Executor) ResumeWithTargets(ctx context.Context, runID string, targets map[string]any, sink core.StreamSink) (*Result, error) {
	run, err := e.store.LoadRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != core.RunStatusInterrupted {
		return nil, fmt.Errorf("%w: %s", core.ErrRunNotInterrupted, runID)
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

func (e *Executor) resumeIter(runCtxBase context.Context, run core.RunRecord, runID string, active *ActiveRunner, targets map[string]any, sink core.StreamSink) (*adk.AsyncIterator[*adk.AgentEvent], error) {
	executionCtx := WithSession(
		buildExecutionContext(runCtxBase, runID, run.SessionID, run.TurnIndex, sink), active.ContextSession)
	iter, err := active.Runner.ResumeWithParams(executionCtx, runID, &adk.ResumeParams{Targets: targets})
	if err != nil {
		return nil, fmt.Errorf("resume run %s: %w", runID, err)
	}
	return iter, nil
}

func (e *Executor) bootstrapResumeContextSession(ctx context.Context, run core.RunRecord, runID string, active *ActiveRunner) error {
	if active.ContextSession != nil {
		return nil
	}
	messages := []adk.Message{}
	if strings.TrimSpace(run.Input) != "" {
		messages = []adk.Message{schema.UserMessage(run.Input)}
	}
	_, err := e.bootstrapContextSessionMessages(ctx, core.ExecuteRequest{
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

func (e *Executor) consume(ctx context.Context, runID, input string, iter *adk.AsyncIterator[*adk.AgentEvent], selectedSkill *SelectedSkill, sink core.StreamSink, chatModel einomodel.BaseChatModel) (*Result, error) {
	state, err := e.collectRunState(ctx, runID, iter, sink, chatModel)
	if err != nil {
		return nil, err
	}
	return e.finishCollectedRun(ctx, runID, input, state, selectedSkill, sink)
}

func (e *Executor) collectRunState(ctx context.Context, runID string, iter *adk.AsyncIterator[*adk.AgentEvent], sink core.StreamSink, chatModel einomodel.BaseChatModel) (RunState, error) {
	state := RunState{}
	for {
		event, ok := iter.Next()
		if !ok {
			return state, nil
		}
		if err := e.applyAgentEvent(ctx, runID, StreamItemsFromAgentEvent(event, chatModel), sink, &state); err != nil {
			return RunState{}, err
		}
	}
}

func (e *Executor) applyAgentEvent(ctx context.Context, runID string, items []core.StreamItem, sink core.StreamSink, state *RunState) error {
	for _, item := range items {
		item.RunID = runID
		if _, err := AppendStreamItem(ctx, e.store, sink, item); err != nil {
			return err
		}
		state.applyStreamItem(item)
	}
	return nil
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

func (e *Executor) verifyAndRecordSkill(ctx context.Context, runID string, selected *SelectedSkill, status core.RunStatus, output string, sink core.StreamSink) error {
	if selected == nil || strings.TrimSpace(runID) == "" || status != core.RunStatusFailed {
		return nil
	}
	_, err := AppendStreamItem(ctx, e.store, sink, core.StreamItem{
		RunID: runID,
		Kind:  core.StreamKindSkillFailed,
		Payload: map[string]any{"skill": &core.StreamSkill{
			SelectedID:    selected.Skill.ID,
			Name:          selected.Skill.Name,
			Source:        selected.Skill.Source,
			Path:          selected.Skill.Path,
			Summary:       selected.Skill.Summary,
			Requirements:  streamSkillRequirementsFromDomain(selected.Skill.Requires),
			FailureReason: failureReasonForStatus(status, output),
		}},
	})
	return err
}

func (e *Executor) finishCollectedRun(ctx context.Context, runID, input string, state RunState, selectedSkill *SelectedSkill, sink core.StreamSink) (*Result, error) {
	switch {
	case state.failure != nil:
		return e.finishFailedRun(ctx, runID, input, state, selectedSkill, sink)
	case state.interrupt != nil:
		return e.finishInterruptedRun(ctx, runID, state)
	default:
		return e.finishSucceededRun(ctx, runID, input, state, selectedSkill, sink)
	}
}

func (e *Executor) finishFailedRun(ctx context.Context, runID, input string, state RunState, selectedSkill *SelectedSkill, sink core.StreamSink) (*Result, error) {
	durableCtx := core.DurableContext(ctx)
	if !state.emittedRunFailed && state.failure != nil {
		if err := e.emitRunFailed(durableCtx, runID, sink, state.failure.Error()); err != nil {
			return nil, err
		}
	}
	if err := e.store.FinishRun(durableCtx, runID, core.RunStatusFailed, state.lastOutput, state.failure.Error()); err != nil {
		return nil, err
	}
	if err := e.verifyAndRecordSkill(durableCtx, runID, selectedSkill, core.RunStatusFailed, state.lastOutput, sink); err != nil {
		return nil, err
	}
	// Distill + append history after the run is marked complete.
	go e.finalizePostRun(context.WithoutCancel(ctx), runID, core.RunStatusFailed, input, state.lastOutput)
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

func (e *Executor) finishSucceededRun(ctx context.Context, runID, input string, state RunState, selectedSkill *SelectedSkill, sink core.StreamSink) (*Result, error) {
	durableCtx := core.DurableContext(ctx)
	if err := e.store.UpdateRunOutput(durableCtx, runID, state.lastOutput); err != nil {
		return nil, err
	}
	if err := e.verifyAndRecordSkill(durableCtx, runID, selectedSkill, core.RunStatusSucceeded, state.lastOutput, sink); err != nil {
		return nil, err
	}
	if err := e.emitRunCompleted(durableCtx, runID, state.lastOutput, sink); err != nil {
		return nil, err
	}
	if err := e.store.FinishRun(durableCtx, runID, core.RunStatusSucceeded, state.lastOutput, ""); err != nil {
		return nil, err
	}
	// Distill + append history after the run is marked complete, so the
	// LLM call does not delay the completion event or status update.
	go e.finalizePostRun(context.WithoutCancel(ctx), runID, core.RunStatusSucceeded, input, state.lastOutput)
	return &Result{
		RunID:  runID,
		Status: core.RunStatusSucceeded,
		Output: state.lastOutput,
	}, nil
}

// finalizePostRun syncs the assistant message and appends a distilled history
// entry. Called asynchronously after the run is marked complete so the LLM
// distillation call does not delay run completion. Errors are logged, not
// propagated — the run is already finished.
func (e *Executor) finalizePostRun(ctx context.Context, runID string, runStatus core.RunStatus, input, output string) {
	if err := e.store.SyncAssistantMessageForRunStatus(ctx, runID, runStatus); err != nil {
		slog.Error("sync assistant message after run completion", "run_id", runID, "err", err)
		return
	}
	if err := e.appendRunHistory(ctx, runID, runStatus, input, output); err != nil {
		slog.Error("append run history", "run_id", runID, "err", err)
	}
	// Periodic memory review: counts completed runs and triggers a review
	// every N runs. nil reviewer = disabled.
	if e.reviewer != nil {
		e.reviewer.RecordRun(runID, input, output)
	}
}

func (e *Executor) appendRunHistory(ctx context.Context, runID string, runStatus core.RunStatus, input, output string) error {
	if e.runRuntime.MemoryModule() == nil {
		return errors.New("memory module is not initialized")
	}
	run, err := e.store.LoadRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("load run for memory history: %w", err)
	}
	summary := e.distillRunSummary(ctx, runStatus, input, output)
	if err := e.runRuntime.MemoryModule().AppendHistory(ctx, memory.HistoryEvent{
		SessionID: run.SessionID,
		RunID:     runID,
		Status:    string(runStatus),
		Summary:   summary,
		Timestamp: time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("append memory history: %w", err)
	}
	return nil
}

// distillRunSummary generates a structured summary of a completed run via a
// single LLM call. On any failure (model unavailable, API error, empty
// response) it falls back to a rune-safe truncation of input + output —
// never blocking run finalization.
// distillRunSummary produces the history summary for a completed run.
//
// Previously this called the LLM on every run, which was too expensive —
// most runs don't warrant a distillation call (simple Q&A, trivial
// triggers). The agent already has the `remember` tool to persist durable
// facts it deems worth keeping. History is an append-only event log, not
// a curated knowledge base; a rune-safe truncation is sufficient for
// recall, and the agent decides what's worth remembering long-term via
// the ambient loop's Record + Crystallize steps.
func (e *Executor) distillRunSummary(_ context.Context, status core.RunStatus, input, output string) string {
	combined := strings.TrimSpace(input + "\n\n" + output)
	return fallbackSummary(combined, status)
}

// fallbackSummary is the degradation path when LLM distillation is
// unavailable. It produces a rune-safe truncation (not byte-cut) so
// multi-byte CJK text is not split mid-character.
func fallbackSummary(combined string, status core.RunStatus) string {
	combined = strings.TrimSpace(combined)
	if combined == "" {
		return string(status)
	}
	const maxRunes = 500
	runes := []rune(combined)
	if len(runes) <= maxRunes {
		return string(status) + ": " + combined
	}
	return string(status) + ": " + string(runes[:maxRunes]) + "..."
}

func failureReasonForStatus(status core.RunStatus, output string) string {
	if status != core.RunStatusFailed {
		return ""
	}
	if strings.TrimSpace(output) == "" {
		return "run_failed"
	}
	return "run_failed:with_output"
}
