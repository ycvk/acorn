package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	storecore "github.com/ycvk/acorn/internal/store"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/runtimehistory"
	"github.com/ycvk/acorn/internal/tooling"
)

type fakeModeRoutingPlane struct {
	directErr      error
	planExecuteErr error
	singleAgentReq *orchestration.SingleAgentRequest
	planReq        *orchestration.PlanExecuteRequest
}

func (p fakeModeRoutingPlane) BuildDirectResponse(context.Context, orchestration.DirectResponseRequest) (*orchestration.RunAssembly, error) {
	return nil, p.directErr
}

func (p fakeModeRoutingPlane) BuildSingleAgent(_ context.Context, req orchestration.SingleAgentRequest) (*orchestration.RunAssembly, error) {
	if p.singleAgentReq != nil {
		*p.singleAgentReq = req
	}
	return &orchestration.RunAssembly{Runner: &adk.Runner{}}, nil
}

func (p fakeModeRoutingPlane) BuildPlanExecute(_ context.Context, req orchestration.PlanExecuteRequest) (*orchestration.RunAssembly, error) {
	if p.planReq != nil {
		*p.planReq = req
	}
	return nil, p.planExecuteErr
}

type directRoutingTestModel struct{}

func (m directRoutingTestModel) Generate(context.Context, []*schema.Message, ...einomodel.Option) (*schema.Message, error) {
	return schema.AssistantMessage("你好", nil), nil
}

func (m directRoutingTestModel) Stream(context.Context, []*schema.Message, ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("direct route should use generate")
}

func TestExecuteMessagesPersistsDirectResponseModeForGreeting(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{})
	exec, err := NewExecutorWithRunnerFactoryAndController(cfg, store, factory, nil)
	if err != nil {
		t.Fatalf("NewExecutorWithRunnerFactoryAndController: %v", err)
	}

	directErr := errors.New("direct response route selected")
	exec.runBuilder.(*RunnerFactory).deps.Orchestration = fakeModeRoutingPlane{directErr: directErr}
	exec.runBuilder.(*RunnerFactory).installRunChatModelBuilderForTest(func(context.Context, RunnerBuildRequest) (einomodel.BaseChatModel, error) {
		return directRoutingTestModel{}, nil
	})

	_, err = exec.ExecuteMessages(ctx, ExecuteRequest{
		RunID:    "run_direct_route",
		Input:    "你好",
		Messages: []adk.Message{},
	}, nil)
	if !errors.Is(err, directErr) {
		t.Fatalf("ExecuteMessages error = %v, want direct response route error %v", err, directErr)
	}

	run, err := store.LoadRun(ctx, "run_direct_route")
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if run.OrchestrationMode != events.ModeDirectResponse {
		t.Fatalf("root run mode = %q, want %q", run.OrchestrationMode, events.ModeDirectResponse)
	}
}

func TestExecuteMessagesDirectResponseExecutesToolLoop(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	lookup := &trackingTool{name: "lookup", result: "lookup result: acorn"}
	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{
		ExtraLocalTools: []einotool.BaseTool{lookup},
	})
	exec, err := NewExecutorWithRunnerFactoryAndController(cfg, store, factory, nil)
	if err != nil {
		t.Fatalf("NewExecutorWithRunnerFactoryAndController: %v", err)
	}

	toolCall := schema.ToolCall{
		ID: "call_1",
		Function: schema.FunctionCall{
			Name:      "lookup",
			Arguments: `{"query":"acorn"}`,
		},
	}
	model := &toolCallingStubModel{
		responses: []*schema.Message{
			makeAssistantMessage(toolCall),
			schema.AssistantMessage("lookup result: acorn", nil),
		},
	}
	exec.runBuilder.(*RunnerFactory).installRunChatModelBuilderForTest(func(context.Context, RunnerBuildRequest) (einomodel.BaseChatModel, error) {
		return model, nil
	})

	result, err := exec.ExecuteMessages(ctx, ExecuteRequest{
		Input:    "look up acorn",
		Messages: []adk.Message{schema.UserMessage("look up acorn")},
	}, nil)
	if err != nil {
		t.Fatalf("ExecuteMessages: %v", err)
	}
	records, err := store.LoadEvents(ctx, result.RunID)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if result.Output != "lookup result: acorn" {
		t.Fatalf("output = %q, want %q", result.Output, "lookup result: acorn")
	}
	if lookup.lastCallCount() != 1 {
		t.Fatalf("tool call count = %d, want 1", lookup.lastCallCount())
	}
	toolStartedIndex := -1
	toolSucceededIndex := -1
	messageIndex := -1
	completedIndex := -1
	for _, record := range records {
		if record.Kind == "plan.created" || strings.HasPrefix(record.Kind, "subagent.") {
			t.Fatalf("direct response emitted planning/subagent event: %s", record.Kind)
		}
		switch record.Kind {
		case "tool.call.started":
			if toolStartedIndex < 0 {
				toolStartedIndex = int(record.Sequence)
			}
		case "tool.call.succeeded":
			if toolSucceededIndex < 0 {
				toolSucceededIndex = int(record.Sequence)
			}
		case "agent.message":
			if messageIndex < 0 && strings.Contains(fmt.Sprint(record.Payload), "lookup result: acorn") {
				messageIndex = int(record.Sequence)
			}
		case "run.completed":
			if completedIndex < 0 {
				completedIndex = int(record.Sequence)
			}
		}
	}
	if toolStartedIndex <= 0 {
		t.Fatal("direct response did not emit tool.call.started")
	}
	if toolSucceededIndex <= toolStartedIndex {
		t.Fatalf("tool.call.succeeded sequence = %d, want after started %d", toolSucceededIndex, toolStartedIndex)
	}
	if messageIndex <= toolSucceededIndex {
		t.Fatalf("agent.message sequence = %d, want after tool success %d", messageIndex, toolSucceededIndex)
	}
	if completedIndex <= messageIndex {
		t.Fatalf("run.completed sequence = %d, want after agent.message %d", completedIndex, messageIndex)
	}
}

type pauseInterruptTool struct {
	name         string
	resumeCalled bool
}

func (t *pauseInterruptTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name}, nil
}

func (t *pauseInterruptTool) InvokableRun(ctx context.Context, _ string, _ ...einotool.Option) (string, error) {
	wasInterrupted, _, _ := einotool.GetInterruptState[string](ctx)
	if !wasInterrupted {
		return "", einotool.StatefulInterrupt(ctx, map[string]any{
			"kind":    "run_command_pause",
			"command": []string{t.name, "--dry-run"},
			"cwd":     "/repo",
			"message": "paused before execution",
		}, "pending")
	}
	t.resumeCalled = true
	return "pause resumed", nil
}

func TestResumeDirectResponseRebuildsContextSession(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	pauseTool := &pauseInterruptTool{name: "pause_tool"}
	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{
		ExtraLocalTools: []einotool.BaseTool{pauseTool},
	})
	exec, err := NewExecutorWithRunnerFactoryAndController(cfg, store, factory, nil)
	if err != nil {
		t.Fatalf("NewExecutorWithRunnerFactoryAndController: %v", err)
	}

	toolCall := schema.ToolCall{
		ID: "call_pause",
		Function: schema.FunctionCall{
			Name:      "pause_tool",
			Arguments: `{}`,
		},
	}
	model := &toolCallingStubModel{
		responses: []*schema.Message{
			makeAssistantMessage(toolCall),
			schema.AssistantMessage("done after pause", nil),
		},
	}
	exec.runBuilder.(*RunnerFactory).installRunChatModelBuilderForTest(func(context.Context, RunnerBuildRequest) (einomodel.BaseChatModel, error) {
		return model, nil
	})

	result, err := exec.ExecuteMessages(ctx, ExecuteRequest{
		RunID:    "run_direct_resume",
		Input:    "call pause tool",
		Messages: []adk.Message{schema.UserMessage("call pause tool")},
	}, nil)
	if err != nil {
		t.Fatalf("ExecuteMessages: %v", err)
	}
	if result.Status != events.RunStatusInterrupted {
		t.Fatalf("initial status = %s, want interrupted", result.Status)
	}
	records, err := store.LoadEvents(ctx, result.RunID)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	contexts, err := LatestRootInterruptContexts(records)
	if err != nil {
		t.Fatalf("LatestRootInterruptContexts: %v", err)
	}

	resumed, err := exec.ResumeWithTargets(ctx, result.RunID, map[string]any{
		contexts[0].ID: map[string]any{},
	}, nil)
	if err != nil {
		t.Fatalf("ResumeWithTargets: %v", err)
	}
	if resumed.Status != events.RunStatusSucceeded {
		t.Fatalf("resumed status = %s, want succeeded", resumed.Status)
	}
	if resumed.Output != "done after pause" {
		t.Fatalf("resumed output = %q, want done after pause", resumed.Output)
	}
	if !pauseTool.resumeCalled {
		t.Fatal("pause tool was not resumed")
	}
	if got, want := model.callCount, 2; got != want {
		t.Fatalf("model call count = %d, want %d", got, want)
	}
}

func TestResumeDirectResponseRunCommandPauseWithoutExtraPayload(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	pauseTool := &pauseInterruptTool{name: "pause_tool"}
	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{
		ExtraLocalTools: []einotool.BaseTool{pauseTool},
	})
	exec, err := NewExecutorWithRunnerFactoryAndController(cfg, store, factory, nil)
	if err != nil {
		t.Fatalf("NewExecutorWithRunnerFactoryAndController: %v", err)
	}

	toolCall := schema.ToolCall{
		ID: "call_pause",
		Function: schema.FunctionCall{
			Name:      "pause_tool",
			Arguments: `{}`,
		},
	}
	model := &toolCallingStubModel{
		responses: []*schema.Message{
			makeAssistantMessage(toolCall),
			schema.AssistantMessage("done after pause", nil),
		},
	}
	exec.runBuilder.(*RunnerFactory).installRunChatModelBuilderForTest(func(context.Context, RunnerBuildRequest) (einomodel.BaseChatModel, error) {
		return model, nil
	})

	result, err := exec.ExecuteMessages(ctx, ExecuteRequest{
		RunID:    "run_direct_pause_resume",
		Input:    "pause before command",
		Messages: []adk.Message{schema.UserMessage("pause before command")},
	}, nil)
	if err != nil {
		t.Fatalf("ExecuteMessages: %v", err)
	}
	if result.Status != events.RunStatusInterrupted {
		t.Fatalf("initial status = %s, want interrupted", result.Status)
	}
	records, err := store.LoadEvents(ctx, result.RunID)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	contexts, err := LatestRootInterruptContexts(records)
	if err != nil {
		t.Fatalf("LatestRootInterruptContexts: %v", err)
	}

	resumed, err := exec.ResumeWithTargets(ctx, result.RunID, map[string]any{
		contexts[0].ID: map[string]any{},
	}, nil)
	if err != nil {
		t.Fatalf("ResumeWithTargets: %v", err)
	}
	if resumed.Status != events.RunStatusSucceeded {
		t.Fatalf("resumed status = %s, want succeeded", resumed.Status)
	}
	if resumed.Output != "done after pause" {
		t.Fatalf("resumed output = %q, want done after pause", resumed.Output)
	}
	if !pauseTool.resumeCalled {
		t.Fatal("pause tool was not resumed")
	}
}

func TestExecuteMessagesPersistsExplicitPlanExecuteMode(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{})
	exec, err := NewExecutorWithRunnerFactoryAndController(cfg, store, factory, nil)
	if err != nil {
		t.Fatalf("NewExecutorWithRunnerFactoryAndController: %v", err)
	}

	exec.runBuilder.(*RunnerFactory).deps.Workspace = nil

	_, err = exec.ExecuteMessages(ctx, ExecuteRequest{
		RunID:             "run_plan_route",
		Input:             "修复 internal/runtime/executor_run.go 里的默认执行模式并跑 go test",
		Messages:          []adk.Message{},
		OrchestrationMode: events.ModePlanExecute,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "workspace contract is not initialized") {
		t.Fatalf("ExecuteMessages error = %v, want workspace contract failure", err)
	}

	run, err := store.LoadRun(ctx, "run_plan_route")
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if run.OrchestrationMode != events.ModePlanExecute {
		t.Fatalf("root run mode = %q, want %q", run.OrchestrationMode, events.ModePlanExecute)
	}
}

func TestBuildSingleAgentAssemblyInjectsMemoryReflection(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{})
	var captured orchestration.SingleAgentRequest
	factory.deps.Orchestration = fakeModeRoutingPlane{singleAgentReq: &captured}

	assembly, err := factory.buildSingleAgentAssembly(ctx, RunnerBuildRequest{
		RunID:             "run_single_memory",
		SessionID:         "session_single_memory",
		Input:             "inspect repo",
		OrchestrationMode: events.ModeSingleAgent,
	}, nil, directRoutingTestModel{}, nil)
	if err != nil {
		t.Fatalf("buildSingleAgentAssembly: %v", err)
	}
	if assembly == nil {
		t.Fatal("assembly is nil")
	}
	for _, want := range []string{"memory_read_file", "memory_replace_span", "status: unverified"} {
		if !strings.Contains(captured.InstructionSuffix, want) {
			t.Fatalf("instruction suffix missing %q:\n%s", want, captured.InstructionSuffix)
		}
	}
}

func TestBuildPlanExecuteAssemblyInjectsMemoryReflection(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{})
	var captured orchestration.PlanExecuteRequest
	routeErr := errors.New("plan execute captured")
	factory.deps.Orchestration = fakeModeRoutingPlane{planExecuteErr: routeErr, planReq: &captured}

	_, err := factory.buildPlanExecuteAssembly(ctx, RunnerBuildRequest{
		RunID:             "run_plan_memory",
		SessionID:         "session_plan_memory",
		Input:             "fix code",
		OrchestrationMode: events.ModePlanExecute,
	}, nil, directRoutingTestModel{}, nil)
	if !errors.Is(err, routeErr) {
		t.Fatalf("buildPlanExecuteAssembly error = %v, want %v", err, routeErr)
	}
	for _, want := range []string{"memory_read_file", "memory_replace_span", "status: unverified"} {
		if !strings.Contains(captured.InstructionSuffix, want) {
			t.Fatalf("instruction suffix missing %q:\n%s", want, captured.InstructionSuffix)
		}
	}
}

func TestResumeWithTargetsRoutesPlanExecuteRunByPersistedMode(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{})
	routeErr := errors.New("plan execute route selected")
	factory.deps.Orchestration = fakeModeRoutingPlane{planExecuteErr: routeErr}

	exec, err := NewExecutorWithRunnerFactoryAndController(cfg, store, factory, nil)
	if err != nil {
		t.Fatalf("NewExecutorWithRunnerFactoryAndController: %v", err)
	}

	runID := "run_resume_mode"
	if err := store.CreateRunWithParams(context.Background(), storecore.RunCreateParams{
		RunID:             runID,
		SessionID:         "session_resume",
		Input:             "resume plan execute",
		CheckpointID:      runID,
		OrchestrationMode: events.ModePlanExecute,
	}); err != nil {
		t.Fatalf("CreateRunWithParams: %v", err)
	}
	if err := store.MarkInterruptedContext(context.Background(), runID, "partial"); err != nil {
		t.Fatalf("MarkInterrupted: %v", err)
	}
	if err := store.SaveRunDecision(ctx, decision.Record{
		RunID:     runID,
		SessionID: "session_resume",
		Action:    decision.ActionExecuteWithoutSkill,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveRunDecision: %v", err)
	}
	if err := store.SaveRunContextSnapshot(ctx, runtimehistory.RunContextSnapshot{
		RunID:     runID,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveRunContextSnapshot: %v", err)
	}

	_, err = exec.ResumeWithTargets(ctx, runID, map[string]any{}, nil)
	if !errors.Is(err, routeErr) {
		t.Fatalf("ResumeWithTargets error = %v, want %v", err, routeErr)
	}
}

func TestResolveRunSelectionRejectsResumeRunDecision(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{})
	decisionRoot := t.TempDir()
	profile := decision.Profile{
		Defaults: decision.Defaults{
			MissingContext:            decision.ActionInspectFirst,
			MissingRequiredCapability: decision.ActionBlock,
		},
		Routes: []decision.Route{
			{Intent: "general", Action: decision.ActionResumeRun},
		},
	}
	raw, err := decision.RenderProfileMarkdown(profile)
	if err != nil {
		t.Fatalf("RenderProfileMarkdown: %v", err)
	}
	if err := os.WriteFile(filepath.Join(decisionRoot, "decision.md"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write decision.md: %v", err)
	}
	factory.deps.DecisionProfiles = decision.NewProfileService(decisionRoot)

	catalog, err := tooling.NewCatalog(ctx, nil)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	caps := &runCapabilities{catalog: catalog}
	selection, err := factory.resolveRunSelection(ctx, RunnerBuildRequest{
		RunID:     "run_resume_decision",
		SessionID: "session_resume_decision",
		Input:     "hello",
	}, caps)
	if err == nil {
		t.Fatalf("resolveRunSelection returned selection %+v, want resume_run rejection", selection)
	}
	if !strings.Contains(err.Error(), "decision resolved to resume_run for a new execution") {
		t.Fatalf("resolveRunSelection error = %v, want resume_run rejection", err)
	}
}

func TestResumeWithTargetsRejectsRemovedWorkflowMode(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{})
	factory.installRunChatModelBuilderForTest(func(context.Context, RunnerBuildRequest) (einomodel.BaseChatModel, error) {
		return nil, errors.New("removed workflow mode should fail before model build")
	})

	exec, err := NewExecutorWithRunnerFactoryAndController(cfg, store, factory, nil)
	if err != nil {
		t.Fatalf("NewExecutorWithRunnerFactoryAndController: %v", err)
	}

	runID := "run_resume_workflow_mode"
	if err := store.CreateRunWithParams(context.Background(), storecore.RunCreateParams{
		RunID:             runID,
		SessionID:         "session_workflow",
		Input:             "resume workflow",
		CheckpointID:      runID,
		OrchestrationMode: events.OrchestrationMode("workflow"),
	}); err != nil {
		t.Fatalf("CreateRunWithParams: %v", err)
	}
	if err := store.MarkInterruptedContext(context.Background(), runID, "partial"); err != nil {
		t.Fatalf("MarkInterrupted: %v", err)
	}
	if err := store.SaveRunDecision(ctx, decision.Record{
		RunID:     runID,
		SessionID: "session_workflow",
		Action:    decision.ActionExecuteWithoutSkill,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveRunDecision: %v", err)
	}
	if err := store.SaveRunContextSnapshot(ctx, runtimehistory.RunContextSnapshot{
		RunID:     runID,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveRunContextSnapshot: %v", err)
	}

	_, err = exec.ResumeWithTargets(ctx, runID, map[string]any{}, nil)
	if err == nil || !strings.Contains(err.Error(), `unsupported orchestration mode "workflow"`) {
		t.Fatalf("ResumeWithTargets error = %v, want unsupported workflow mode", err)
	}
}

func TestSubagentExecuteUsesRealChildRunIDInEvents(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	parentRunID := createFinalizationRun(t, ctx, store, "session-parent", "inspect repo")
	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{})
	sub := NewSubagentExecutor(cfg, store, factory, nil)

	_, err := sub.Execute(ctx, orchestration.ChildAgentRequest{
		ParentRunID:        parentRunID,
		ParentSessionID:    "session-parent",
		ParentStepID:       "s1",
		Task:               "inspect repo",
		RequestedMode:      events.ModeSingleAgent,
		AcceptanceCriteria: []string{"completed"},
	})
	if err == nil {
		t.Fatal("expected provider-backed execution to fail in test environment")
	}

	raw, err := store.LoadEvents(ctx, parentRunID)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	var started *SubagentStartedPayload
	var failed *SubagentFailedPayload
	for _, record := range raw {
		item := projectEventToStreamItem(record)
		switch payload := item.Payload.(type) {
		case *SubagentStartedPayload:
			started = payload
		case *SubagentFailedPayload:
			failed = payload
		}
	}
	if started == nil || failed == nil {
		t.Fatalf("expected started and failed subagent payloads, got started=%v failed=%v", started, failed)
	}
	if started.SubRunID == "" || failed.SubRunID == "" {
		t.Fatalf("expected real child run ids, got started=%+v failed=%+v", started, failed)
	}
	if started.SubRunID != failed.SubRunID {
		t.Fatalf("child run ids diverged: started=%q failed=%q", started.SubRunID, failed.SubRunID)
	}
	if failed.ParentStepID != "s1" || failed.OrchestrationMode != "single_agent" {
		t.Fatalf("failed payload missing step/mode truth: %+v", failed)
	}
	if started.WorkspaceMode != string(orchestration.ChildWorkspaceModeShared) || failed.WorkspaceMode != string(orchestration.ChildWorkspaceModeShared) {
		t.Fatalf("expected shared workspace mode, got started=%+v failed=%+v", started, failed)
	}
}

type progressEmittingTool struct{}

func (progressEmittingTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "progress_tool"}, nil
}

func (progressEmittingTool) InvokableRun(context.Context, string, ...einotool.Option) (string, error) {
	return "done", nil
}

func (progressEmittingTool) InvokableRunWithProgress(ctx context.Context, _ string, emit tooling.ToolProgressEmitter, _ ...einotool.Option) (string, error) {
	if emit != nil {
		for i := 1; i <= 3; i++ {
			if err := emit(ctx, tooling.ToolProgressEvent{Delta: fmt.Sprintf("chunk-%d", i)}); err != nil {
				return "", err
			}
		}
	}
	return "done", nil
}

func TestExecuteMessagesDirectResponseEmitsToolProgress(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	progressTool := &progressEmittingTool{}
	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{
		ExtraLocalTools: []einotool.BaseTool{progressTool},
	})
	exec, err := NewExecutorWithRunnerFactoryAndController(cfg, store, factory, nil)
	if err != nil {
		t.Fatalf("NewExecutorWithRunnerFactoryAndController: %v", err)
	}

	toolCall := schema.ToolCall{
		ID: "call_progress",
		Function: schema.FunctionCall{
			Name:      "progress_tool",
			Arguments: `{}`,
		},
	}
	model := &toolCallingStubModel{
		responses: []*schema.Message{
			makeAssistantMessage(toolCall),
			schema.AssistantMessage("result: done", nil),
		},
	}
	exec.runBuilder.(*RunnerFactory).installRunChatModelBuilderForTest(func(context.Context, RunnerBuildRequest) (einomodel.BaseChatModel, error) {
		return model, nil
	})

	result, err := exec.ExecuteMessages(ctx, ExecuteRequest{
		Input:    "run progress tool",
		Messages: []adk.Message{schema.UserMessage("run progress tool")},
	}, nil)
	if err != nil {
		t.Fatalf("ExecuteMessages: %v", err)
	}
	records, err := store.LoadEvents(ctx, result.RunID)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}

	var startedSeq, progressSeqs, succeededSeq int
	for _, r := range records {
		switch r.Kind {
		case "tool.call.started":
			startedSeq = int(r.Sequence)
		case "tool.call.progress":
			progressSeqs++
		case "tool.call.succeeded":
			succeededSeq = int(r.Sequence)
		}
	}
	if startedSeq == 0 {
		t.Fatal("expected tool.call.started event")
	}
	if progressSeqs != 3 {
		t.Fatalf("expected 3 tool.call.progress events, got %d", progressSeqs)
	}
	if succeededSeq == 0 {
		t.Fatal("expected tool.call.succeeded event")
	}
	if succeededSeq <= startedSeq {
		t.Fatalf("succeeded sequence %d should be after started %d", succeededSeq, startedSeq)
	}
}
