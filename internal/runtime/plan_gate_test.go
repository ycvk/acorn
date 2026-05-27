package runtime

import (
	"context"
	"path/filepath"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/model"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/runtime/graph"
	storesqlite "github.com/ycvk/acorn/internal/store/sqlite"
	"github.com/ycvk/acorn/internal/stream"
)

func TestAgentGraphAlwaysRunsPlanNode(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateRun(context.Background(), "run_plan_gate", "fix sqlite rows", "run_plan_gate"); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	runCtx := withRunID(runtimeapi.WithSessionID(ctx, "sess_plan_gate"), "run_plan_gate")

	toolCall := makeToolCall("call_1", "search", `{"query":"sqlite rows"}`)
	testModel := &toolCallingStubModel{
		responses: []*schema.Message{
			schema.AssistantMessage(`{"steps":[{"id":"s1","action":"Search sqlite rows","status":"pending"}]}`, nil),
			makeAssistantMessage(toolCall),
		},
	}
	tool := &stubTool{name: "search", description: "search things", result: "found"}
	safeNode, err := NewSafeParallelToolsNode(context.Background(), []einotool.BaseTool{tool}, fixedReadOnlyClassifier("search"))
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}
	runCtx = safeParallelLifecycleContextFromWithLedger(t, runCtx, safeNode, store)
	info, err := tool.Info(ctx)
	if err != nil {
		t.Fatalf("tool Info: %v", err)
	}
	runnable, err := BuildAgentGraph(runCtx, "test-agent", testModel, safeNode, newDirectAssistantStreamer(nil), 10, store, nil, NewPlanStore(store), "Make a plan", nil, []string{info.Name}, nil)
	if err != nil {
		t.Fatalf("buildAgentGraph: %v", err)
	}

	if _, err := runnable.Invoke(runCtx, &graph.AgentGraphInput{Messages: []*schema.Message{schema.UserMessage("fix sqlite rows")}}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	plan, err := store.LoadPlanBySession(ctx, "sess_plan_gate")
	if err != nil {
		t.Fatalf("LoadPlanBySession: %v", err)
	}
	if len(plan.Steps) != 1 {
		t.Fatalf("step count = %d, want 1", len(plan.Steps))
	}
	if got, want := plan.Steps[0].Action, "Search sqlite rows"; got != want {
		t.Fatalf("step action = %q, want %q", got, want)
	}
	if string(plan.Steps[0].Status) != string(model.PlanStepCompleted) {
		t.Fatalf("step status = %q, want completed", plan.Steps[0].Status)
	}
	if testModel.callCount != 2 {
		t.Fatalf("model callCount = %d, want 2", testModel.callCount)
	}
	records, err := store.LoadEvents(ctx, "run_plan_gate")
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	for _, record := range records {
		if record.Kind == string(stream.StreamKindPlanCreated) {
			return
		}
	}
	t.Fatalf("expected %s event", stream.StreamKindPlanCreated)
}

func TestAgentGraphNoExistingPlanRunsPlanNode(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateRun(context.Background(), "run_plan_gate_empty", "read README", "run_plan_gate_empty"); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	runCtx := withRunID(runtimeapi.WithSessionID(ctx, "sess_plan_gate_empty"), "run_plan_gate_empty")

	toolCall := makeToolCall("call_1", "search", `{"query":"README"}`)
	testModel := &toolCallingStubModel{
		responses: []*schema.Message{
			schema.AssistantMessage(`{"steps":[{"id":"s1","action":"Read README","status":"pending"}]}`, nil),
			makeAssistantMessage(toolCall),
		},
	}
	tool := &stubTool{name: "search", description: "search things", result: "README"}
	safeNode, err := NewSafeParallelToolsNode(context.Background(), []einotool.BaseTool{tool}, fixedReadOnlyClassifier("search"))
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}
	runCtx = safeParallelLifecycleContextFromWithLedger(t, runCtx, safeNode, store)
	info, err := tool.Info(ctx)
	if err != nil {
		t.Fatalf("tool Info: %v", err)
	}
	runnable, err := BuildAgentGraph(runCtx, "test-agent", testModel, safeNode, newDirectAssistantStreamer(nil), 10, store, nil, NewPlanStore(store), "Make a plan", nil, []string{info.Name}, nil)
	if err != nil {
		t.Fatalf("buildAgentGraph: %v", err)
	}

	if _, err := runnable.Invoke(runCtx, &graph.AgentGraphInput{Messages: []*schema.Message{schema.UserMessage("read README")}}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if testModel.callCount != 2 {
		t.Fatalf("model callCount = %d, want 2", testModel.callCount)
	}
	records, err := store.LoadEvents(ctx, "run_plan_gate_empty")
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	for _, record := range records {
		if record.Kind == string(stream.StreamKindPlanCreated) {
			return
		}
	}
	t.Fatalf("expected %s event when SOP matches are empty", stream.StreamKindPlanCreated)
}

func TestMemoryEvolutionFinalizationAppendsHistoryThenKeepsNormalPlanPath(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{})
	exec := newFinalizationTestExecutor(t, store, cfg)
	exec.runRuntime = factory
	runID := createFinalizationRun(t, ctx, store, "session-evolution", "fix sqlite rows")
	saveCompletedPlan(t, ctx, store, runID, "session-evolution")

	if _, err := exec.finishCollectedRun(ctx, runID, "fix sqlite rows", runState{lastOutput: "done"}, nil, nil); err != nil {
		t.Fatalf("finishCollectedRun: %v", err)
	}
	assertMemoryHistoryContains(t, cfg, "session-evolution", runID, "succeeded", "fix sqlite rows done")

	if err := store.CreateRun(context.Background(), "run_evolution_second", "fix sqlite rows again", "run_evolution_second"); err != nil {
		t.Fatalf("CreateRun second: %v", err)
	}
	runCtx := withRunID(runtimeapi.WithSessionID(ctx, "session-evolution-second"), "run_evolution_second")
	toolCall := makeToolCall("call_1", "search", `{"query":"sqlite rows"}`)
	testModel := &toolCallingStubModel{responses: []*schema.Message{
		schema.AssistantMessage(`{"steps":[{"id":"s1","action":"Search sqlite rows again","status":"pending"}]}`, nil),
		makeAssistantMessage(toolCall),
	}}
	tool := &stubTool{name: "search", description: "search things", result: "found"}
	safeNode, err := NewSafeParallelToolsNode(context.Background(), []einotool.BaseTool{tool}, fixedReadOnlyClassifier("search"))
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}
	runCtx = safeParallelLifecycleContextFromWithLedger(t, runCtx, safeNode, store)
	info, err := tool.Info(ctx)
	if err != nil {
		t.Fatalf("tool Info: %v", err)
	}
	runnable, err := BuildAgentGraph(runCtx, "test-agent", testModel, safeNode, newDirectAssistantStreamer(nil), 10, store, nil, NewPlanStore(store), "Make a plan", nil, []string{info.Name}, nil)
	if err != nil {
		t.Fatalf("buildAgentGraph: %v", err)
	}
	if _, err := runnable.Invoke(runCtx, &graph.AgentGraphInput{Messages: []*schema.Message{schema.UserMessage("fix sqlite rows again")}}); err != nil {
		t.Fatalf("Invoke second: %v", err)
	}
	if testModel.callCount != 2 {
		t.Fatalf("second run model callCount = %d, want 2", testModel.callCount)
	}
	events, err := store.LoadEvents(ctx, "run_evolution_second")
	if err != nil {
		t.Fatalf("LoadEvents second: %v", err)
	}
	for _, event := range events {
		if event.Kind == string(stream.StreamKindPlanCreated) {
			return
		}
	}
	t.Fatalf("expected second run to emit %s", stream.StreamKindPlanCreated)
}

func saveCompletedPlan(t *testing.T, ctx context.Context, store *storesqlite.Store, runID string, sessionID string) {
	t.Helper()
	if err := store.SavePlan(ctx, &model.Plan{
		PlanID:    sessionID,
		SessionID: sessionID,
		RunID:     runID,
		Steps: []model.PlanStep{{
			ID:     "s1",
			Action: "Complete task",
			Status: model.PlanStepCompleted,
			Evidence: []model.PlanEvidence{{
				Summary: "tool proof",
			}},
		}},
	}); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}
}
