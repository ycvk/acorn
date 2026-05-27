package plan

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/orchestration"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/runtime/graph"
	"github.com/ycvk/acorn/internal/runtime/tool"
	"github.com/ycvk/acorn/internal/tooling"
)

type actNodeModel struct {
	response  *schema.Message
	callCount int
}

func (m *actNodeModel) Generate(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	m.callCount++
	return m.response, nil
}

func (m *actNodeModel) Stream(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

type fakeToolInvoker struct {
	results   []*schema.Message
	resultSeq [][]*schema.Message
	err       error
	callCount int
	lastInput *schema.Message
}

func (n *fakeToolInvoker) Invoke(_ context.Context, input *schema.Message, _ ...compose.ToolsNodeOption) ([]*schema.Message, error) {
	n.callCount++
	n.lastInput = input
	if len(n.resultSeq) >= n.callCount {
		return n.resultSeq[n.callCount-1], n.err
	}
	return n.results, n.err
}

func (n *fakeToolInvoker) Stream(ctx context.Context, input *schema.Message, opts ...compose.ToolsNodeOption) (*schema.StreamReader[[]*schema.Message], error) {
	results, err := n.Invoke(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([][]*schema.Message{results}), nil
}

func (n *fakeToolInvoker) NewStreamingExecutor(ctx context.Context) orchestration.StreamingExecutor {
	return &fakeToolStreamingExecutor{node: n, ctx: ctx}
}

type fakeToolStreamingExecutor struct {
	node  *fakeToolInvoker
	ctx   context.Context
	calls []schema.ToolCall
}

func (e *fakeToolStreamingExecutor) Submit(call schema.ToolCall) {
	e.calls = append(e.calls, call)
}

func (e *fakeToolStreamingExecutor) GetRemainingResults(ctx context.Context) ([]*schema.Message, error) {
	if len(e.calls) == 0 {
		return nil, nil
	}
	input := &schema.Message{
		Role:      schema.Assistant,
		ToolCalls: e.calls,
	}
	return e.node.Invoke(ctx, input)
}

func (e *fakeToolStreamingExecutor) Discard() {}

func TestActNodeStreamingPathCallsModelStream(t *testing.T) {
	toolCall := makeToolCall("call_stream", "read_file", `{"path":"README.md"}`)
	nodeModel := &actNodeModel{response: schema.AssistantMessage("", []schema.ToolCall{toolCall})}
	tools := &fakeToolInvoker{results: []*schema.Message{
		schema.ToolMessage("streaming works", "call_stream", schema.WithToolName("read_file")),
	}}
	store := &fakePlanStore{loaded: &model.Plan{
		PlanID:    "plan_stream",
		SessionID: "sess_stream",
		RunID:     "run_stream",
		Steps: []model.PlanStep{
			{ID: "s1", Action: "Stream test", Status: model.PlanStepPending},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	node := NewActNode(nodeModel, tools, tool.NewDirectAssistantStreamer(nil), store, nil, nil, nil)
	ctx := runtimeapi.WithRunID(runtimeapi.WithSessionID(context.Background(), "sess_stream"), "run_stream")

	out, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("test")}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if nodeModel.callCount != 1 {
		t.Fatalf("model call count = %d, want 1", nodeModel.callCount)
	}
	if tools.callCount != 1 {
		t.Fatalf("tools call count = %d, want 1", tools.callCount)
	}
	if out.Plan.Steps[0].Status != model.PlanStepCompleted {
		t.Fatalf("step status = %q, want completed", out.Plan.Steps[0].Status)
	}
	if len(out.Messages) != 3 {
		t.Fatalf("message count = %d, want 3", len(out.Messages))
	}
	if out.Messages[1].Role != schema.Assistant {
		t.Fatalf("messages[1].role = %q, want assistant", out.Messages[1].Role)
	}
	if len(out.Messages[1].ToolCalls) != 1 {
		t.Fatalf("assistant tool calls = %d, want 1", len(out.Messages[1].ToolCalls))
	}
	if out.Messages[2].Role != schema.Tool {
		t.Fatalf("messages[2].role = %q, want tool", out.Messages[2].Role)
	}
}

func TestActNodeCompletesNextPendingStep(t *testing.T) {
	toolCall := makeToolCall("call_1", "read_file", `{"path":"README.md"}`)
	nodeModel := &actNodeModel{response: schema.AssistantMessage("", []schema.ToolCall{toolCall})}
	tools := &fakeToolInvoker{results: []*schema.Message{
		schema.ToolMessage("README contents", "call_1", schema.WithToolName("read_file")),
	}}
	store := &fakePlanStore{loaded: &model.Plan{
		PlanID:    "plan_1",
		SessionID: "sess_act",
		RunID:     "run_act",
		Steps: []model.PlanStep{
			{ID: "s1", Action: "Read README", Status: model.PlanStepPending},
			{ID: "s2", Action: "Summarize README", Status: model.PlanStepPending, DependsOn: []string{"s1"}},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	node := NewActNode(nodeModel, tools, tool.NewDirectAssistantStreamer(nil), store, nil, nil, nil)
	ctx := runtimeapi.WithRunID(runtimeapi.WithSessionID(context.Background(), "sess_act"), "run_act")

	out, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("read")}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if nodeModel.callCount != 1 || tools.callCount != 1 {
		t.Fatalf("model/tools calls = %d/%d, want 1/1", nodeModel.callCount, tools.callCount)
	}
	if out.Plan.Steps[0].Status != model.PlanStepCompleted {
		t.Fatalf("step status = %q, want completed", out.Plan.Steps[0].Status)
	}
	if got := len(out.Plan.Steps[0].Evidence); got != 1 {
		t.Fatalf("evidence count = %d, want 1", got)
	}
	if out.Plan.Steps[0].Evidence[0].ToolName != "read_file" {
		t.Fatalf("tool_name = %q, want read_file", out.Plan.Steps[0].Evidence[0].ToolName)
	}
	if len(out.Messages) != 3 {
		t.Fatalf("message count = %d, want 3", len(out.Messages))
	}
}

func TestActNodeCompletesTestIntentStepWithPassedCommandEvidence(t *testing.T) {
	toolCall := makeToolCall("call_1", "run_command", `{"command":["go","test","./internal/runtime"]}`)
	nodeModel := &actNodeModel{response: schema.AssistantMessage("", []schema.ToolCall{toolCall})}
	tools := &fakeToolInvoker{results: []*schema.Message{
		schema.ToolMessage(`{"exit_code":0}`, "call_1", schema.WithToolName("run_command")),
	}}
	store := &fakePlanStore{loaded: &model.Plan{
		PlanID:    "plan_test",
		SessionID: "sess_test",
		RunID:     "run_test",
		Steps: []model.PlanStep{{
			ID:     "s1",
			Action: "Run runtime tests",
			Status: model.PlanStepPending,
			Risk:   model.PlanStepRiskExecute,
			VerificationIntent: []model.VerificationIntent{{
				Kind:    "test",
				Command: []string{"go", "test", "./internal/runtime"},
				Paths:   []string{"internal/runtime"},
				Reason:  "prove runtime still passes",
			}},
		}},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	node := NewActNode(nodeModel, tools, tool.NewDirectAssistantStreamer(nil), store, nil, nil, nil)
	ctx := runtimeapi.WithRunID(runtimeapi.WithSessionID(context.Background(), "sess_test"), "run_test")

	out, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("test runtime")}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out.Plan.Steps[0].Status != model.PlanStepCompleted {
		t.Fatalf("step status = %q, want completed", out.Plan.Steps[0].Status)
	}
	if got := len(out.Plan.Steps[0].Evidence); got < 2 {
		t.Fatalf("evidence count = %d, want >= 2", got)
	}
	found := false
	for _, item := range out.Plan.Steps[0].Evidence {
		if item.Kind == model.EvidenceKindTest && item.Status == model.EvidenceStatusPassed {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected passed test evidence, got %+v", out.Plan.Steps[0].Evidence)
	}
}

func TestActNodeFailsWhenVerificationIntentHasOnlyRecordedEvidence(t *testing.T) {
	toolCall := makeToolCall("call_1", "read_file", `{"path":"README.md"}`)
	nodeModel := &actNodeModel{response: schema.AssistantMessage("", []schema.ToolCall{toolCall})}
	tools := &fakeToolInvoker{results: []*schema.Message{
		schema.ToolMessage("README contents", "call_1", schema.WithToolName("read_file")),
	}}
	store := &fakePlanStore{loaded: &model.Plan{
		PlanID:    "plan_gap",
		SessionID: "sess_gap",
		RunID:     "run_gap",
		Steps: []model.PlanStep{{
			ID:     "s1",
			Action: "Verify with tests",
			Status: model.PlanStepPending,
			Risk:   model.PlanStepRiskExecute,
			VerificationIntent: []model.VerificationIntent{{
				Kind:   "test",
				Paths:  []string{"internal/runtime"},
				Reason: "need test coverage",
			}},
		}},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	node := NewActNode(nodeModel, tools, tool.NewDirectAssistantStreamer(nil), store, nil, nil, nil)
	ctx := runtimeapi.WithRunID(runtimeapi.WithSessionID(context.Background(), "sess_gap"), "run_gap")

	out, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("verify")}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out.Plan.Steps[0].Status != model.PlanStepFailed {
		t.Fatalf("step status = %q, want failed", out.Plan.Steps[0].Status)
	}
}

func TestActNodeContinuesSameStepUntilRollbackEvidenceCoversIntent(t *testing.T) {
	createCall := makeToolCall("call_1", "create_file", `{"path":"notes.txt","content":"hello"}`)
	rollbackCall := makeToolCall("call_2", "rollback_workspace_checkpoint", `{"checkpoint_id":"workspace_checkpoint_1"}`)
	actModel := &recordingActNodeModel{responses: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{createCall}),
		schema.AssistantMessage("", []schema.ToolCall{rollbackCall}),
	}}
	tools := &fakeToolInvoker{
		resultSeq: [][]*schema.Message{
			{schema.ToolMessage(`{"path":"notes.txt","bytes":5,"message":"ok","checkpoint_id":"workspace_checkpoint_1","checkpoint_paths":["notes.txt"],"verified_bytes":5,"verified_content":"hello","verification_truncated":false}`, "call_1", schema.WithToolName("create_file"))},
			{schema.ToolMessage(`{"checkpoint_id":"workspace_checkpoint_1","rollback_id":"workspace_rollback_1","status":"succeeded","restored_paths":["notes.txt"],"conflict_paths":[],"error":""}`, "call_2", schema.WithToolName("rollback_workspace_checkpoint"))},
		},
	}
	store := &fakePlanStore{loaded: &model.Plan{
		PlanID:    "plan_rollback_continue",
		SessionID: "sess_rollback_continue",
		RunID:     "run_rollback_continue",
		Steps: []model.PlanStep{{
			ID:        "s1",
			Action:    "Create and rollback a file",
			Status:    model.PlanStepPending,
			Risk:      model.PlanStepRiskWrite,
			ToolHints: []string{"create_file", "rollback_workspace_checkpoint"},
			VerificationIntent: []model.VerificationIntent{{
				Kind:   "rollback",
				Reason: "rollback must succeed",
			}},
		}},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	node := NewActNode(actModel, tools, tool.NewDirectAssistantStreamer(nil), store, nil, nil, nil)
	ctx := runtimeapi.WithRunID(runtimeapi.WithSessionID(context.Background(), "sess_rollback_continue"), "run_rollback_continue")

	out, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("create then rollback")}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out.Plan.Steps[0].Status != model.PlanStepCompleted {
		t.Fatalf("step status = %q, want completed", out.Plan.Steps[0].Status)
	}
	if actModel.callCount != 2 || tools.callCount != 2 {
		t.Fatalf("model/tools calls = %d/%d, want 2/2", actModel.callCount, tools.callCount)
	}
	var foundRollback bool
	for _, item := range out.Plan.Steps[0].Evidence {
		if item.Kind == model.EvidenceKindRollback && item.Status == model.EvidenceStatusPassed {
			foundRollback = true
		}
	}
	if !foundRollback {
		t.Fatalf("expected passed rollback evidence, got %+v", out.Plan.Steps[0].Evidence)
	}
	if len(actModel.inputs) < 2 {
		t.Fatalf("model inputs = %d, want second round", len(actModel.inputs))
	}
	var foundContinuation bool
	for _, msg := range actModel.inputs[1] {
		if msg != nil && msg.Role == schema.User && strings.Contains(msg.Content, "Missing verification") {
			foundContinuation = true
			break
		}
	}
	if !foundContinuation {
		t.Fatalf("second model input missing verification continuation: %+v", actModel.inputs[1])
	}
}

func TestActNodeCompletesDelegateStepWithPassedSubagentEvidence(t *testing.T) {
	toolCall := makeToolCall("call_1", "delegate_task", `{"task":"write tests","acceptance_criteria":["tests pass"]}`)
	nodeModel := &actNodeModel{response: schema.AssistantMessage("", []schema.ToolCall{toolCall})}
	tools := &fakeToolInvoker{results: []*schema.Message{
		schema.ToolMessage(`{"child_run_id":"run_child_1","child_session_id":"delegate_run_child_1","final_status":"succeeded","output_summary":"tests pass","acceptance":{"status":"passed","reasons":[]}}`, "call_1", schema.WithToolName("delegate_task")),
	}}
	store := &fakePlanStore{loaded: &model.Plan{
		PlanID:    "plan_delegate",
		SessionID: "sess_delegate",
		RunID:     "run_delegate",
		Steps: []model.PlanStep{{
			ID:     "s1",
			Action: "Delegate test writing",
			Status: model.PlanStepPending,
			Risk:   model.PlanStepRiskDelegate,
			VerificationIntent: []model.VerificationIntent{{
				Kind:   "subagent",
				Reason: "child task must pass acceptance",
			}},
		}},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	node := NewActNode(nodeModel, tools, tool.NewDirectAssistantStreamer(nil), store, nil, nil, nil)
	ctx := runtimeapi.WithRunID(runtimeapi.WithSessionID(context.Background(), "sess_delegate"), "run_delegate")

	out, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("delegate tests")}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out.Plan.Steps[0].Status != model.PlanStepCompleted {
		t.Fatalf("step status = %q, want completed", out.Plan.Steps[0].Status)
	}
	found := false
	for _, item := range out.Plan.Steps[0].Evidence {
		if item.Kind == model.EvidenceKindSubagent && item.Status == model.EvidenceStatusPassed && item.ChildRunID == "run_child_1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected passed subagent evidence, got %+v", out.Plan.Steps[0].Evidence)
	}
}

func TestActNodeFailsDelegateStepWhenSubagentAcceptanceFails(t *testing.T) {
	toolCall := makeToolCall("call_1", "delegate_task", `{"task":"write tests","acceptance_criteria":["tests pass"]}`)
	nodeModel := &actNodeModel{response: schema.AssistantMessage("", []schema.ToolCall{toolCall})}
	tools := &fakeToolInvoker{results: []*schema.Message{
		schema.ToolMessage(`{"child_run_id":"run_child_2","child_session_id":"delegate_run_child_2","final_status":"succeeded","output_summary":"tests missing","acceptance":{"status":"failed","reasons":["missing expected evidence: go test ./internal/auth"]}}`, "call_1", schema.WithToolName("delegate_task")),
	}}
	store := &fakePlanStore{loaded: &model.Plan{
		PlanID:    "plan_delegate_fail",
		SessionID: "sess_delegate_fail",
		RunID:     "run_delegate_fail",
		Steps: []model.PlanStep{{
			ID:     "s1",
			Action: "Delegate test writing",
			Status: model.PlanStepPending,
			Risk:   model.PlanStepRiskDelegate,
			VerificationIntent: []model.VerificationIntent{{
				Kind:   "subagent",
				Reason: "child task must pass acceptance",
			}},
		}},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	node := NewActNode(nodeModel, tools, tool.NewDirectAssistantStreamer(nil), store, nil, nil, nil)
	ctx := runtimeapi.WithRunID(runtimeapi.WithSessionID(context.Background(), "sess_delegate_fail"), "run_delegate_fail")

	out, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("delegate tests")}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out.Plan.Steps[0].Status != model.PlanStepFailed {
		t.Fatalf("step status = %q, want failed", out.Plan.Steps[0].Status)
	}
	found := false
	for _, item := range out.Plan.Steps[0].Evidence {
		if item.Kind == model.EvidenceKindSubagent && item.Status == model.EvidenceStatusFailed && strings.Contains(item.Error, "missing expected evidence") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected failed subagent evidence, got %+v", out.Plan.Steps[0].Evidence)
	}
}

func TestActNodeRecordsFailedToolEvidence(t *testing.T) {
	toolCall := makeToolCall("call_1", "read_file", `{"path":"missing.md"}`)
	nodeModel := &actNodeModel{response: schema.AssistantMessage("", []schema.ToolCall{toolCall})}
	msg := schema.ToolMessage("file does not exist", "call_1", schema.WithToolName("read_file"))
	if msg.Extra == nil {
		msg.Extra = map[string]any{}
	}
	msg.Extra["tool_error"] = true
	msg.Extra["tool_error_reason"] = "file does not exist"
	tools := &fakeToolInvoker{results: []*schema.Message{msg}}
	store := &fakePlanStore{loaded: &model.Plan{
		PlanID:    "plan_fail_evidence",
		SessionID: "sess_fail_evidence",
		RunID:     "run_fail_evidence",
		Steps:     []model.PlanStep{{ID: "s1", Action: "Read missing file", Status: model.PlanStepPending}},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	node := NewActNode(nodeModel, tools, tool.NewDirectAssistantStreamer(nil), store, nil, nil, nil)
	ctx := runtimeapi.WithRunID(runtimeapi.WithSessionID(context.Background(), "sess_fail_evidence"), "run_fail_evidence")

	out, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("read missing")}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out.Plan.Steps[0].Status != model.PlanStepFailed {
		t.Fatalf("step status = %q, want failed", out.Plan.Steps[0].Status)
	}
	if len(out.Plan.Steps[0].Evidence) == 0 || out.Plan.Steps[0].Evidence[0].Status != model.EvidenceStatusFailed {
		t.Fatalf("expected failed evidence, got %+v", out.Plan.Steps[0].Evidence)
	}
}

func TestActNodeRecordsDiffEvidenceFromRecorder(t *testing.T) {
	toolCall := makeToolCall("call_1", "create_file", `{"path":"target.txt"}`)
	nodeModel := &actNodeModel{response: schema.AssistantMessage("", []schema.ToolCall{toolCall})}
	msg := schema.ToolMessage(`{"path":"target.txt","message":"ok","checkpoint_id":"checkpoint_create_file","checkpoint_paths":["target.txt"],"verified_bytes":1,"verified_content":"target","verification_truncated":false}`, "call_1", schema.WithToolName("create_file"))
	msg.Extra = map[string]any{
		"plan_evidence_recorder": toolExecutionRecorder{
			items: []recordedToolArtifact{{
				Kind:    model.EvidenceKindDiff,
				Status:  model.EvidenceStatusRecorded,
				Summary: "created diff evidence",
				Paths:   []string{"target.txt"},
				DiffRef: "diff_1",
			}},
		},
	}
	tools := &fakeToolInvoker{results: []*schema.Message{msg}}
	store := &fakePlanStore{loaded: &model.Plan{
		PlanID:    "plan_snapshot",
		SessionID: "sess_snapshot",
		RunID:     "run_snapshot",
		Steps:     []model.PlanStep{{ID: "s1", Action: "Write file", Status: model.PlanStepPending}},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	node := NewActNode(nodeModel, tools, tool.NewDirectAssistantStreamer(nil), store, nil, nil, nil)
	ctx := runtimeapi.WithRunID(runtimeapi.WithSessionID(context.Background(), "sess_snapshot"), "run_snapshot")

	out, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("write")}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	found := false
	for _, item := range out.Plan.Steps[0].Evidence {
		if item.Kind == model.EvidenceKindDiff && item.DiffRef == "diff_1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected diff evidence, got %+v", out.Plan.Steps[0].Evidence)
	}
}

func TestActNodeEnforcesRiskyToolPlanBeforeTools(t *testing.T) {
	toolCall := makeToolCall("call_1", "create_file", `{"path":"x.txt"}`)
	nodeModel := &actNodeModel{response: schema.AssistantMessage("", []schema.ToolCall{toolCall})}
	tools := &fakeToolInvoker{results: []*schema.Message{
		schema.ToolMessage(`{"path":"x.txt","message":"ok","checkpoint_id":"checkpoint_create_file","checkpoint_paths":["x.txt"],"verified_bytes":1,"verified_content":"x","verification_truncated":false}`, "call_1", schema.WithToolName("create_file")),
	}}
	store := &fakePlanStore{loaded: &model.Plan{
		PlanID:    "plan_1",
		SessionID: "sess_risky",
		RunID:     "run_risky",
		Steps: []model.PlanStep{
			{ID: "s1", Action: "Create file", Status: model.PlanStepPending},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	node := NewActNode(nodeModel, tools, tool.NewDirectAssistantStreamer(nil), store, nil, []tooling.ToolSpec{
		{
			ToolContract: tooling.ToolContract{
				Name:          "create_file",
				Source:        "local",
				Kind:          tooling.ToolKindNative,
				Category:      tooling.ToolCategoryWrite,
				ResourceScope: tooling.ResourceScopeWorkspaceFile,
				Profiles:      []tooling.ToolProfile{tooling.ToolProfileRun},
				PlanPolicy:    tooling.PlanPolicyRequireActivePlan,
				FactPolicy:    tooling.FactPolicyAuto,
				Loading:       tooling.EagerLoadingPolicy(),
				Execution: tooling.ToolExecutionPolicy{
					ParallelPolicy: tooling.ParallelPolicyWriteScoped,
					PathArg:        "path",
				},
				Result:     tooling.InlineResultPolicy(0),
				Boundary:   tooling.ToolResultBoundaryPolicy(),
				Projection: tooling.ActivityProjectionPolicy(),
			},
		},
	}, nil)
	ctx := runtimeapi.WithRunID(runtimeapi.WithSessionID(context.Background(), "sess_risky"), "run_risky")

	if _, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("create")}}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if store.loadCount < 2 {
		t.Fatalf("LoadPlan called %d times, want at least 2 to prove risky enforcement loaded active plan", store.loadCount)
	}
	if tools.callCount != 1 {
		t.Fatalf("tools callCount = %d, want 1", tools.callCount)
	}
}

func TestActNodeMarksFailedWhenToolNodeFails(t *testing.T) {
	toolCall := makeToolCall("call_1", "read_file", `{"path":"README.md"}`)
	nodeModel := &actNodeModel{response: schema.AssistantMessage("", []schema.ToolCall{toolCall})}
	tools := &fakeToolInvoker{err: errors.New("tool node failed")}
	store := &fakePlanStore{loaded: &model.Plan{
		PlanID:    "plan_1",
		SessionID: "sess_fail",
		RunID:     "run_fail",
		Steps: []model.PlanStep{
			{ID: "s1", Action: "Read README", Status: model.PlanStepPending},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	node := NewActNode(nodeModel, tools, tool.NewDirectAssistantStreamer(nil), store, nil, nil, nil)
	ctx := runtimeapi.WithRunID(runtimeapi.WithSessionID(context.Background(), "sess_fail"), "run_fail")

	out, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("read")}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out.Plan.Steps[0].Status != model.PlanStepFailed {
		t.Fatalf("step status = %q, want failed", out.Plan.Steps[0].Status)
	}
}

func TestActNodeMarksFailedWhenModelReturnsNoToolCalls(t *testing.T) {
	nodeModel := &actNodeModel{response: schema.AssistantMessage("I cannot call tools.", nil)}
	store := &fakePlanStore{loaded: &model.Plan{
		PlanID:    "plan_1",
		SessionID: "sess_no_tools",
		RunID:     "run_no_tools",
		Steps: []model.PlanStep{
			{ID: "s1", Action: "Read README", Status: model.PlanStepPending},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	node := NewActNode(nodeModel, &fakeToolInvoker{}, tool.NewDirectAssistantStreamer(nil), store, nil, nil, nil)
	ctx := runtimeapi.WithRunID(runtimeapi.WithSessionID(context.Background(), "sess_no_tools"), "run_no_tools")

	out, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("read")}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out.Plan.Steps[0].Status != model.PlanStepFailed {
		t.Fatalf("step status = %q, want failed", out.Plan.Steps[0].Status)
	}
	if !strings.Contains(out.Messages[len(out.Messages)-1].Content, "cannot call") {
		t.Fatalf("assistant content not preserved: %+v", out.Messages)
	}
}

func TestActNodeContinuesStepAfterLoadToolsOnlyRound(t *testing.T) {
	loadCall := makeToolCall("call_1", "load_tools", `{"tool_names":["memory_search"]}`)
	searchCall := makeToolCall("call_2", "memory_search", `{"query":"golang"}`)
	modelResponses := []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{loadCall}),
		schema.AssistantMessage("", []schema.ToolCall{searchCall}),
	}
	actModel := &recordingActNodeModel{responses: modelResponses}
	tools := &fakeToolInvoker{
		resultSeq: [][]*schema.Message{
			{schema.ToolMessage(`{"messages":["<deferred-tool-definitions>\n- memory_search: Search memory records [memory_tool]\n</deferred-tool-definitions>"],"loaded_tool_names":["memory_search"]}`, "call_1", schema.WithToolName("load_tools"))},
			{schema.ToolMessage("knowledge hit", "call_2", schema.WithToolName("memory_search"))},
		},
	}
	store := &fakePlanStore{loaded: &model.Plan{
		PlanID:    "plan_load_tools",
		SessionID: "sess_load_tools",
		RunID:     "run_load_tools",
		Steps:     []model.PlanStep{{ID: "s1", Action: "Research knowledge", Status: model.PlanStepPending}},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	node := NewActNode(actModel, tools, tool.NewDirectAssistantStreamer(nil), store, nil, nil, []string{"read_file"})
	ctx := runtimeapi.WithRunID(runtimeapi.WithSessionID(context.Background(), "sess_load_tools"), "run_load_tools")

	out, err := node.Invoke(ctx, &graph.AgentGraphState{Messages: []*schema.Message{schema.UserMessage("research")}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out.Plan.Steps[0].Status != model.PlanStepCompleted {
		t.Fatalf("step status = %q, want completed", out.Plan.Steps[0].Status)
	}
	if tools.callCount != 2 {
		t.Fatalf("tool callCount = %d, want 2", tools.callCount)
	}
	if len(out.Plan.Steps[0].Evidence) < 2 {
		t.Fatalf("expected both load_tools and search evidence, got %+v", out.Plan.Steps[0].Evidence)
	}
	if len(actModel.inputs) < 2 {
		t.Fatalf("model inputs = %d, want at least 2 rounds", len(actModel.inputs))
	}
	var found bool
	for _, msg := range actModel.inputs[1] {
		if msg != nil && msg.Role == schema.User && strings.Contains(msg.Content, "<deferred-tool-definitions>") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("second round input missing deferred definition message: %+v", actModel.inputs[1])
	}
}

type recordingActNodeModel struct {
	responses []*schema.Message
	callCount int
	inputs    [][]*schema.Message
}

func (m *recordingActNodeModel) Generate(_ context.Context, messages []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	copied := make([]*schema.Message, len(messages))
	copy(copied, messages)
	m.inputs = append(m.inputs, copied)
	if m.callCount >= len(m.responses) {
		return schema.AssistantMessage("done", nil), nil
	}
	msg := m.responses[m.callCount]
	m.callCount++
	return msg, nil
}

func (m *recordingActNodeModel) Stream(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

func TestParseLoadToolsOutput(t *testing.T) {
	payload := parseLoadToolsOutput(`{"messages":["hello"],"loaded_tool_names":["x"]}`)
	if len(payload.Messages) != 1 || payload.Messages[0] != "hello" {
		t.Fatalf("payload = %+v", payload)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(raw), "hello") {
		t.Fatalf("marshaled payload = %s", raw)
	}
}

// --- Test helper duplicated from tool package ---

func makeToolCall(id, name, args string) schema.ToolCall {
	return schema.ToolCall{
		ID: id,
		Function: schema.FunctionCall{
			Name:      name,
			Arguments: args,
		},
	}
}
