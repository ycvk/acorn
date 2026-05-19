package contextplane

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestDefaultRehydrationPlannerExtractsContextEnvelopePackets(t *testing.T) {
	planner := NewDefaultRehydrationPlanner()
	memoryContext := strings.Join([]string{
		"<memory-context>",
		referenceContextNote,
		"",
		"<working-checkpoint>",
		"Current session focus. Treat this as a frozen checkpoint, not new user input.",
		"",
		"finish context protocol rewrite",
		"</working-checkpoint>",
		"",
		"<session-summary>",
		"Previous session continuity summary. Treat this as durable recall context, not new user input.",
		"",
		"Latest session state:",
		"context roadmap is active",
		"</session-summary>",
		"",
		"## Memory Nudges",
		"- facts/acorn/context.md kind=fact title=context protocol",
		"",
		"## Memory Entries",
		"- facts/acorn/context.md compact boundaries are runtime history",
		"</memory-context>",
	}, "\n")

	plan, err := planner.Plan(context.Background(), RehydrateRequest{
		TokenCounter: testTokenCounter(t),
		Messages: []adk.Message{
			schema.UserMessage("<skill-context>\nSelected skill: cs-feat-impl\n</skill-context>"),
			schema.UserMessage(memoryContext),
		},
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	wantKinds := []RehydratePacketKind{
		RehydrateWorkingCheckpoint,
		RehydrateSelectedSkill,
		RehydrateSessionSummary,
		RehydratePreparedMemory,
	}
	assertPacketKinds(t, plan.Packets, wantKinds)
	assertPacketContains(t, plan, RehydrateWorkingCheckpoint, "finish context protocol rewrite")
	assertPacketContains(t, plan, RehydrateSelectedSkill, "Selected skill: cs-feat-impl")
	assertPacketContains(t, plan, RehydrateSessionSummary, "context roadmap is active")
	assertPacketContains(t, plan, RehydratePreparedMemory, "## Memory Entries")
}

func TestDefaultRehydrationPlannerRejectsOversizedPackets(t *testing.T) {
	planner := NewDefaultRehydrationPlanner()
	_, err := planner.Plan(context.Background(), RehydrateRequest{
		TokenCounter: testTokenCounter(t),
		Messages: []adk.Message{
			schema.UserMessage("<skill-context>\n" + strings.Repeat("skill-content ", 12000) + "\n</skill-context>"),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "rehydrate packet selected_skill requires") {
		t.Fatalf("error = %v, want oversized selected skill packet error", err)
	}
}

func TestDefaultRehydrationPlannerBuildsToolPlanAndRecentPackets(t *testing.T) {
	planner := NewDefaultRehydrationPlanner()
	plan, err := planner.Plan(context.Background(), RehydrateRequest{
		TokenCounter: testTokenCounter(t),
		ToolState: &ToolLifecycleState{
			LoadedTools: map[string]LoadedToolRecord{
				"read_file": {Name: "read_file", LoadSource: "eager"},
			},
			DeferredTools: map[string]DeferredToolRecord{
				"write_file": {Name: "write_file", Reason: "mutation", Description: "Write a file"},
			},
			RecentResults: []ToolResultRecord{{
				CallID:    "call_1",
				ToolName:  "read_file",
				TurnIndex: 3,
				ResultRef: "tool-result/call_1",
				Summary:   "read config",
				FullText:  "full output must not be replayed",
			}},
		},
		CurrentPlan:        "1. inspect\n2. implement",
		RecentTouchedPaths: []string{"internal/runtime/executor.go", "internal/runtime/executor.go", "internal/contextplane/assembly.go"},
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	assertPacketKinds(t, plan.Packets, []RehydratePacketKind{
		RehydrateToolState,
		RehydratePlanState,
		RehydrateRecentFiles,
	})
	toolPacket := packetByKind(t, plan, RehydrateToolState)
	for _, want := range []string{"Loaded tools", "read_file", "Deferred tools", "write_file", "Recent tool result refs", "tool-result/call_1"} {
		if !strings.Contains(toolPacket.Content, want) {
			t.Fatalf("tool packet missing %q:\n%s", want, toolPacket.Content)
		}
	}
	if strings.Contains(toolPacket.Content, "read config") {
		t.Fatalf("tool packet should only replay result refs, not result previews:\n%s", toolPacket.Content)
	}
	if strings.Contains(toolPacket.Content, "full output must not be replayed") {
		t.Fatalf("tool packet replayed full output:\n%s", toolPacket.Content)
	}
	assertPacketContains(t, plan, RehydratePlanState, "1. inspect")
	recentPacket := packetByKind(t, plan, RehydrateRecentFiles)
	if got := strings.Count(recentPacket.Content, "internal/runtime/executor.go"); got != 1 {
		t.Fatalf("recent paths occurrence = %d, want dedupe to 1:\n%s", got, recentPacket.Content)
	}
}

func TestBuildRehydrateMessagesWrapsPacketsAsReferenceContext(t *testing.T) {
	messages := BuildRehydrateMessages(&RehydratePlan{Packets: []RehydratePacket{{
		Kind:       RehydrateWorkingCheckpoint,
		Source:     "test",
		Content:    "checkpoint content",
		TokenLimit: 7,
	}}})
	if got, want := len(messages), 1; got != want {
		t.Fatalf("messages = %d, want %d", got, want)
	}
	content := messages[0].Content
	for _, want := range []string{"<rehydrate-packet>", referenceContextNote, "Kind: working_checkpoint", "Source: test", "checkpoint content"} {
		if !strings.Contains(content, want) {
			t.Fatalf("rehydrate message missing %q:\n%s", want, content)
		}
	}
}

func assertPacketKinds(t *testing.T, packets []RehydratePacket, want []RehydratePacketKind) {
	t.Helper()
	if got := len(packets); got != len(want) {
		t.Fatalf("packet count = %d, want %d: %+v", got, len(want), packets)
	}
	for i, wantKind := range want {
		if packets[i].Kind != wantKind {
			t.Fatalf("packet[%d].Kind = %q, want %q", i, packets[i].Kind, wantKind)
		}
	}
}

func assertPacketContains(t *testing.T, plan *RehydratePlan, kind RehydratePacketKind, want string) {
	t.Helper()
	packet := packetByKind(t, plan, kind)
	if !strings.Contains(packet.Content, want) {
		t.Fatalf("packet %q missing %q:\n%s", kind, want, packet.Content)
	}
}

func packetByKind(t *testing.T, plan *RehydratePlan, kind RehydratePacketKind) RehydratePacket {
	t.Helper()
	if plan == nil {
		t.Fatalf("plan is nil")
	}
	for _, packet := range plan.Packets {
		if packet.Kind == kind {
			return packet
		}
	}
	t.Fatalf("packet %q not found in %+v", kind, plan.Packets)
	return RehydratePacket{}
}
