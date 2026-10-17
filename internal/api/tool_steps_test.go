package api

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestSkillWriteApprovalCarriesTheWholeSkill(t *testing.T) {
	markdown := "---\nname: shop-orders\ndescription: Look up recent orders on the shop site.\n---\n1. Run scripts/orders.py.\n"
	arguments, err := json.Marshal(map[string]any{"markdown": markdown, "files": map[string]string{"scripts/orders.py": "print('orders')\n"}, "delete": []string{"old.md"}, "base_revision": 2})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{"tool_name": "skill_write", "arguments": string(arguments)})
	if err != nil {
		t.Fatal(err)
	}
	record := core.PendingActionRecord{
		ActionID: "action_1", RunID: "run_1", Kind: core.PendingActionKindToolApproval, Status: core.PendingActionStatusPending,
		Subject: "Approve skill_write", PayloadJSON: string(payload),
	}
	summary, err := buildPendingActionSummary(record, core.RunRecord{RunID: "run_1", SessionID: "thread_1"})
	if err != nil {
		t.Fatal(err)
	}
	if summary.ToolSummary != "shop-orders: Look up recent orders on the shop site." {
		t.Fatalf("summary = %q", summary.ToolSummary)
	}
	service := NewPendingActionService(nil).WithSkillPackages(stagedPackagesStub{})
	want := strings.TrimSpace(markdown) + "\n\n── scripts/orders.py (16 bytes) ──\nprint('orders')\n\n── old.md (removed) ──"
	if got, err := service.toolDetail(context.Background(), "skill_write", string(arguments)); err != nil || got != want {
		t.Fatalf("detail = %q, %v", got, err)
	}
	if got, err := service.toolDetail(context.Background(), "shell", `{"command":"ls"}`); err != nil || got != "" {
		t.Fatalf("shell detail = %q, %v", got, err)
	}
	if got := toolApprovalSummary("skill_retire", `{"name":"shop-orders","base_revision":3}`); got != "shop-orders" {
		t.Fatalf("retire summary = %q", got)
	}
	if got := toolApprovalSummary("skill_import", `{"name":"pdf","sha256":"abc"}`); got != "pdf" {
		t.Fatalf("import summary = %q", got)
	}
}

type stagedPackagesStub map[string]core.SkillPackage

func (s stagedPackagesStub) StagedSkillPackage(_ context.Context, sha256 string) (core.SkillPackage, error) {
	pkg, ok := s[sha256]
	if !ok {
		return core.SkillPackage{}, core.ErrSkillPackageNotStaged
	}
	return pkg, nil
}

// SkillFiles reads a revision stored as the package staged under
// "<id>@<revision>".
func (s stagedPackagesStub) SkillFiles(_ context.Context, id string, revision int64) ([]core.SkillFile, error) {
	return s[fmt.Sprintf("%s@%d", id, revision)].Files, nil
}

func TestSkillImportApprovalShowsTheStagedPackage(t *testing.T) {
	service := NewPendingActionService(nil).WithSkillPackages(stagedPackagesStub{"abc": {
		SHA256: "abc", Name: "pdf", Markdown: "---\nname: pdf\ndescription: Fill PDF forms.\n---\nRun the script.\n",
		Files: []core.SkillFile{{Path: "scripts/fill.py", Content: []byte("print(1)\n")}, {Path: "assets/logo.png", Content: []byte{0x89, 0xff, 0x00}}},
	}})
	got, err := service.toolDetail(context.Background(), "skill_import", `{"name":"pdf","sha256":"abc"}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "---\nname: pdf\ndescription: Fill PDF forms.\n---\nRun the script.\n\n── assets/logo.png (3 bytes) ──\n(binary)\n\n── scripts/fill.py (9 bytes) ──\nprint(1)"
	if got != want {
		t.Fatalf("detail = %q", got)
	}
	got, err = service.toolDetail(context.Background(), "skill_import", `{"name":"pdf","sha256":"gone"}`)
	if err != nil || !strings.Contains(got, "not staged") {
		t.Fatalf("unstaged detail = %q, %v", got, err)
	}
}

func TestSkillApprovalsListThePackageManifestFirst(t *testing.T) {
	manifest := "mcp:\n  command: uvx\n  args: [mcp-server-github, --read-only]\n  secrets: [GITHUB_TOKEN]\n"
	markdown := "---\nname: github\ndescription: Read GitHub issues.\n---\nUse the tools.\n"
	service := NewPendingActionService(nil).WithSkillPackages(stagedPackagesStub{
		"abc": {SHA256: "abc", Name: "github", Markdown: markdown, Files: []core.SkillFile{{Path: "acorn-package.yaml", Content: []byte(manifest)}}},
		// Revision 1 of the stored skill.
		"github@1": {Files: []core.SkillFile{{Path: "acorn-package.yaml", Content: []byte(manifest)}, {Path: "notes.md", Content: []byte("n")}}},
	})
	ctx := context.Background()
	header := "MCP server, run on the workstation from the package's directory:\nuvx mcp-server-github --read-only\nSecrets: GITHUB_TOKEN\n\n"
	imported, err := service.toolDetail(ctx, "skill_import", `{"name":"github","sha256":"abc"}`)
	if err != nil || !strings.HasPrefix(imported, header+"---\nname: github") || !strings.Contains(imported, "── acorn-package.yaml (") {
		t.Fatalf("import detail = %q, %v", imported, err)
	}

	write := func(args map[string]any) string {
		t.Helper()
		args["markdown"] = markdown
		data, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		got, err := service.toolDetail(ctx, "skill_write", string(data))
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := write(map[string]any{"files": map[string]string{"acorn-package.yaml": manifest}}); !strings.HasPrefix(got, header) {
		t.Fatalf("new package detail = %q", got)
	}
	kept := write(map[string]any{"files": map[string]string{"notes.md": "m"}, "base_revision": 1})
	if !strings.HasPrefix(kept, "MCP server (unchanged), run on the workstation from the package's directory:\nuvx mcp-server-github --read-only\nSecrets: GITHUB_TOKEN\n\n---") {
		t.Fatalf("edit detail = %q", kept)
	}
	if got := write(map[string]any{"delete": []string{"acorn-package.yaml"}, "base_revision": 1}); !strings.HasPrefix(got, "Removes acorn-package.yaml: the package no longer runs an MCP server.\n\n---") {
		t.Fatalf("removal detail = %q", got)
	}
	if got := write(map[string]any{"files": map[string]string{"acorn-package.yaml": "mcp:\n  command: uvx\nextra: 1\n"}}); !strings.HasPrefix(got, "acorn-package.yaml does not parse, and the write will fail") {
		t.Fatalf("bad manifest detail = %q", got)
	}
	if got := write(map[string]any{"files": map[string]string{"notes.md": "m"}}); strings.Contains(got, "MCP server") {
		t.Fatalf("a package without a manifest shows none: %q", got)
	}
	data, err := json.Marshal(map[string]any{"markdown": "no frontmatter", "base_revision": 1})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := service.toolDetail(ctx, "skill_write", string(data)); err != nil || !strings.HasPrefix(got, "SKILL.md does not parse, and the write will fail: ") || !strings.HasSuffix(got, "no frontmatter") {
		t.Fatalf("unparsable skill detail = %q, %v", got, err)
	}
}
