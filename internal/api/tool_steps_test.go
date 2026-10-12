package api

import (
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

func TestProjectToolCallEventsCarrySummaryNotOutput(t *testing.T) {
	started, err := ProjectRunEvent(core.EventRecord{RunID: "run_1", Sequence: 5, Kind: "tool.call.started", Payload: map[string]any{
		"tool_call_id": "call_1", "tool_name": "web_search", "arguments": `{"limit":5,"query":"  go 1.28\n release  "}`,
	}})
	if err != nil {
		t.Fatalf("project started: %v", err)
	}
	if got := started.Data.(core.ToolCallData); got != (core.ToolCallData{ToolCallID: "call_1", ToolName: "web_search", Summary: "go 1.28 release"}) {
		t.Fatalf("started data = %+v", got)
	}

	succeeded, err := ProjectRunEvent(core.EventRecord{RunID: "run_1", Sequence: 6, Kind: "tool.call.succeeded", Payload: map[string]any{
		"tool_call_id": "call_1", "tool_name": "web_search", "output": "secret search results",
	}})
	if err != nil {
		t.Fatalf("project succeeded: %v", err)
	}
	if got := succeeded.Data.(core.ToolCallData); got != (core.ToolCallData{ToolCallID: "call_1", ToolName: "web_search"}) {
		t.Fatalf("succeeded data = %+v", got)
	}

	failed, err := ProjectRunEvent(core.EventRecord{RunID: "run_1", Sequence: 7, Kind: "tool.call.failed", Payload: map[string]any{
		"tool_call_id": "call_2", "tool_name": "web_fetch", "error": strings.Repeat("e", 400),
	}})
	if err != nil {
		t.Fatalf("project failed: %v", err)
	}
	if got := failed.Data.(core.ToolCallData).Error; len([]rune(got)) != toolCallErrorRunes+1 || !strings.HasSuffix(got, "…") {
		t.Fatalf("failed error not clipped: %d runes", len([]rune(got)))
	}
}

func TestToolCallSummaryPicksTheTellingArgument(t *testing.T) {
	for _, tc := range []struct{ args, want string }{
		{`{"path":"inbox/rust.md","query":"rust"}`, "rust"},
		{`{"url":"https://go.dev/blog"}`, "https://go.dev/blog"},
		{`{"path":"briefings/2026-10-11.md"}`, "briefings/2026-10-11.md"},
		{`{"limit":3}`, ""},
		{``, ""},
		{`{"content":"` + strings.Repeat("长", 100) + `"}`, strings.Repeat("长", toolCallSummaryRunes) + "…"},
	} {
		if got := toolCallSummary(tc.args); got != tc.want {
			t.Fatalf("toolCallSummary(%q) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestToolStepsFoldCallsInStartOrder(t *testing.T) {
	events := []core.EventRecord{
		{RunID: "run_1", Sequence: 1, Kind: "tool.call.started", Payload: map[string]any{"tool_call_id": "a", "tool_name": "recall", "arguments": `{"query":"wifi"}`}},
		{RunID: "run_1", Sequence: 2, Kind: "tool.call.started", Payload: map[string]any{"tool_call_id": "b", "tool_name": "web_fetch", "arguments": `{"url":"https://x.dev"}`}},
		{RunID: "run_1", Sequence: 3, Kind: "tool.call.succeeded", Payload: map[string]any{"tool_call_id": "a", "tool_name": "recall"}},
		{RunID: "run_1", Sequence: 4, Kind: "tool.call.failed", Payload: map[string]any{"tool_call_id": "b", "tool_name": "web_fetch", "error": "HTTP 503"}},
		{RunID: "run_1", Sequence: 5, Kind: "tool.call.started", Payload: map[string]any{"tool_call_id": "c", "tool_name": "notify_owner", "arguments": `{"message":"done"}`}},
		// Recorded before tool.call.started existed: a result with no start.
		{RunID: "run_1", Sequence: 6, Kind: "tool.call.succeeded", Payload: map[string]any{"tool_call_id": "d", "tool_name": "knowledge_list"}},
	}
	steps, err := toolSteps(events)
	if err != nil {
		t.Fatalf("toolSteps: %v", err)
	}
	want := []ToolStep{
		{ToolName: "recall", Summary: "wifi", Outcome: ToolStepSucceeded},
		{ToolName: "web_fetch", Summary: "https://x.dev", Outcome: ToolStepFailed, Error: "HTTP 503"},
		{ToolName: "notify_owner", Summary: "done", Outcome: ToolStepRunning},
		{ToolName: "knowledge_list", Outcome: ToolStepSucceeded},
	}
	if len(steps) != len(want) {
		t.Fatalf("steps = %+v", steps)
	}
	for i := range want {
		if steps[i] != want[i] {
			t.Fatalf("step %d = %+v, want %+v", i, steps[i], want[i])
		}
	}
}

func TestToolApprovalSummaryNamesTheToolAndItsTarget(t *testing.T) {
	record := core.PendingActionRecord{
		ActionID: "action_1", RunID: "run_1", Kind: core.PendingActionKindToolApproval, Status: core.PendingActionStatusPending,
		Subject:     "Approve web_fetch",
		PayloadJSON: `{"message":"web_fetch wants to run","tool_name":"web_fetch","arguments":"{\"url\":\"https://go.dev/blog\"}"}`,
	}
	summary, err := buildPendingActionSummary(record, core.RunRecord{RunID: "run_1", SessionID: "thread_1"})
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.ToolName != "web_fetch" || summary.ToolSummary != "https://go.dev/blog" || summary.ThreadID != "thread_1" {
		t.Fatalf("summary = %+v", summary)
	}
	record.PayloadJSON = `{"tool_name":"shell","arguments":"{\"command\":\"bsk click @e3 --session s1\"}","summary":"在浏览器里点击「提交订单」 · 订单结算页 —— 涉及付款"}`
	reviewed, err := buildPendingActionSummary(record, core.RunRecord{RunID: "run_1", SessionID: "thread_1"})
	if err != nil {
		t.Fatalf("reviewed summary: %v", err)
	}
	if reviewed.ToolName != "shell" || reviewed.ToolSummary != "在浏览器里点击「提交订单」 · 订单结算页 —— 涉及付款" {
		t.Fatalf("reviewed summary = %+v", reviewed)
	}
}
