package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/events"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/stream"
)

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
	return e.executeResume(ctx, runCtxBase, *run, runID, targets, sink)
}

func (e *Executor) executeResume(ctx context.Context, runCtxBase context.Context, run events.RunRecord, runID string, targets map[string]any, sink stream.StreamSink) (*Result, error) {
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

func (e *Executor) resumeIter(runCtxBase context.Context, run events.RunRecord, runID string, active *ActiveRunner, targets map[string]any, sink stream.StreamSink) (*adk.AsyncIterator[*adk.AgentEvent], error) {
	executionCtx := contextplane.WithContextSession(
		buildExecutionContext(runCtxBase, runID, run.SessionID, run.TurnIndex, sink), active.ContextSession)
	iter, err := active.Runner.ResumeWithParams(executionCtx, runID, &adk.ResumeParams{Targets: targets})
	if err != nil {
		return nil, fmt.Errorf("resume run %s: %w", runID, err)
	}
	return iter, nil
}

func (e *Executor) bootstrapResumeContextSession(ctx context.Context, run events.RunRecord, runID string, active *ActiveRunner) error {
	if active.ContextSession != nil {
		return nil
	}
	messages := []adk.Message{}
	if strings.TrimSpace(run.Input) != "" {
		messages = []adk.Message{schema.UserMessage(run.Input)}
	}
	_, err := e.bootstrapContextSessionMessages(ctx, runtimeapi.ExecuteRequest{
		SessionID: run.SessionID,
		TurnIndex: run.TurnIndex,
		Input:     run.Input,
		Messages:  messages,
	}, runID, events.ModeDirectResponse, active)
	return err
}
