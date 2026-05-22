package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/memorymodule"
)

func TestBuildCapabilityRegistryFailsOnResourceToolInfoError(t *testing.T) {
	_, err := buildCapabilityRegistryForTest(
		context.Background(),
		nil,
		nil,
		[]einotool.BaseTool{failingInfoTool{message: "resource metadata unavailable"}},
		nil,
	)
	if err == nil {
		t.Fatal("buildCapabilityRegistry returned nil error, want resource metadata error")
	}
	if !strings.Contains(err.Error(), "read resource tool info") {
		t.Fatalf("error = %q, want resource tool info context", err.Error())
	}
	if !strings.Contains(err.Error(), "resource metadata unavailable") {
		t.Fatalf("error = %q, want root metadata error", err.Error())
	}
}

func TestBuildCapabilityRegistryFailsOnPromptToolInfoError(t *testing.T) {
	_, err := buildCapabilityRegistryForTest(
		context.Background(),
		nil,
		nil,
		nil,
		[]einotool.BaseTool{failingInfoTool{message: "prompt metadata unavailable"}},
	)
	if err == nil {
		t.Fatal("buildCapabilityRegistry returned nil error, want prompt metadata error")
	}
	if !strings.Contains(err.Error(), "read prompt tool info") {
		t.Fatalf("error = %q, want prompt tool info context", err.Error())
	}
	if !strings.Contains(err.Error(), "prompt metadata unavailable") {
		t.Fatalf("error = %q, want root metadata error", err.Error())
	}
}

type failingInfoTool struct {
	message string
}

func (t failingInfoTool) Info(context.Context) (*schema.ToolInfo, error) {
	return nil, errors.New(t.message)
}

func TestBuildStableInstruction(t *testing.T) {
	got := buildStableInstruction("you are a helper", "be concise")
	if !strings.Contains(got, "you are a helper") {
		t.Fatalf("stable instruction missing base: %q", got)
	}
	if !strings.Contains(got, "Capability discovery rules:") {
		t.Fatalf("stable instruction missing capability discovery guidance: %q", got)
	}
	if !strings.Contains(got, "be concise") {
		t.Fatalf("stable instruction missing suffix: %q", got)
	}
	if strings.Contains(got, "Selected skill") {
		t.Fatalf("stable instruction should not contain skill brief (moved to skill-context message): %q", got)
	}
}

func TestBuildStableInstructionBaseOnly(t *testing.T) {
	got := buildStableInstruction("base prompt", "")
	if !strings.Contains(got, "base prompt") {
		t.Fatalf("stable instruction missing base prompt: %q", got)
	}
	if !strings.Contains(got, "call skill_list or skill_view before answering") {
		t.Fatalf("stable instruction missing skill discovery rule: %q", got)
	}
}

func TestBuildStableInstructionAllEmpty(t *testing.T) {
	got := buildStableInstruction("", "")
	if !strings.Contains(got, "Capability discovery rules:") {
		t.Fatalf("expected capability discovery guidance, got %q", got)
	}
}

func TestStableInstructionExcludesDynamicContent(t *testing.T) {
	got := buildStableInstruction("you are a helper", "be concise")
	if strings.Contains(got, "checkpoint") {
		t.Fatalf("stable instruction should not contain checkpoint: %q", got)
	}
	if strings.Contains(got, "profile") {
		t.Fatalf("stable instruction should not contain profile: %q", got)
	}
	if strings.Contains(got, "Retrieval Cards") {
		t.Fatalf("stable instruction should not contain retrieval cards: %q", got)
	}
	if strings.Contains(got, "Selected skill") {
		t.Fatalf("stable instruction should not contain skill brief: %q", got)
	}
}

func TestEmitMemoryPreparedEventWritesPreparedPayload(t *testing.T) {
	ctx := context.Background()
	store, _ := newRunnerFactoryMemoryTestContext(t)
	result := &memorymodule.PrepareResult{
		Nudges: []memorymodule.Nudge{{
			Ref:    "facts/workspaces/acorn/runtime.md",
			Kind:   "fact",
			Title:  "Runtime",
			Status: "verified",
			Reason: "matched input",
		}},
		Entries: []memorymodule.Entry{{
			Ref:     "facts/workspaces/acorn/runtime.md",
			Kind:    "fact",
			Title:   "Runtime",
			Content: "not stored in event",
		}},
	}

	if err := emitMemoryPreparedEvent(ctx, store, RunnerBuildRequest{RunID: "run_usage", Input: "release closeout"}, "workspace:acorn", result); err != nil {
		t.Fatalf("emitMemoryPreparedEvent: %v", err)
	}
	records, err := store.LoadEvents(ctx, "run_usage")
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("events = %#v", records)
	}
	item := projectEventToStreamItem(records[0])
	prepared := item.GetMemoryPrepared()
	if prepared == nil {
		t.Fatalf("event payload = %#v", item.Payload)
	}
	if prepared.Query != "release closeout" || prepared.WorkspaceScope != "workspace:acorn" {
		t.Fatalf("prepared identity = %#v", prepared)
	}
	if prepared.NudgeCount != 1 || prepared.EntryCount != 1 {
		t.Fatalf("prepared counts = %#v", prepared)
	}
	if len(prepared.Nudges) != 1 || prepared.Nudges[0].Ref != "facts/workspaces/acorn/runtime.md" {
		t.Fatalf("prepared nudges = %#v", prepared.Nudges)
	}
	if len(prepared.Entries) != 1 || prepared.Entries[0].Title != "Runtime" {
		t.Fatalf("prepared entries = %#v", prepared.Entries)
	}
}

func TestEmitProcedureActivationEventsWritesActivationPayload(t *testing.T) {
	ctx := context.Background()
	store, _ := newRunnerFactoryMemoryTestContext(t)
	err := emitProcedureActivationEvents(ctx, store, nil, "run_proc", []memorymodule.ProcedureActivation{{
		RunID:        "run_proc",
		SessionID:    "sess_proc",
		ProcedureRef: "skills/learned/sqlite.md#sqlite",
		Title:        "SQLite",
		Kind:         "skill",
		Phase:        memorymodule.ProcedureActivationInjected,
		Reason:       "injected_into_memory_context",
		Score:        5,
		Status:       memorymodule.StatusVerified,
		Origin:       memorymodule.ProcedureOriginActionVerified,
		EvidenceRefs: []string{"tool-result:run_proc:call_1"},
	}})
	if err != nil {
		t.Fatalf("emitProcedureActivationEvents: %v", err)
	}
	records, err := store.LoadEvents(ctx, "run_proc")
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("events = %#v", records)
	}
	item := projectEventToStreamItem(records[0])
	activation := item.GetProcedureActivation()
	if activation == nil {
		t.Fatalf("event payload = %#v", item.Payload)
	}
	if activation.Phase != "injected" || activation.ProcedureRef != "skills/learned/sqlite.md#sqlite" {
		t.Fatalf("activation = %#v", activation)
	}
	if len(activation.EvidenceRefs) != 1 || activation.EvidenceRefs[0] != "tool-result:run_proc:call_1" {
		t.Fatalf("evidence refs = %#v", activation.EvidenceRefs)
	}
}
