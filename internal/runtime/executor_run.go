package runtime

import (
	"context"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/events"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/stream"
)

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

func resolveRunID(req runtimeapi.ExecuteRequest) string {
	if id := strings.TrimSpace(req.RunID); id != "" {
		return id
	}
	return NewRunID()
}

func (e *Executor) prepareExecuteRequest(ctx context.Context, req runtimeapi.ExecuteRequest) (runtimeapi.ExecuteRequest, error) {
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

func (e *Executor) ExecuteMessages(ctx context.Context, req runtimeapi.ExecuteRequest, sink stream.StreamSink) (*Result, error) {
	runID := resolveRunID(req)
	req, err := e.prepareExecuteRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	mode := resolveRootOrchestrationMode(req)
	if err := e.createBoundRun(ctx, runID, req, mode); err != nil {
		return nil, err
	}
	runCtxBase, cleanup := e.newManagedRunContext(ctx, runID)
	defer cleanup()
	if err := e.emitRunStarted(ctx, runID, req.Input, sink); err != nil {
		return nil, err
	}
	active, err := e.buildExecuteRunner(runCtxBase, req, runID, mode, sink)
	if err != nil {
		return nil, e.failSetupOrErr(ctx, runID, err, sink)
	}
	defer active.Close()
	if err := e.persistSelectedSkillID(ctx, runID, active.SelectedSkill); err != nil {
		return nil, err
	}
	messages, err := e.bootstrapContextSessionMessages(ctx, req, runID, mode, active)
	if err != nil {
		return nil, e.failSetupOrErr(ctx, runID, err, sink)
	}
	iter := active.Runner.Run(e.executionContext(runCtxBase, runID, req, active, sink), messages, adk.WithCheckPointID(runID))
	return e.consume(ctx, runID, req.Input, iter, active.SelectedSkill, sink, active.ChatModel)
}

func (e *Executor) createBoundRun(ctx context.Context, runID string, req runtimeapi.ExecuteRequest) error {
	return e.store.CreateBoundRunWithParams(ctx, store.RunCreateParams{
		RunID:          runID,
		SessionID:      req.SessionID,
		TurnIndex:      req.TurnIndex,
		Input:          req.Input,
		BoundMessageID: req.BoundMessageID,
	})
}

// persistSelectedSkillID records the run's resolved skill id so resume can
// recover it without the deleted run_decisions table. The explicit skill id
// from req.SkillID is already persisted by createBoundRun; this updates it
// when selection resolves to a different (top recommended) skill or clears it.
func (e *Executor) persistSelectedSkillID(ctx context.Context, runID string, selected *SelectedSkill) error {
	resolved := ""
	if selected != nil {
		resolved = selected.Skill.ID
	}
	return e.store.UpdateRunSkillID(ctx, runID, resolved)
}

func (e *Executor) buildExecuteRunner(runCtxBase context.Context, req runtimeapi.ExecuteRequest, runID string, mode events.OrchestrationMode, sink stream.StreamSink) (*ActiveRunner, error) {
	return e.runRuntime.New(runCtxBase, RunnerBuildRequest{
		SessionID:         req.SessionID,
		RunID:             runID,
		Input:             req.Input,
		SkillID:           req.SkillID,
		AllowedToolNames:  append([]string(nil), req.AllowedToolNames...),
		Sink:              sink,
		OrchestrationMode: mode,
		ParentRunID:       req.ParentRunID,
	})
}

func (e *Executor) executionContext(runCtxBase context.Context, runID string, req runtimeapi.ExecuteRequest, active *ActiveRunner, sink stream.StreamSink) context.Context {
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

func buildExecutionContext(runCtxBase context.Context, runID, sessionID string, turnIndex int, sink stream.StreamSink) context.Context {
	runCtx := runtimeapi.WithRunID(runCtxBase, runID)
	runCtx = runtimeapi.WithSessionID(runCtx, sessionID)
	runCtx = runtimeapi.WithTurnIndex(runCtx, turnIndex)
	return stream.WithStreamSink(runCtx, sink)
}
