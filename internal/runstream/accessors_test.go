package runstream

import (
	"testing"
)

func TestStreamItemGetMessage(t *testing.T) {
	msg := &StreamMessage{Role: "assistant", Content: "hello"}

	item := StreamItem{Payload: &AssistantMessagePayload{Message: msg}}
	if got := item.GetMessage(); got != msg {
		t.Fatalf("GetMessage from assistant_message = %v, want %v", got, msg)
	}

	item = StreamItem{Payload: &RunCompletedPayload{Message: msg}}
	if got := item.GetMessage(); got != msg {
		t.Fatalf("GetMessage from run_completed = %v, want %v", got, msg)
	}

	item = StreamItem{Payload: &RunStartedPayload{Input: "test"}}
	if got := item.GetMessage(); got != nil {
		t.Fatalf("GetMessage from run_started = %v, want nil", got)
	}
}

func TestStreamItemGetAssistantDelta(t *testing.T) {
	delta := &StreamAssistantDelta{Delta: "world"}

	item := StreamItem{Payload: &AssistantDeltaPayload{AssistantDelta: delta}}
	if got := item.GetAssistantDelta(); got != delta {
		t.Fatalf("GetAssistantDelta = %v, want %v", got, delta)
	}

	item = StreamItem{Payload: &RunStartedPayload{}}
	if got := item.GetAssistantDelta(); got != nil {
		t.Fatalf("GetAssistantDelta = %v, want nil", got)
	}
}

func TestStreamItemGetToolCall(t *testing.T) {
	call := &StreamToolCall{CallID: "call_1"}

	cases := []StreamPayload{
		&ToolCallStartedPayload{ToolCall: call},
		&ToolCallSucceededPayload{ToolCall: call},
		&ToolCallFailedPayload{ToolCall: call},
		&ToolCallInterruptedPayload{ToolCall: call},
	}
	for i, p := range cases {
		item := StreamItem{Payload: p}
		if got := item.GetToolCall(); got != call {
			t.Fatalf("case %d: GetToolCall = %v, want %v", i, got, call)
		}
	}

	item := StreamItem{Payload: &RunStartedPayload{}}
	if got := item.GetToolCall(); got != nil {
		t.Fatalf("GetToolCall = %v, want nil", got)
	}
}

func TestStreamItemGetToolCallProgress(t *testing.T) {
	prog := &StreamToolCallProgress{CallID: "call_1"}

	item := StreamItem{Payload: &ToolCallProgressPayload{ToolCall: prog}}
	if got := item.GetToolCallProgress(); got != prog {
		t.Fatalf("GetToolCallProgress = %v, want %v", got, prog)
	}

	item = StreamItem{Payload: &RunStartedPayload{}}
	if got := item.GetToolCallProgress(); got != nil {
		t.Fatalf("GetToolCallProgress = %v, want nil", got)
	}
}

func TestStreamItemGetInterrupt(t *testing.T) {
	interrupt := &StreamInterrupt{ContextCount: 1}

	item := StreamItem{Payload: &RunInterruptedPayload{Interrupt: interrupt}}
	if got := item.GetInterrupt(); got != interrupt {
		t.Fatalf("GetInterrupt = %v, want %v", got, interrupt)
	}

	item = StreamItem{Payload: &RunStartedPayload{}}
	if got := item.GetInterrupt(); got != nil {
		t.Fatalf("GetInterrupt = %v, want nil", got)
	}
}

func TestStreamItemGetSkill(t *testing.T) {
	skill := &StreamSkill{SelectedID: "skill_1"}

	cases := []StreamPayload{
		&SkillDiscoveredPayload{Skill: skill},
		&SkillSelectedPayload{Skill: skill},
		&SkillLoadedPayload{Skill: skill},
		&SkillFailedPayload{Skill: skill},
	}
	for i, p := range cases {
		item := StreamItem{Payload: p}
		if got := item.GetSkill(); got != skill {
			t.Fatalf("case %d: GetSkill = %v, want %v", i, got, skill)
		}
	}

	item := StreamItem{Payload: &RunStartedPayload{}}
	if got := item.GetSkill(); got != nil {
		t.Fatalf("GetSkill = %v, want nil", got)
	}
}

func TestStreamItemGetSkillLifecycle(t *testing.T) {
	lc := &StreamSkillLifecycle{SkillID: "skill_1", Status: "verified"}

	item := StreamItem{Payload: &SkillLifecyclePayload{SkillLifecycle: lc}}
	if got := item.GetSkillLifecycle(); got != lc {
		t.Fatalf("GetSkillLifecycle = %v, want %v", got, lc)
	}

	item = StreamItem{Payload: &RunStartedPayload{}}
	if got := item.GetSkillLifecycle(); got != nil {
		t.Fatalf("GetSkillLifecycle = %v, want nil", got)
	}
}

func TestStreamItemGetMemoryPrepared(t *testing.T) {
	mp := &StreamMemoryPrepared{Entries: []StreamMemoryPreparedEntry{{Ref: "mem_1"}}}

	item := StreamItem{Payload: &MemoryPreparedPayload{MemoryPrepared: mp}}
	if got := item.GetMemoryPrepared(); got != mp {
		t.Fatalf("GetMemoryPrepared = %v, want %v", got, mp)
	}

	item = StreamItem{Payload: &RunStartedPayload{}}
	if got := item.GetMemoryPrepared(); got != nil {
		t.Fatalf("GetMemoryPrepared = %v, want nil", got)
	}
}

func TestStreamItemGetProcedureActivation(t *testing.T) {
	pa := &StreamProcedureActivation{ProcedureRef: "proc_1"}

	item := StreamItem{Payload: &ProcedureActivationPayload{ProcedureActivation: pa}}
	if got := item.GetProcedureActivation(); got != pa {
		t.Fatalf("GetProcedureActivation = %v, want %v", got, pa)
	}

	item = StreamItem{Payload: &RunStartedPayload{}}
	if got := item.GetProcedureActivation(); got != nil {
		t.Fatalf("GetProcedureActivation = %v, want nil", got)
	}
}

func TestStreamItemGetContextCompressed(t *testing.T) {
	cc := &StreamContextCompressed{BoundaryID: "bound_1"}

	item := StreamItem{Payload: &ContextCompressedPayload{ContextCompressed: cc}}
	if got := item.GetContextCompressed(); got != cc {
		t.Fatalf("GetContextCompressed = %v, want %v", got, cc)
	}

	item = StreamItem{Payload: &RunStartedPayload{}}
	if got := item.GetContextCompressed(); got != nil {
		t.Fatalf("GetContextCompressed = %v, want nil", got)
	}
}

func TestStreamItemGetContextPressure(t *testing.T) {
	cp := &StreamContextPressure{PercentUsed: 50}

	item := StreamItem{Payload: &ContextPressurePayload{ContextPressure: cp}}
	if got := item.GetContextPressure(); got != cp {
		t.Fatalf("GetContextPressure = %v, want %v", got, cp)
	}

	item = StreamItem{Payload: &RunStartedPayload{}}
	if got := item.GetContextPressure(); got != nil {
		t.Fatalf("GetContextPressure = %v, want nil", got)
	}
}

func TestStreamItemGetError(t *testing.T) {
	item := StreamItem{Payload: &RunFailedPayload{Error: "boom"}}
	if got := item.GetError(); got != "boom" {
		t.Fatalf("GetError = %q, want %q", got, "boom")
	}

	item = StreamItem{Payload: &RunStartedPayload{}}
	if got := item.GetError(); got != "" {
		t.Fatalf("GetError = %q, want empty", got)
	}
}

func TestStreamItemGetInput(t *testing.T) {
	item := StreamItem{Payload: &RunStartedPayload{Input: "hello"}}
	if got := item.GetInput(); got != "hello" {
		t.Fatalf("GetInput = %q, want %q", got, "hello")
	}

	item = StreamItem{Payload: &RunCompletedPayload{}}
	if got := item.GetInput(); got != "" {
		t.Fatalf("GetInput = %q, want empty", got)
	}
}

func TestStreamItemGetTargets(t *testing.T) {
	targets := map[string]any{"action": "approve"}

	item := StreamItem{Payload: &RunResumeRequestedPayload{Targets: targets}}
	if got := item.GetTargets(); got == nil {
		t.Fatalf("GetTargets = nil, want %v", targets)
	}

	item = StreamItem{Payload: &RunStartedPayload{}}
	if got := item.GetTargets(); got != nil {
		t.Fatalf("GetTargets = %v, want nil", got)
	}
}

func TestStreamItemGetPlan(t *testing.T) {
	plan := &StreamPlan{PlanID: "plan_1"}

	cases := []StreamPayload{
		&PlanCreatedPayload{Plan: plan},
		&PlanUpdatedPayload{Plan: plan},
		&PlanStepStartedPayload{PlanStepPayload: PlanStepPayload{Plan: plan}},
		&PlanStepCompletedPayload{PlanStepPayload: PlanStepPayload{Plan: plan}},
		&PlanStepFailedPayload{PlanStepPayload: PlanStepPayload{Plan: plan}},
	}
	for i, p := range cases {
		item := StreamItem{Payload: p}
		if got := item.GetPlan(); got != plan {
			t.Fatalf("case %d: GetPlan = %v, want %v", i, got, plan)
		}
	}

	item := StreamItem{Payload: &RunStartedPayload{}}
	if got := item.GetPlan(); got != nil {
		t.Fatalf("GetPlan = %v, want nil", got)
	}
}
