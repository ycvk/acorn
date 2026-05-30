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

func TestPlanActObserveE2E(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.CreateRun(context.Background(), "run_e2e", "do two steps", "run_e2e"); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	runCtx := runtimeapi.WithRunID(runtimeapi.WithSessionID(ctx, "sess_e2e"), "run_e2e")
	sinkItems := make([]stream.StreamItem, 0)
	runCtx = stream.WithStreamSink(runCtx, func(item stream.StreamItem) error {
		sinkItems = append(sinkItems, item)
		return nil
	})

	toolCallA := makeToolCall("call_1", "search", `{"query":"alpha"}`)
	toolCallB := makeToolCall("call_2", "search", `{"query":"beta"}`)
	testModel := &toolCallingStubModel{
		responses: []*schema.Message{
			schema.AssistantMessage(`{"steps":[{"id":"s1","action":"Search alpha","status":"pending"},{"id":"s2","action":"Search beta","status":"pending","depends_on":["s1"]}]}`, nil),
			makeAssistantMessage(toolCallA),
			schema.AssistantMessage(`{"decision":"next","step_id":"s2","reason":"continue"}`, nil),
			makeAssistantMessage(toolCallB),
		},
	}
	tool := &stubTool{name: "search", description: "search things", result: "found"}
	safeNode, err := rtool.NewSafeParallelToolsNode(context.Background(), []einotool.BaseTool{tool}, fixedReadOnlyClassifier("search"))
	if err != nil {
		t.Fatalf("rtool.NewSafeParallelToolsNode: %v", err)
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

	if _, err := runnable.Invoke(runCtx, &graph.AgentGraphInput{Messages: []*schema.Message{schema.UserMessage("do two steps")}}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if _, err := stream.AppendStreamItem(runCtx, store, nil, stream.StreamItem{RunID: "run_e2e", Kind: stream.StreamKindRunCompleted, Payload: map[string]any{}}); err != nil {
		t.Fatalf("append run completed: %v", err)
	}

	plan, err := store.LoadPlanBySession(ctx, "sess_e2e")
	if err != nil {
		t.Fatalf("LoadPlanBySession: %v", err)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("step count = %d, want 2", len(plan.Steps))
	}
	for _, step := range plan.Steps {
		if string(step.Status) != string(model.PlanStepCompleted) {
			t.Fatalf("step %s status = %q, want completed", step.ID, step.Status)
		}
		if len(step.Evidence) != 1 {
			t.Fatalf("step %s evidence count = %d, want 1", step.ID, len(step.Evidence))
		}
	}

	events, err := store.LoadEvents(ctx, "run_e2e")
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	gotKinds := make([]string, 0, len(events))
	for _, event := range events {
		gotKinds = append(gotKinds, event.Kind)
	}
	for _, want := range []string{"run.completed"} {
		if !containsInOrder(&gotKinds, want) {
			t.Fatalf("event %q not found in order; remaining kinds=%v", want, gotKinds)
		}
	}
	if got, want := testModel.callCount, 4; got != want {
		t.Fatalf("model callCount = %d, want %d", got, want)
	}
	for _, item := range sinkItems {
		switch item.Kind {
		case "plan.created", "plan.updated", "plan.cleared", "step.started", "step.completed", "step.failed":
			t.Fatalf("plan/step execution should not emit stream sink diagnostic %q", item.Kind)
		}
	}
}

func TestPlanActObserveE2ESingleStep(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateRun(context.Background(), "run_single", "read README", "run_single"); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	runCtx := runtimeapi.WithRunID(runtimeapi.WithSessionID(ctx, "sess_single"), "run_single")

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
		t.Fatalf("rtool.NewSafeParallelToolsNode: %v", err)
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
	plan, err := store.LoadPlanBySession(ctx, "sess_single")
	if err != nil {
		t.Fatalf("LoadPlanBySession: %v", err)
	}
	if len(plan.Steps) != 1 {
		t.Fatalf("step count = %d, want 1", len(plan.Steps))
	}
	if testModel.callCount != 2 {
		t.Fatalf("model callCount = %d, want 2", testModel.callCount)
	}
}

func TestPlanActObserveE2EReplanConsumesOneAdditionalIteration(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateRun(context.Background(), "run_replan", "recover after failure", "run_replan"); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	runCtx := runtimeapi.WithRunID(runtimeapi.WithSessionID(ctx, "sess_replan"), "run_replan")

	secondToolCall := makeToolCall("call_2", "search", `{"query":"fixed"}`)
	testModel := &toolCallingStubModel{
		responses: []*schema.Message{
			schema.AssistantMessage(`{"steps":[{"id":"s1","action":"Try first path","status":"pending"},{"id":"s2","action":"Continue after first path","status":"pending","depends_on":["s1"]}]}`, nil),
			schema.AssistantMessage("unable to choose a tool", nil),
			schema.AssistantMessage(`{"decision":"replan","reason":"first path failed"}`, nil),
			schema.AssistantMessage(`{"steps":[{"id":"s1","action":"Try corrected path","status":"pending"}]}`, nil),
			makeAssistantMessage(secondToolCall),
		},
	}
	tool := &stubTool{name: "search", description: "search things", result: "ok"}
	safeNode, err := rtool.NewSafeParallelToolsNode(context.Background(), []einotool.BaseTool{tool}, fixedReadOnlyClassifier("search"))
	if err != nil {
		t.Fatalf("rtool.NewSafeParallelToolsNode: %v", err)
	}
	runCtx = tooltest.WithLoadedTools(t, runCtx, []einotool.BaseTool{tool}, store)
	info, err := tool.Info(ctx)
	if err != nil {
		t.Fatalf("tool Info: %v", err)
	}
	runnable, err := BuildAgentGraph(runCtx, "test-agent", testModel, safeNode, rtool.NewDirectAssistantStreamer(nil), 2, store, nil, NewPlanStore(store), "Make a plan", nil, []string{info.Name}, nil)
	if err != nil {
		t.Fatalf("buildAgentGraph: %v", err)
	}

	if _, err := runnable.Invoke(runCtx, &graph.AgentGraphInput{Messages: []*schema.Message{schema.UserMessage("recover")}}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	plan, err := store.LoadPlanBySession(ctx, "sess_replan")
	if err != nil {
		t.Fatalf("LoadPlanBySession: %v", err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Action != "Try corrected path" {
		t.Fatalf("regenerated plan = %+v", plan.Steps)
	}
	if string(plan.Steps[0].Status) != string(model.PlanStepCompleted) {
		t.Fatalf("regenerated step status = %q, want completed", plan.Steps[0].Status)
	}
}

func containsInOrder(kinds *[]string, want string) bool {
	for i, kind := range *kinds {
		if kind == want {
			*kinds = (*kinds)[i+1:]
			return true
		}
	}
	return false
}
