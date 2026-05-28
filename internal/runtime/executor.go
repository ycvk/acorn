package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/model"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/stream"
)

type Result struct {
	RunID        string           `json:"run_id"`
	Status       events.RunStatus `json:"status"`
	Output       string           `json:"output,omitempty"`
	Error        string           `json:"error,omitempty"`
	Interrupted  map[string]any   `json:"interrupted,omitempty"`
	TraceSummary *TraceSummary    `json:"trace_summary,omitempty"`
}

type Executor struct {
	store             ExecutorStore
	runRuntime        RunRuntime
	controller        *RunController
	newChatModel      func(ctx context.Context) (einomodel.BaseChatModel, error)
	archiveRunFunc    func(ctx context.Context, runID string, runStatus events.RunStatus) error
	sessionSummarySvc *model.SessionSummaryService
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
		return nil, fmt.Errorf("%w: %v", runtimeapi.ErrExecutionNotReady, err)
	}
	exec := &Executor{
		store:             store,
		runRuntime:        runRuntime,
		controller:        controller,
		sessionSummarySvc: runRuntime.SessionSummarySvc(),
		newChatModel:      runRuntime.NewChatModel,
	}
	exec.archiveRunFunc = exec.archiveRun
	return exec, nil
}

func archiveSignalsFromEvents(records []events.EventRecord) ([]string, []string) {
	pathSet := make(map[string]struct{})
	toolSet := make(map[string]struct{})
	for _, record := range records {
		payload, ok := record.Payload.(map[string]any)
		if !ok {
			continue
		}
		toolName := strings.TrimSpace(ExtractString(payload["tool_name"]))
		if toolName == "" && strings.HasPrefix(record.Kind, "tool.call") {
			toolName = strings.TrimSpace(ExtractString(payload["name"]))
		}
		if toolName != "" {
			toolSet[toolName] = struct{}{}
		}
		if arguments := strings.TrimSpace(ExtractString(payload["arguments_json"])); arguments != "" {
			for _, path := range extractTouchedPaths(arguments) {
				pathSet[path] = struct{}{}
			}
		}
	}
	return sortedKeys(pathSet), sortedKeys(toolSet)
}

func extractTouchedPaths(argumentsJSON string) []string {
	if strings.TrimSpace(argumentsJSON) == "" {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(argumentsJSON), &payload); err != nil {
		return nil
	}
	paths := make([]string, 0, 2)
	for _, key := range []string{"path", "file_path", "target", "root_dir", "work_dir"} {
		value := strings.TrimSpace(ExtractString(payload[key]))
		if value != "" {
			paths = append(paths, value)
		}
	}
	return paths
}

func sortedKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func compactArchiveText(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) <= 280 {
		return trimmed
	}
	return trimmed[:280] + "..."
}

func (e *Executor) traceSummary(ctx context.Context, runID string) (*TraceSummary, error) {
	items, err := e.store.LoadEvents(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("load trace summary events: %w", err)
	}
	return BuildTraceSummary(items), nil
}

func (e *Executor) failRunSetup(ctx context.Context, runID string, setupErr error, sink stream.StreamSink) error {
	if strings.TrimSpace(runID) == "" || setupErr == nil {
		return setupErr
	}
	durableCtx := DurableContext(ctx)
	if err := e.emitRunFailed(durableCtx, runID, sink, setupErr.Error()); err != nil {
		return err
	}
	return e.store.FinishRunContext(durableCtx, runID, events.RunStatusFailed, "", setupErr.Error())
}

func (e *Executor) recordFinalizationFailure(ctx context.Context, runID, output string, finalizationErr error, sink stream.StreamSink) error {
	durableCtx := DurableContext(ctx)
	message := fmt.Sprintf("run finalization failed: %v", finalizationErr)
	var errs []error
	if err := e.store.FinishRunContext(durableCtx, runID, events.RunStatusFailed, output, message); err != nil {
		errs = append(errs, fmt.Errorf("mark run failed after finalization failure: %w", err))
	}
	if err := e.emitRunFailed(durableCtx, runID, sink, message); err != nil {
		errs = append(errs, fmt.Errorf("append finalization failure event: %w", err))
	}
	if _, err := e.traceSummary(durableCtx, runID); err != nil {
		errs = append(errs, err)
	}
	errs = append([]error{finalizationErr}, errs...)
	return errors.Join(errs...)
}

func (e *Executor) verifyAndRecordSkill(ctx context.Context, runID string, selected *SelectedSkill, status events.RunStatus, output string, sink stream.StreamSink) error {
	_ = ctx
	if selected == nil || strings.TrimSpace(runID) == "" || status != events.RunStatusFailed {
		return nil
	}
	_, err := stream.AppendStreamItem(ctx, e.store, sink, stream.StreamItem{
		RunID: runID,
		Kind:  stream.StreamKindSkillFailed,
		Payload: &stream.SkillFailedPayload{Skill: &stream.StreamSkill{
			SelectedID:    selected.Skill.ID,
			Name:          selected.Skill.Name,
			Source:        selected.Skill.Source,
			Path:          selected.Skill.Path,
			Summary:       selected.Skill.Summary,
			Requirements:  StreamSkillRequirementsFromDomain(selected.Skill.Requires),
			FailureReason: failureReasonForStatus(status, output),
		}},
	})
	if err != nil {
		return err
	}
	return nil
}

func failureReasonForStatus(status events.RunStatus, output string) string {
	if status != events.RunStatusFailed {
		return ""
	}
	if strings.TrimSpace(output) == "" {
		return "run_failed"
	}
	return "run_failed:with_output"
}

type RunState struct {
	lastOutput       string
	interrupt        map[string]any
	failure          error
	emittedRunFailed bool
}

func (e *Executor) consume(ctx context.Context, runID, input string, iter *adk.AsyncIterator[*adk.AgentEvent], selectedSkill *SelectedSkill, sink stream.StreamSink, chatModel einomodel.BaseChatModel) (*Result, error) {
	state, err := e.collectRunState(ctx, runID, iter, sink, chatModel)
	if err != nil {
		return nil, err
	}
	if err := e.runRuntime.ConsumeEventError(runID); err != nil {
		state.failure = err
	}
	if rc, ok := e.runRuntime.Registry().Get(runID); ok {
		rc.SetFinalizing()
	}
	return e.finishCollectedRun(ctx, runID, input, state, selectedSkill, sink)
}

func (e *Executor) collectRunState(ctx context.Context, runID string, iter *adk.AsyncIterator[*adk.AgentEvent], sink stream.StreamSink, chatModel einomodel.BaseChatModel) (RunState, error) {
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

func (e *Executor) prepareSkillExecution(ctx context.Context, runID string, selected *SelectedSkill, downstreamSink stream.StreamSink) (stream.StreamSink, error) {
	_ = ctx
	_ = runID
	_ = selected
	return downstreamSink, nil
}

func (e *Executor) applyAgentEvent(ctx context.Context, runID string, items []stream.StreamItem, sink stream.StreamSink, state *RunState) error {
	for _, item := range items {
		item.RunID = runID
		if _, err := stream.AppendStreamItem(ctx, e.store, sink, item); err != nil {
			return err
		}
		state.applyStreamItem(item)
	}
	return nil
}

func (s *RunState) applyStreamItem(item stream.StreamItem) {
	if delta := item.GetAssistantDelta(); delta != nil {
		s.lastOutput += delta.Delta
	}
	if msg := item.GetMessage(); msg != nil && msg.Content != "" {
		s.lastOutput = msg.Content
	}
	if interrupt := item.GetInterrupt(); interrupt != nil {
		s.interrupt = InterruptPayloadFromStream(interrupt)
	}
	if item.Kind == stream.StreamKindRunFailed && item.GetError() != "" {
		s.failure = errors.New(item.GetError())
		s.emittedRunFailed = true
	}
}

func (e *Executor) Run(ctx context.Context, input, skillID string, sink stream.StreamSink) (*Result, error) {
	sessionID := newSessionID()
	title, _ := compactText(input, 48)
	turnIndex, err := e.store.CreateFreshSessionTurn(ctx, sessionID, title, input)
	if err != nil {
		return nil, err
	}
	return e.ExecuteMessages(ctx, runtimeapi.ExecuteRequest{
		SessionID: sessionID,
		TurnIndex: turnIndex,
		Input:     input,
		SkillID:   skillID,
		Messages:  []adk.Message{schema.UserMessage(input)},
	}, sink)
}

func (e *Executor) ExecuteMessages(ctx context.Context, req runtimeapi.ExecuteRequest, sink stream.StreamSink) (*Result, error) {
	runID := strings.TrimSpace(req.RunID)
	if runID == "" {
		runID = newRunID()
	}
	if strings.TrimSpace(req.SessionID) == "" {
		req.SessionID = newSessionID()
		title, _ := compactText(req.Input, 48)
		turnIndex, err := e.store.CreateFreshSessionTurn(ctx, req.SessionID, title, req.Input)
		if err != nil {
			return nil, err
		}
		req.TurnIndex = turnIndex
		if len(req.Messages) == 0 && strings.TrimSpace(req.Input) != "" {
			req.Messages = []adk.Message{schema.UserMessage(req.Input)}
		}
	}
	mode := resolveRootOrchestrationMode(req)
	if err := e.store.CreateBoundRunWithParams(ctx, store.RunCreateParams{
		RunID:             runID,
		SessionID:         req.SessionID,
		TurnIndex:         req.TurnIndex,
		Input:             req.Input,
		CheckpointID:      runID,
		OrchestrationMode: mode,
		ParentRunID:       req.ParentRunID,
		Depth:             req.Depth,
	}); err != nil {
		return nil, err
	}

	runCtxBase, cleanup := e.newManagedRunContext(ctx, runID)
	defer cleanup()

	if err := e.emitRunStarted(ctx, runID, req.Input, sink); err != nil {
		return nil, err
	}

	active, err := e.runRuntime.New(runCtxBase, RunnerBuildRequest{
		SessionID:         req.SessionID,
		RunID:             runID,
		Input:             req.Input,
		SkillID:           req.SkillID,
		AllowedToolNames:  append([]string(nil), req.AllowedToolNames...),
		Sink:              sink,
		OrchestrationMode: mode,
		ParentRunID:       req.ParentRunID,
	})
	if err != nil {
		if failErr := e.failRunSetup(ctx, runID, err, sink); failErr != nil {
			return nil, failErr
		}
		return nil, err
	}
	defer active.Close()

	messages, err := e.bootstrapContextSessionMessages(ctx, req, runID, mode, active)
	if err != nil {
		if failErr := e.failRunSetup(ctx, runID, err, sink); failErr != nil {
			return nil, failErr
		}
		return nil, err
	}
	executionSink, err := e.prepareSkillExecution(ctx, runID, active.SelectedSkill, sink)
	if err != nil {
		return nil, err
	}

	executionCtx := buildExecutionContext(runCtxBase, runID, req.SessionID, req.TurnIndex, executionSink)
	if active.ContextSession != nil {
		executionCtx = contextplane.WithContextSession(executionCtx, active.ContextSession)
	}
	iter := active.Runner.Run(executionCtx, messages, adk.WithCheckPointID(runID))
	return e.consume(ctx, runID, req.Input, iter, active.SelectedSkill, executionSink, active.ChatModel)
}

func (e *Executor) ResumeWithTargets(ctx context.Context, runID string, targets map[string]any, sink stream.StreamSink) (*Result, error) {
	run, err := e.store.LoadRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != events.RunStatusInterrupted {
		return nil, fmt.Errorf("%w: %s", runtimeapi.ErrRunNotInterrupted, runID)
	}

	runCtxBase, cleanup := e.newManagedRunContext(ctx, runID)
	defer cleanup()

	if err := e.emitRunResumeRequested(ctx, runID, targets, sink); err != nil {
		return nil, err
	}

	active, err := e.runRuntime.New(runCtxBase, RunnerBuildRequest{
		SessionID:         run.SessionID,
		RunID:             runID,
		OrchestrationMode: run.OrchestrationMode,
		ParentRunID:       run.ParentRunID,
	})
	if err != nil {
		return nil, err
	}
	defer active.Close()

	executionSink, err := e.prepareSkillExecution(ctx, runID, active.SelectedSkill, sink)
	if err != nil {
		return nil, err
	}
	if active.ContextSession == nil {
		messages := []adk.Message{}
		if strings.TrimSpace(run.Input) != "" {
			messages = []adk.Message{schema.UserMessage(run.Input)}
		}
		if _, err := e.bootstrapContextSessionMessages(ctx, runtimeapi.ExecuteRequest{
			SessionID:         run.SessionID,
			TurnIndex:         run.TurnIndex,
			Input:             run.Input,
			Messages:          messages,
			OrchestrationMode: run.OrchestrationMode,
			ParentRunID:       run.ParentRunID,
			Depth:             run.Depth,
		}, runID, run.OrchestrationMode, active); err != nil {
			return nil, fmt.Errorf("bootstrap resume context session: %w", err)
		}
	}

	iter, err := active.Runner.ResumeWithParams(
		contextplane.WithContextSession(buildExecutionContext(runCtxBase, runID, run.SessionID, run.TurnIndex, executionSink), active.ContextSession),
		runID,
		&adk.ResumeParams{Targets: targets},
	)
	if err != nil {
		return nil, fmt.Errorf("resume run %s: %w", runID, err)
	}

	result, err := e.consume(ctx, runID, run.Input, iter, active.SelectedSkill, executionSink, active.ChatModel)
	if err != nil {
		return nil, err
	}
	if err := e.store.SyncAssistantMessageForRun(ctx, runID); err != nil {
		return nil, err
	}
	return result, nil
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

func buildExecutionContext(runCtxBase context.Context, runID, sessionID string, turnIndex int, sink stream.StreamSink) context.Context {
	runCtx := runtimeapi.WithRunID(runCtxBase, runID)
	runCtx = runtimeapi.WithSessionID(runCtx, sessionID)
	runCtx = withTurnIndex(runCtx, turnIndex)
	return stream.WithStreamSink(runCtx, sink)
}

func (e *Executor) emitLifecyclePayload(ctx context.Context, runID string, sink stream.StreamSink, payload stream.StreamPayload) error {
	if payload == nil {
		return errors.New("lifecycle payload is nil")
	}
	_, err := stream.AppendStreamItem(ctx, e.store, sink, stream.StreamItem{
		RunID:     runID,
		Kind:      payload.StreamKind(),
		CreatedAt: time.Now().UTC(),
		Payload:   payload,
	})
	return err
}

func (e *Executor) emitRunStarted(ctx context.Context, runID, input string, sink stream.StreamSink) error {
	return e.emitLifecyclePayload(ctx, runID, sink, &stream.RunStartedPayload{Input: input})
}

func (e *Executor) emitRunResumeRequested(ctx context.Context, runID string, targets map[string]any, sink stream.StreamSink) error {
	return e.emitLifecyclePayload(ctx, runID, sink, &stream.RunResumeRequestedPayload{Targets: targets})
}

func (e *Executor) emitRunCompleted(ctx context.Context, runID, output string, sink stream.StreamSink) error {
	return e.emitLifecyclePayload(ctx, runID, sink, &stream.RunCompletedPayload{
		Message: &stream.StreamMessage{
			Role:    string(schema.Assistant),
			Content: output,
		},
	})
}

func (e *Executor) emitRunFailed(ctx context.Context, runID string, sink stream.StreamSink, message string) error {
	return e.emitLifecyclePayload(ctx, runID, sink, &stream.RunFailedPayload{Error: message})
}

func (e *Executor) finishCollectedRun(ctx context.Context, runID, input string, state RunState, selectedSkill *SelectedSkill, sink stream.StreamSink) (*Result, error) {
	switch {
	case state.failure != nil:
		return e.finishFailedRun(ctx, runID, input, state, selectedSkill, sink)
	case state.interrupt != nil:
		return e.finishInterruptedRun(ctx, runID, state)
	default:
		return e.finishSucceededRun(ctx, runID, input, state, selectedSkill, sink)
	}
}

func (e *Executor) finishFailedRun(ctx context.Context, runID, input string, state RunState, selectedSkill *SelectedSkill, sink stream.StreamSink) (*Result, error) {
	durableCtx := DurableContext(ctx)
	if !state.emittedRunFailed && state.failure != nil {
		if err := e.emitRunFailed(durableCtx, runID, sink, state.failure.Error()); err != nil {
			return nil, err
		}
	}
	if err := e.store.FinishRunContext(durableCtx, runID, events.RunStatusFailed, state.lastOutput, state.failure.Error()); err != nil {
		return nil, err
	}
	if err := e.verifyAndRecordSkill(durableCtx, runID, selectedSkill, events.RunStatusFailed, state.lastOutput, sink); err != nil {
		return nil, err
	}
	if err := e.finalizePostRun(durableCtx, runID, events.RunStatusFailed, input, state.lastOutput); err != nil {
		return nil, errors.Join(state.failure, fmt.Errorf("finalize failed run: %w", err))
	}
	summary, err := e.traceSummary(durableCtx, runID)
	if err != nil {
		return nil, err
	}
	return &Result{
		RunID:        runID,
		Status:       events.RunStatusFailed,
		Output:       state.lastOutput,
		Error:        state.failure.Error(),
		TraceSummary: summary,
	}, nil
}

func (e *Executor) finishInterruptedRun(ctx context.Context, runID string, state RunState) (*Result, error) {
	durableCtx := DurableContext(ctx)
	if err := e.store.MarkInterruptedContext(durableCtx, runID, state.lastOutput); err != nil {
		return nil, err
	}
	summary, err := e.traceSummary(durableCtx, runID)
	if err != nil {
		return nil, err
	}
	return &Result{
		RunID:        runID,
		Status:       events.RunStatusInterrupted,
		Output:       state.lastOutput,
		Interrupted:  state.interrupt,
		TraceSummary: summary,
	}, nil
}

func (e *Executor) finishSucceededRun(ctx context.Context, runID, input string, state RunState, selectedSkill *SelectedSkill, sink stream.StreamSink) (*Result, error) {
	durableCtx := DurableContext(ctx)
	if err := e.store.UpdateRunOutputContext(durableCtx, runID, state.lastOutput); err != nil {
		return nil, err
	}
	if err := e.verifyAndRecordSkill(durableCtx, runID, selectedSkill, events.RunStatusSucceeded, state.lastOutput, sink); err != nil {
		return nil, err
	}
	if err := e.finalizePostRun(durableCtx, runID, events.RunStatusSucceeded, input, state.lastOutput); err != nil {
		return nil, e.recordFinalizationFailure(durableCtx, runID, state.lastOutput, err, sink)
	}
	if err := e.emitRunCompleted(durableCtx, runID, state.lastOutput, sink); err != nil {
		return nil, err
	}
	if err := e.store.FinishRunContext(durableCtx, runID, events.RunStatusSucceeded, state.lastOutput, ""); err != nil {
		return nil, err
	}
	summary, err := e.traceSummary(durableCtx, runID)
	if err != nil {
		return nil, err
	}
	return &Result{
		RunID:        runID,
		Status:       events.RunStatusSucceeded,
		Output:       state.lastOutput,
		TraceSummary: summary,
	}, nil
}

func (e *Executor) finalizePostRun(ctx context.Context, runID string, runStatus events.RunStatus, input, output string) error {
	if err := e.store.SyncAssistantMessageForRunStatus(ctx, runID, runStatus); err != nil {
		return fmt.Errorf("sync assistant message: %w", err)
	}
	if err := e.persistConversationSegment(ctx, runID, runStatus); err != nil {
		return err
	}
	if e.archiveRunFunc == nil {
		return errors.New("run archive finalizer is not initialized")
	}
	if err := e.archiveRunFunc(ctx, runID, runStatus); err != nil {
		return err
	}
	if err := e.appendRunHistory(ctx, runID, runStatus, input, output); err != nil {
		return err
	}
	return nil
}

func (e *Executor) persistConversationSegment(ctx context.Context, runID string, runStatus events.RunStatus) error {
	if _, err := e.store.CreateSegmentFromRun(ctx, runID, runStatus); err != nil {
		return fmt.Errorf("create conversation segment: %w", err)
	}
	return nil
}

func (e *Executor) appendRunHistory(ctx context.Context, runID string, runStatus events.RunStatus, input, output string) error {
	if e.runRuntime.MemoryModule() == nil {
		return errors.New("memory module is not initialized")
	}
	run, err := e.store.LoadRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("load run for memory history: %w", err)
	}
	archive, err := e.store.GetRunArchive(ctx, runID)
	if err != nil {
		return fmt.Errorf("load run archive for memory history: %w", err)
	}
	var filesChanged []string
	if archive != nil {
		filesChanged = append(filesChanged, archive.TouchedPaths...)
	}
	if err := e.runRuntime.MemoryModule().AppendHistory(ctx, memorymodule.HistoryEvent{
		SessionID:    run.SessionID,
		RunID:        runID,
		Status:       string(runStatus),
		Summary:      compactArchiveText(strings.TrimSpace(input + " " + output)),
		FilesChanged: filesChanged,
		Timestamp:    time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("append memory history: %w", err)
	}
	return nil
}

func (e *Executor) archiveRun(ctx context.Context, runID string, runStatus events.RunStatus) error {
	run, err := e.store.LoadRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("archive run: load run: %w", err)
	}
	records, err := e.store.LoadEvents(ctx, runID)
	if err != nil {
		return fmt.Errorf("archive run: load events: %w", err)
	}
	touchedPaths, toolNames := archiveSignalsFromEvents(records)
	archive := model.RunArchive{
		RunID:         run.RunID,
		SessionID:     run.SessionID,
		InputExcerpt:  compactArchiveText(run.Input),
		OutputExcerpt: compactArchiveText(run.Output),
		TouchedPaths:  touchedPaths,
		ToolNames:     toolNames,
		RunStatus:     string(runStatus),
		CreatedAt:     time.Now().UTC(),
	}
	if err := e.store.UpsertRunArchive(ctx, archive); err != nil {
		return fmt.Errorf("archive run: %w", err)
	}
	if e.sessionSummarySvc != nil && strings.TrimSpace(run.SessionID) != "" {
		if _, err := e.sessionSummarySvc.Update(ctx, run.SessionID, run.RunID, string(runStatus), buildSessionSummaryText(*run, toolNames)); err != nil {
			return fmt.Errorf("archive run: update session summary: %w", err)
		}
	}
	return nil
}

func buildSessionSummaryText(run events.RunRecord, toolNames []string) string {
	lines := []string{
		"Last request: " + compactArchiveText(run.Input),
		"Last outcome: " + firstNonEmpty(compactArchiveText(run.Output), compactArchiveText(run.Error), string(run.Status)),
	}
	if len(toolNames) > 0 {
		lines = append(lines, "Tools used: "+strings.Join(toolNames, ", "))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
