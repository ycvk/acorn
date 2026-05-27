package plan

import (
	"context"
	"path/filepath"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/model"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/runtime/graph"
	rtool "github.com/ycvk/acorn/internal/runtime/tool"
	"github.com/ycvk/acorn/internal/runtime/tooltest"
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
	runCtx := runtimeapi.WithRunID(runtimeapi.WithSessionID(ctx, "sess_plan_gate"), "run_plan_gate")

	toolCall := makeToolCall("call_1", "search", `{"query":"sqlite rows"}`)
	testModel := &toolCallingStubModel{
		responses: []*schema.Message{
			schema.AssistantMessage(`{"steps":[{"id":"s1","action":"Search sqlite rows","status":"pending"}]}`, nil),
			makeAssistantMessage(toolCall),
		},
	}
	tool := &stubTool{name: "search", description: "search things", result: "found"}
	safeNode, err := rtool.NewSafeParallelToolsNode(context.Background(), []einotool.BaseTool{tool}, fixedReadOnlyClassifier("search"))
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}
	runCtx = tooltest.WithLoadedTools(t, runCtx, []einotool.BaseTool{tool}, store)
	info, err := tool.Info(ctx)
	if err != nil {
		t.Fatalf("tool Info: %v", err)
	}
	runnable, err := BuildAgentGraph(runCtx, "test-agent", testModel, safeNode, rtool.NewDirectAssistantStreamer(nil), 10, store, nil, NewPlanStore(store), "Make a plan", nil, []string{info.Name}, nil)
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
	runCtx := runtimeapi.WithRunID(runtimeapi.WithSessionID(ctx, "sess_plan_gate_empty"), "run_plan_gate_empty")

	toolCall := makeToolCall("call_1", "search", `{"query":"README"}`)
	testModel := &toolCallingStubModel{
		responses: []*schema.Message{
			schema.AssistantMessage(`{"steps":[{"id":"s1","action":"Read README","status":"pending"}]}`, nil),
			makeAssistantMessage(toolCall),
		},
	}
	tool := &stubTool{name: "search", description: "search things", result: "README"}
	safeNode, err := rtool.NewSafeParallelToolsNode(context.Background(), []einotool.BaseTool{tool}, fixedReadOnlyClassifier("search"))
	if err != nil {
		t.Fatalf("NewSafeParallelToolsNode: %v", err)
	}
	runCtx = tooltest.WithLoadedTools(t, runCtx, []einotool.BaseTool{tool}, store)
	info, err := tool.Info(ctx)
	if err != nil {
		t.Fatalf("tool Info: %v", err)
	}
	runnable, err := BuildAgentGraph(runCtx, "test-agent", testModel, safeNode, rtool.NewDirectAssistantStreamer(nil), 10, store, nil, NewPlanStore(store), "Make a plan", nil, []string{info.Name}, nil)
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
