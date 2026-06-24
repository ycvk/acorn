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
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/stream"
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
	runRuntime        *RunnerFactory
	controller        *RunController
	newChatModel      func(ctx context.Context) (einomodel.BaseChatModel, error)
	sessionSummarySvc *domain.SessionSummaryService
}

func NewExecutorWithRunRuntimeAndController(cfg *config.Config, store ExecutorStore, runRuntime *RunnerFactory, controller *RunController) (*Executor, error) {
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
func (e *Executor) Run(ctx context.Context, input, skillID string, sink domain.StreamSink) (*Result, error) {
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

func (e *Executor) ExecuteMessages(ctx context.Context, req domain.ExecuteRequest, sink domain.StreamSink) (*Result, error) {
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

func (e *Executor) buildExecuteRunner(runCtxBase context.Context, req domain.ExecuteRequest, runID string, sink domain.StreamSink) (*ActiveRunner, error) {
	return e.runRuntime.New(runCtxBase, RunnerBuildRequest{
		SessionID:        req.SessionID,
		RunID:            runID,
		Input:            req.Input,
		SkillID:          req.SkillID,
		AllowedToolNames: append([]string(nil), req.AllowedToolNames...),
		Sink:             sink,
	})
}

func (e *Executor) executionContext(runCtxBase context.Context, runID string, req domain.ExecuteRequest, active *ActiveRunner, sink domain.StreamSink) context.Context {
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

func buildExecutionContext(runCtxBase context.Context, runID, sessionID string, turnIndex int, sink domain.StreamSink) context.Context {
	runCtx := domain.WithRunID(runCtxBase, runID)
	runCtx = domain.WithSessionID(runCtx, sessionID)
	runCtx = domain.WithTurnIndex(runCtx, turnIndex)
	return domain.WithStreamSink(runCtx, sink)
}

func (e *Executor) ResumeWithTargets(ctx context.Context, runID string, targets map[string]any, sink domain.StreamSink) (*Result, error) {
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

func (e *Executor) executeResume(ctx context.Context, runCtxBase context.Context, run domain.RunRecord, runID string, targets map[string]any, sink domain.StreamSink) (*Result, error) {
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

func (e *Executor) resumeIter(runCtxBase context.Context, run domain.RunRecord, runID string, active *ActiveRunner, targets map[string]any, sink domain.StreamSink) (*adk.AsyncIterator[*adk.AgentEvent], error) {
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

func (e *Executor) consume(ctx context.Context, runID, input string, iter *adk.AsyncIterator[*adk.AgentEvent], selectedSkill *SelectedSkill, sink domain.StreamSink, chatModel einomodel.BaseChatModel) (*Result, error) {
	state, err := e.collectRunState(ctx, runID, iter, sink, chatModel)
	if err != nil {
		return nil, err
	}
	return e.finishCollectedRun(ctx, runID, input, state, selectedSkill, sink)
}

func (e *Executor) collectRunState(ctx context.Context, runID string, iter *adk.AsyncIterator[*adk.AgentEvent], sink domain.StreamSink, chatModel einomodel.BaseChatModel) (RunState, error) {
	state := RunState{}
	for {
		event, ok := iter.Next()
		if !ok {
			return state, nil
		}
		if err := e.applyAgentEvent(ctx, runID, stream.StreamItemsFromAgentEvent(event, chatModel), sink, &state); err != nil {
			return RunState{}, err
		}
	}
}

func (e *Executor) applyAgentEvent(ctx context.Context, runID string, items []domain.StreamItem, sink domain.StreamSink, state *RunState) error {
	for _, item := range items {
		item.RunID = runID
		if _, err := stream.AppendStreamItem(ctx, e.store, sink, item); err != nil {
			return err
		}
		state.applyStreamItem(item)
	}
	return nil
}

func (s *RunState) applyStreamItem(item domain.StreamItem) {
	if delta := domain.ItemGetAssistantDelta(item); delta != nil {
		s.lastOutput += delta.Delta
	}
	if msg := domain.ItemGetMessage(item); msg != nil && msg.Content != "" {
		s.lastOutput = msg.Content
	}
	if interrupt := domain.ItemGetInterrupt(item); interrupt != nil {
		s.interrupt = InterruptPayloadFromStream(interrupt)
	}
	if item.Kind == domain.StreamKindRunFailed && domain.ItemGetError(item) != "" {
		s.failure = errors.New(domain.ItemGetError(item))
		s.emittedRunFailed = true
	}
}

func (e *Executor) emitLifecyclePayload(ctx context.Context, runID string, sink domain.StreamSink, kind domain.StreamItemKind, payload map[string]any) error {
	_, err := stream.AppendStreamItem(ctx, e.store, sink, domain.StreamItem{
		RunID:     runID,
		Kind:      kind,
		CreatedAt: time.Now().UTC(),
		Payload:   payload,
	})
	return err
}

func (e *Executor) emitRunStarted(ctx context.Context, runID, input string, sink domain.StreamSink) error {
	return e.emitLifecyclePayload(ctx, runID, sink, domain.StreamKindRunStarted, map[string]any{"input": input})
}

func (e *Executor) emitRunResumeRequested(ctx context.Context, runID string, targets map[string]any, sink domain.StreamSink) error {
	return e.emitLifecyclePayload(ctx, runID, sink, domain.StreamKindRunResumeRequested, map[string]any{"targets": targets})
}

func (e *Executor) emitRunCompleted(ctx context.Context, runID, output string, sink domain.StreamSink) error {
	return e.emitLifecyclePayload(ctx, runID, sink, domain.StreamKindRunCompleted, map[string]any{"message": &domain.StreamMessage{
		Role:    string(schema.Assistant),
		Content: output,
	}})
}

func (e *Executor) emitRunFailed(ctx context.Context, runID string, sink domain.StreamSink, message string) error {
	return e.emitLifecyclePayload(ctx, runID, sink, domain.StreamKindRunFailed, map[string]any{"error": message})
}

func (e *Executor) failRunSetup(ctx context.Context, runID string, setupErr error, sink domain.StreamSink) error {
	if strings.TrimSpace(runID) == "" || setupErr == nil {
		return setupErr
	}
	durableCtx := DurableContext(ctx)
	if err := e.emitRunFailed(durableCtx, runID, sink, setupErr.Error()); err != nil {
		return err
	}
	return e.store.FinishRunContext(durableCtx, runID, domain.RunStatusFailed, "", setupErr.Error())
}

func (e *Executor) failSetupOrErr(ctx context.Context, runID string, setupErr error, sink domain.StreamSink) error {
	if failErr := e.failRunSetup(ctx, runID, setupErr, sink); failErr != nil {
		return failErr
	}
	return setupErr
}

func (e *Executor) recordFinalizationFailure(ctx context.Context, runID, output string, finalizationErr error, sink domain.StreamSink) error {
	durableCtx := DurableContext(ctx)
	message := fmt.Sprintf("run finalization failed: %v", finalizationErr)
	var errs []error
	if err := e.store.FinishRunContext(durableCtx, runID, domain.RunStatusFailed, output, message); err != nil {
		errs = append(errs, fmt.Errorf("mark run failed after finalization failure: %w", err))
	}
	if err := e.emitRunFailed(durableCtx, runID, sink, message); err != nil {
		errs = append(errs, fmt.Errorf("append finalization failure event: %w", err))
	}
	errs = append([]error{finalizationErr}, errs...)
	return errors.Join(errs...)
}

func (e *Executor) verifyAndRecordSkill(ctx context.Context, runID string, selected *SelectedSkill, status domain.RunStatus, output string, sink domain.StreamSink) error {
	if selected == nil || strings.TrimSpace(runID) == "" || status != domain.RunStatusFailed {
		return nil
	}
	_, err := stream.AppendStreamItem(ctx, e.store, sink, domain.StreamItem{
		RunID: runID,
		Kind:  domain.StreamKindSkillFailed,
		Payload: map[string]any{"skill": &domain.StreamSkill{
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

func (e *Executor) finishCollectedRun(ctx context.Context, runID, input string, state RunState, selectedSkill *SelectedSkill, sink domain.StreamSink) (*Result, error) {
	switch {
	case state.failure != nil:
		return e.finishFailedRun(ctx, runID, input, state, selectedSkill, sink)
	case state.interrupt != nil:
		return e.finishInterruptedRun(ctx, runID, state)
	default:
		return e.finishSucceededRun(ctx, runID, input, state, selectedSkill, sink)
	}
}

func (e *Executor) finishFailedRun(ctx context.Context, runID, input string, state RunState, selectedSkill *SelectedSkill, sink domain.StreamSink) (*Result, error) {
	durableCtx := DurableContext(ctx)
	if !state.emittedRunFailed && state.failure != nil {
		if err := e.emitRunFailed(durableCtx, runID, sink, state.failure.Error()); err != nil {
			return nil, err
		}
	}
	if err := e.store.FinishRunContext(durableCtx, runID, domain.RunStatusFailed, state.lastOutput, state.failure.Error()); err != nil {
		return nil, err
	}
	if err := e.verifyAndRecordSkill(durableCtx, runID, selectedSkill, domain.RunStatusFailed, state.lastOutput, sink); err != nil {
		return nil, err
	}
	if err := e.finalizePostRun(durableCtx, runID, domain.RunStatusFailed, input, state.lastOutput); err != nil {
		return nil, errors.Join(state.failure, fmt.Errorf("finalize failed run: %w", err))
	}
	return &Result{
		RunID:  runID,
		Status: domain.RunStatusFailed,
		Output: state.lastOutput,
		Error:  state.failure.Error(),
	}, nil
}

func (e *Executor) finishInterruptedRun(ctx context.Context, runID string, state RunState) (*Result, error) {
	durableCtx := DurableContext(ctx)
	if err := e.store.MarkInterruptedContext(durableCtx, runID, state.lastOutput); err != nil {
		return nil, err
	}
	return &Result{
		RunID:       runID,
		Status:      domain.RunStatusInterrupted,
		Output:      state.lastOutput,
		Interrupted: state.interrupt,
	}, nil
}

func (e *Executor) finishSucceededRun(ctx context.Context, runID, input string, state RunState, selectedSkill *SelectedSkill, sink domain.StreamSink) (*Result, error) {
	durableCtx := DurableContext(ctx)
	if err := e.store.UpdateRunOutputContext(durableCtx, runID, state.lastOutput); err != nil {
		return nil, err
	}
	if err := e.verifyAndRecordSkill(durableCtx, runID, selectedSkill, domain.RunStatusSucceeded, state.lastOutput, sink); err != nil {
		return nil, err
	}
	if err := e.finalizePostRun(durableCtx, runID, domain.RunStatusSucceeded, input, state.lastOutput); err != nil {
		return nil, e.recordFinalizationFailure(durableCtx, runID, state.lastOutput, err, sink)
	}
	if err := e.emitRunCompleted(durableCtx, runID, state.lastOutput, sink); err != nil {
		return nil, err
	}
	if err := e.store.FinishRunContext(durableCtx, runID, domain.RunStatusSucceeded, state.lastOutput, ""); err != nil {
		return nil, err
	}
	return &Result{
		RunID:  runID,
		Status: domain.RunStatusSucceeded,
		Output: state.lastOutput,
	}, nil
}

func (e *Executor) finalizePostRun(ctx context.Context, runID string, runStatus domain.RunStatus, input, output string) error {
	if err := e.store.SyncAssistantMessageForRunStatus(ctx, runID, runStatus); err != nil {
		return fmt.Errorf("sync assistant message: %w", err)
	}
	return e.appendRunHistory(ctx, runID, runStatus, input, output)
}

func (e *Executor) appendRunHistory(ctx context.Context, runID string, runStatus domain.RunStatus, input, output string) error {
	if e.runRuntime.MemoryModule() == nil {
		return errors.New("memory module is not initialized")
	}
	run, err := e.store.LoadRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("load run for memory history: %w", err)
	}
	if err := e.runRuntime.MemoryModule().AppendHistory(ctx, memory.HistoryEvent{
		SessionID: run.SessionID,
		RunID:     runID,
		Status:    string(runStatus),
		Summary:   compactArchiveText(strings.TrimSpace(input + " " + output)),
		Timestamp: time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("append memory history: %w", err)
	}
	return nil
}
func compactArchiveText(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) <= 280 {
		return trimmed
	}
	return trimmed[:280] + "..."
}

func failureReasonForStatus(status domain.RunStatus, output string) string {
	if status != domain.RunStatusFailed {
		return ""
	}
	if strings.TrimSpace(output) == "" {
		return "run_failed"
	}
	return "run_failed:with_output"
}
