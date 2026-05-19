package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/events"
	storecore "github.com/ycvk/acorn/internal/store"
)

func (e *Executor) Run(ctx context.Context, input, skillID string, sink StreamSink) (*Result, error) {
	sessionID := newSessionID()
	title, _ := compactText(input, 48)
	turnIndex, err := e.store.CreateFreshSessionTurn(ctx, sessionID, title, input)
	if err != nil {
		return nil, err
	}
	return e.ExecuteMessages(ctx, ExecuteRequest{
		SessionID: sessionID,
		TurnIndex: turnIndex,
		Input:     input,
		SkillID:   skillID,
		Messages:  []adk.Message{schema.UserMessage(input)},
	}, sink)
}

func (e *Executor) ExecuteMessages(ctx context.Context, req ExecuteRequest, sink StreamSink) (*Result, error) {
	if e == nil || e.runnerFactory == nil || e.store == nil {
		return nil, errors.New("executor is not initialized")
	}

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
	if err := e.store.CreateBoundRunWithParams(ctx, storecore.RunCreateParams{
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

	active, err := e.runnerFactory.New(runCtxBase, RunnerBuildRequest{
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
	executionSink, err := e.prepareSkillExecution(ctx, runID, active.selectedSkill, sink)
	if err != nil {
		return nil, err
	}

	executionCtx := buildExecutionContext(runCtxBase, runID, req.SessionID, req.TurnIndex, executionSink)
	if active.contextSession != nil {
		executionCtx = contextplane.WithContextSession(executionCtx, active.contextSession)
	}
	iter := active.runner.Run(executionCtx, messages, adk.WithCheckPointID(runID))
	return e.consume(ctx, runID, req.Input, iter, active.selectedSkill, executionSink, active.chatModel)
}

func (e *Executor) ResumeWithTargets(ctx context.Context, runID string, targets map[string]any, sink StreamSink) (*Result, error) {
	if e == nil || e.runnerFactory == nil || e.store == nil {
		return nil, errors.New("executor is not initialized")
	}

	run, err := e.store.LoadRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != events.RunStatusInterrupted {
		return nil, fmt.Errorf("%w: %s", ErrRunNotInterrupted, runID)
	}

	runCtxBase, cleanup := e.newManagedRunContext(ctx, runID)
	defer cleanup()

	if err := e.emitRunResumeRequested(ctx, runID, targets, sink); err != nil {
		return nil, err
	}

	active, err := e.runnerFactory.New(runCtxBase, RunnerBuildRequest{
		SessionID:         run.SessionID,
		RunID:             runID,
		OrchestrationMode: run.OrchestrationMode,
		ParentRunID:       run.ParentRunID,
	})
	if err != nil {
		return nil, err
	}
	defer active.Close()

	executionSink, err := e.prepareSkillExecution(ctx, runID, active.selectedSkill, sink)
	if err != nil {
		return nil, err
	}
	if active.contextSession == nil {
		messages := []adk.Message{}
		if strings.TrimSpace(run.Input) != "" {
			messages = []adk.Message{schema.UserMessage(run.Input)}
		}
		if _, err := e.bootstrapContextSessionMessages(ctx, ExecuteRequest{
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

	iter, err := active.runner.ResumeWithParams(
		contextplane.WithContextSession(buildExecutionContext(runCtxBase, runID, run.SessionID, run.TurnIndex, executionSink), active.contextSession),
		runID,
		&adk.ResumeParams{Targets: targets},
	)
	if err != nil {
		return nil, fmt.Errorf("resume run %s: %w", runID, err)
	}

	result, err := e.consume(ctx, runID, run.Input, iter, active.selectedSkill, executionSink, active.chatModel)
	if err != nil {
		return nil, err
	}
	if err := e.store.SyncAssistantMessageForRun(ctx, runID); err != nil {
		return nil, err
	}
	return result, nil
}

func (e *Executor) newManagedRunContext(ctx context.Context, runID string) (context.Context, func()) {
	runTimeout := time.Duration(e.runnerFactory.cfg.Runtime.RunTimeoutSeconds) * time.Second
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

func buildExecutionContext(runCtxBase context.Context, runID, sessionID string, turnIndex int, sink StreamSink) context.Context {
	runCtx := withRunID(runCtxBase, runID)
	runCtx = withSessionID(runCtx, sessionID)
	runCtx = withTurnIndex(runCtx, turnIndex)
	return withStreamSink(runCtx, sink)
}
