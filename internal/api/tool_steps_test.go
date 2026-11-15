package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tools"
)

func TestProjectToolCallEventsCarrySummaryNotOutput(t *testing.T) {
	started, err := ProjectRunEvent(core.EventRecord{RunID: "run_1", Sequence: 5, Kind: "tool.call.started", Payload: map[string]any{
		"tool_call_id": "call_1", "tool_name": "web_search", "arguments": `{"limit":5,"query":"  go 1.28\n release  "}`,
	}})
	if err != nil {
		t.Fatalf("project started: %v", err)
	}
	if got := started.Data.(ToolCallData); got != (ToolCallData{ToolCallID: "call_1", ToolName: "web_search", Summary: "go 1.28 release"}) {
		t.Fatalf("started data = %+v", got)
	}

	succeeded, err := ProjectRunEvent(core.EventRecord{RunID: "run_1", Sequence: 6, Kind: "tool.call.succeeded", Payload: map[string]any{
		"tool_call_id": "call_1", "tool_name": "web_search", "output": "secret search results",
	}})
	if err != nil {
		t.Fatalf("project succeeded: %v", err)
	}
	if got := succeeded.Data.(ToolCallData); got != (ToolCallData{ToolCallID: "call_1", ToolName: "web_search"}) {
		t.Fatalf("succeeded data = %+v", got)
	}

	failed, err := ProjectRunEvent(core.EventRecord{RunID: "run_1", Sequence: 7, Kind: "tool.call.failed", Payload: map[string]any{
		"tool_call_id": "call_2", "tool_name": "web_fetch", "error": strings.Repeat("e", 400),
	}})
	if err != nil {
		t.Fatalf("project failed: %v", err)
	}
	if got := failed.Data.(ToolCallData).Error; len([]rune(got)) != toolCallErrorRunes+1 || !strings.HasSuffix(got, "…") {
		t.Fatalf("failed error not clipped: %d runes", len([]rune(got)))
	}
}

func TestToolCallSummaryPicksTheTellingArgument(t *testing.T) {
	for _, tc := range []struct{ args, want string }{
		{`{"path":"inbox/rust.md","query":"rust"}`, "rust"},
		{`{"url":"https://go.dev/blog"}`, "https://go.dev/blog"},
		{`{"path":"briefings/2026-10-11.md"}`, "briefings/2026-10-11.md"},
		{`{"action":"start","objective":"读 A","acceptance":["三条要点"]}`, "读 A"},
		{`{"action":"redo","task":2,"feedback":"要对照原文"}`, "要对照原文"},
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

// newSkillApprovals is the pending-action service and the describer the gate
// asks, over one skill library.
func newSkillApprovals(t *testing.T) (skillTestEnv, *PendingActionService, tools.ApprovalDescriber) {
	t.Helper()
	env := newSkillTestEnv(t)
	return env, NewPendingActionService(nil).WithSkillPackages(env.db).WithSkillChanges(env.library), tools.ApprovalDescriber{Skills: env.library}
}

func callArguments(t *testing.T, args map[string]any) string {
	t.Helper()
	data, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSkillWriteApprovalSaysWhatTheWriteChanges(t *testing.T) {
	env, service, describe := newSkillApprovals(t)
	ctx := t.Context()
	markdown := "---\nname: shop-orders\ndescription: Look up recent orders on the shop site.\n---\n1. Run scripts/orders.py.\n"
	create := callArguments(t, map[string]any{"markdown": markdown, "files": map[string]string{"scripts/orders.py": "print('orders')\n"}})
	created := "新建技能 shop-orders，附 1 个文件：Look up recent orders on the shop site."
	if got, err := describe.DescribeApproval(ctx, "skill_write", create); err != nil || got != created {
		t.Fatalf("new skill = %q, %v", got, err)
	}
	want := created + "\n\n" + strings.TrimSpace(markdown) + "\n\n── scripts/orders.py (16 bytes) ──\nprint('orders')"
	if got, err := service.toolDetail(ctx, "skill_write", create); err != nil || got != want {
		t.Fatalf("new skill detail = %q, %v", got, err)
	}

	if _, err := env.library.Write(ctx, skills.WriteRequest{Markdown: markdown, Put: map[string]string{"scripts/orders.py": "print('orders')\n", "old.md": "old\n"}}); err != nil {
		t.Fatal(err)
	}
	next := strings.Replace(markdown, "orders.py.", "orders.py --all.", 1)
	rewrite := callArguments(t, map[string]any{
		"markdown": next, "files": map[string]string{"scripts/orders.py": "print('all orders')\n", "notes.md": "n\n"}, "delete": []string{"old.md"}, "base_revision": 1,
	})
	rewritten := "改写技能 shop-orders（现为第 1 版）：SKILL.md 删 1 行、加 1 行；新增文件 notes.md；改动文件 scripts/orders.py；删除文件 old.md"
	if got, err := describe.DescribeApproval(ctx, "skill_write", rewrite); err != nil || got != rewritten {
		t.Fatalf("rewrite = %q, %v", got, err)
	}
	want = rewritten +
		"\n\n── SKILL.md 相对第 1 版的改动 ──\n@@ -2 +2 @@\n name: shop-orders\n description: Look up recent orders on the shop site.\n ---\n-1. Run scripts/orders.py.\n+1. Run scripts/orders.py --all." +
		"\n\n── scripts/orders.py 相对第 1 版的改动 ──\n@@ -1 +1 @@\n-print('orders')\n+print('all orders')" +
		"\n\n── 写入的全文 ──\n" + strings.TrimSpace(next) +
		"\n\n── notes.md (2 bytes) ──\nn\n\n── scripts/orders.py (20 bytes) ──\nprint('all orders')\n\n── old.md (removed) ──"
	if got, err := service.toolDetail(ctx, "skill_write", rewrite); err != nil || got != want {
		t.Fatalf("rewrite detail = %q, %v", got, err)
	}

	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"another revision", map[string]any{"markdown": next, "base_revision": 7}, "技能 shop-orders 现在是第 1 版，这次写入基于第 7 版，会失败。"},
		{"as a new skill", map[string]any{"markdown": next}, "技能 shop-orders 已经有了（第 1 版），这次写入按新建提交，会失败。"},
		{"a skill that is not there", map[string]any{"markdown": strings.Replace(next, "shop-orders", "shop-returns", 1), "base_revision": 2}, "技能 shop-returns 不存在，这次写入基于第 2 版，会失败。"},
		{"a file that is not there", map[string]any{"markdown": next, "delete": []string{"gone.md"}, "base_revision": 1}, `技能 shop-orders 的这次写入会失败：skill shop-orders has no file "gone.md" to delete`},
		{"the same package", map[string]any{"markdown": markdown, "base_revision": 1}, "改写技能 shop-orders（现为第 1 版）：内容与当前版本相同"},
	} {
		got, err := describe.DescribeApproval(ctx, "skill_write", callArguments(t, tc.args))
		if err != nil || got != tc.want {
			t.Errorf("%s = %q, %v", tc.name, got, err)
		}
	}
	if current, err := env.library.Skill(ctx, "shop-orders"); err != nil || current.Revision != 1 {
		t.Fatalf("describing a write stored something: %+v, %v", current, err)
	}

	// The card reads what the gate recorded when it asked; a tool that
	// writes no skill reads as its arguments say.
	record := core.PendingActionRecord{
		ActionID: "action_1", RunID: "run_1", Kind: core.PendingActionKindToolApproval, Status: core.PendingActionStatusPending, Subject: "Approve skill_write",
		PayloadJSON: callArguments(t, map[string]any{"tool_name": "skill_write", "arguments": rewrite, "summary": rewritten}),
	}
	summary, err := buildPendingActionSummary(record, core.RunRecord{RunID: "run_1", SessionID: "thread_1"})
	if err != nil || summary.ToolSummary != rewritten {
		t.Fatalf("summary = %q, %v", summary.ToolSummary, err)
	}
	if got, err := service.toolDetail(ctx, "shell", `{"command":"ls"}`); err != nil || got != "" {
		t.Fatalf("shell detail = %q, %v", got, err)
	}
	if got := toolCallSummary(`{"name":"shop-orders","base_revision":3}`); got != "shop-orders" {
		t.Fatalf("retire summary = %q", got)
	}
}

// The agent once rewrote the skill that says how to write skills with a text
// of its own and two files named after another tool's arguments, and the
// approval read like one for the skill the owner had asked for.
func TestRewritingABuiltInSkillSaysSoOnItsApproval(t *testing.T) {
	env, service, describe := newSkillApprovals(t)
	ctx := t.Context()
	if err := env.library.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	shipped, err := env.library.Skill(ctx, "skill-creator")
	if err != nil {
		t.Fatal(err)
	}
	arguments := callArguments(t, map[string]any{
		"markdown":      "---\nname: skill-creator\ndescription: Create new skills, modify and improve existing skills, and measure skill performance.\n---\nA skill is a folder of files.\n",
		"files":         map[string]string{"action": "read", "file": "skill-creator"},
		"base_revision": shipped.Revision,
	})
	summary, err := describe.DescribeApproval(ctx, "skill_write", arguments)
	if err != nil {
		t.Fatal(err)
	}
	lead := fmt.Sprintf("改写内置技能 skill-creator（现为第 %d 版）：SKILL.md 删 ", shipped.Revision)
	if !strings.HasPrefix(summary, lead) || !strings.HasSuffix(summary, " 行；新增文件 action、file") {
		t.Fatalf("summary = %q", summary)
	}
	detail, err := service.toolDetail(ctx, "skill_write", arguments)
	if err != nil {
		t.Fatal(err)
	}
	// The shipped text says what never goes into a skill; the rewrite drops
	// it, and the owner sees the line go.
	dropped := slices.IndexFunc(strings.Split(shipped.Markdown, "\n"), func(line string) bool { return strings.Contains(line, "never go into a skill") })
	if dropped < 0 {
		t.Fatal("the shipped skill-creator no longer says what never goes into a skill")
	}
	changed := fmt.Sprintf("\n\n── SKILL.md 相对第 %d 版的改动 ──\n@@ -1 +1 @@\n", shipped.Revision)
	if !strings.HasPrefix(detail, summary+changed) || !strings.Contains(detail, "\n-"+strings.Split(shipped.Markdown, "\n")[dropped]+"\n") ||
		!strings.Contains(detail, "\n\n── 写入的全文 ──\n---\nname: skill-creator\n") || !strings.HasSuffix(detail, "\n\n── action (4 bytes) ──\nread\n\n── file (13 bytes) ──\nskill-creator") {
		t.Fatalf("detail = %q", detail)
	}
	if current, err := env.library.Skill(ctx, "skill-creator"); err != nil || current.Revision != shipped.Revision {
		t.Fatalf("describing the write stored something: %+v, %v", current, err)
	}
}

func TestSkillImportApprovalSaysWhatThePackageChanges(t *testing.T) {
	env, service, describe := newSkillApprovals(t)
	ctx := t.Context()
	markdown := "---\nname: pdf\ndescription: Fill PDF forms.\n---\nRun the script.\n"
	staged, err := env.library.Stage(ctx, []core.SkillFile{
		{Path: "pdf/SKILL.md", Content: []byte(markdown)}, {Path: "pdf/scripts/fill.py", Content: []byte("print(1)\n")}, {Path: "pdf/assets/logo.png", Content: []byte{0x89, 0xff, 0x00}},
	})
	if err != nil {
		t.Fatal(err)
	}
	arguments := callArguments(t, map[string]any{"name": "pdf", "sha256": staged.SHA256})
	created := "导入技能包，新建技能 pdf，附 2 个文件：Fill PDF forms."
	if got, err := describe.DescribeApproval(ctx, "skill_import", arguments); err != nil || got != created {
		t.Fatalf("new skill = %q, %v", got, err)
	}
	want := created + "\n\n" + strings.TrimSpace(markdown) + "\n\n── assets/logo.png (3 bytes) ──\n(binary)\n\n── scripts/fill.py (9 bytes) ──\nprint(1)"
	if got, err := service.toolDetail(ctx, "skill_import", arguments); err != nil || got != want {
		t.Fatalf("detail = %q, %v", got, err)
	}
	unstaged := "技能 pdf 的这次导入会失败：skill package not staged: gone"
	if got, err := service.toolDetail(ctx, "skill_import", `{"name":"pdf","sha256":"gone"}`); err != nil || got != unstaged {
		t.Fatalf("unstaged detail = %q, %v", got, err)
	}
	misnamed := fmt.Sprintf("技能 docx 的这次导入会失败：staged package %s holds skill pdf, not docx", staged.SHA256)
	if got, err := describe.DescribeApproval(ctx, "skill_import", callArguments(t, map[string]any{"name": "docx", "sha256": staged.SHA256})); err != nil || got != misnamed {
		t.Fatalf("misnamed import = %q, %v", got, err)
	}

	if _, err := env.library.ImportStaged(ctx, "pdf", staged.SHA256, 0, "run_1"); err != nil {
		t.Fatal(err)
	}
	again, err := env.library.Stage(ctx, []core.SkillFile{
		{Path: "pdf/SKILL.md", Content: []byte(markdown)}, {Path: "pdf/scripts/fill.py", Content: []byte("print(2)\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	replaced := "导入技能包，改写技能 pdf（现为第 1 版）：SKILL.md 不变；改动文件 scripts/fill.py；删除文件 assets/logo.png"
	arguments = callArguments(t, map[string]any{"name": "pdf", "sha256": again.SHA256, "base_revision": 1})
	if got, err := describe.DescribeApproval(ctx, "skill_import", arguments); err != nil || got != replaced {
		t.Fatalf("replacement = %q, %v", got, err)
	}
	want = replaced + "\n\n── scripts/fill.py 相对第 1 版的改动 ──\n@@ -1 +1 @@\n-print(1)\n+print(2)\n\n── 写入的全文 ──\n" + strings.TrimSpace(markdown) + "\n\n── scripts/fill.py (9 bytes) ──\nprint(2)"
	if got, err := service.toolDetail(ctx, "skill_import", arguments); err != nil || got != want {
		t.Fatalf("replacement detail = %q, %v", got, err)
	}
	if _, err := env.db.StagedSkillPackage(ctx, again.SHA256); err != nil {
		t.Fatalf("describing an import took the package off the stage: %v", err)
	}
}

func TestSkillApprovalsListThePackageManifestBeforeTheText(t *testing.T) {
	env, service, _ := newSkillApprovals(t)
	ctx := t.Context()
	manifest := "mcp:\n  command: uvx\n  args: [mcp-server-github, --read-only]\ntriggers:\n  - name: issues\n    schedule: \"*/30 * * * *\"\n    run: scripts/issues.py --open\n    deliver: immediate\n  - name: weekly\n    schedule: \"0 9 * * 1\"\n    run: scripts/weekly.py\n    deliver: digest\nsecrets: [GITHUB_TOKEN]\n"
	markdown := "---\nname: github\ndescription: Read GitHub issues.\n---\nUse the tools.\n"
	staged, err := env.library.Stage(ctx, []core.SkillFile{
		{Path: "github/SKILL.md", Content: []byte(markdown)}, {Path: "github/acorn-package.yaml", Content: []byte(manifest)}, {Path: "github/notes.md", Content: []byte("n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	header := "Runs on the workstation from the package's directory:\nMCP server: uvx mcp-server-github --read-only\nTrigger issues at \"*/30 * * * *\" (owner's time), events start a run at once: scripts/issues.py --open\nTrigger weekly at \"0 9 * * 1\" (owner's time), events wait for the morning briefing: scripts/weekly.py\nSecrets: GITHUB_TOKEN\n\n"
	created := "新建技能 github，附 1 个文件：Read GitHub issues.\n\n"
	imported, err := service.toolDetail(ctx, "skill_import", callArguments(t, map[string]any{"name": "github", "sha256": staged.SHA256}))
	if err != nil || !strings.HasPrefix(imported, "导入技能包，新建技能 github，附 2 个文件：Read GitHub issues.\n\n"+header+"---\nname: github") || !strings.Contains(imported, "── acorn-package.yaml (") {
		t.Fatalf("import detail = %q, %v", imported, err)
	}

	write := func(args map[string]any) string {
		t.Helper()
		args["markdown"] = markdown
		got, err := service.toolDetail(ctx, "skill_write", callArguments(t, args))
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := write(map[string]any{"files": map[string]string{"acorn-package.yaml": manifest}}); !strings.HasPrefix(got, created+header+"---") {
		t.Fatalf("new package detail = %q", got)
	}
	if got := write(map[string]any{"files": map[string]string{"notes.md": "m"}}); strings.Contains(got, "MCP server") {
		t.Fatalf("a package without a manifest shows none: %q", got)
	}
	if got := write(map[string]any{"files": map[string]string{"acorn-package.yaml": "mcp:\n  command: uvx\nextra: 1\n"}}); !strings.HasPrefix(got, "技能 github 的这次写入会失败：") || !strings.Contains(got, "\n\nacorn-package.yaml does not parse, and the write will fail") {
		t.Fatalf("bad manifest detail = %q", got)
	}

	if _, err := env.library.ImportStaged(ctx, "github", staged.SHA256, 0, "run_1"); err != nil {
		t.Fatal(err)
	}
	kept := write(map[string]any{"files": map[string]string{"notes.md": "m"}, "base_revision": 1})
	if !strings.HasPrefix(kept, "改写技能 github（现为第 1 版）：SKILL.md 不变；改动文件 notes.md\n\n"+strings.Replace(header, "directory:", "directory (unchanged):", 1)+"── notes.md 相对第 1 版的改动 ──\n@@ -1 +1 @@\n-n\n+m\n\n── 写入的全文 ──\n---") {
		t.Fatalf("edit detail = %q", kept)
	}
	if got := write(map[string]any{"delete": []string{"acorn-package.yaml"}, "base_revision": 1}); !strings.HasPrefix(got, "改写技能 github（现为第 1 版）：SKILL.md 不变；删除文件 acorn-package.yaml\n\nRemoves acorn-package.yaml: the package no longer runs an MCP server or triggers.\n\n---") {
		t.Fatalf("removal detail = %q", got)
	}
	unparsable := callArguments(t, map[string]any{"markdown": "no frontmatter", "base_revision": 1})
	if got, err := service.toolDetail(ctx, "skill_write", unparsable); err != nil || !strings.HasPrefix(got, "这次写入会失败：") || !strings.HasSuffix(got, "\n\nno frontmatter") {
		t.Fatalf("unparsable skill detail = %q, %v", got, err)
	}
}

type newsStub struct {
	watches  map[int64]core.Watch
	sections []core.NewsSection
}

func (s newsStub) LoadWatch(_ context.Context, id int64) (*core.Watch, error) {
	w, ok := s.watches[id]
	if !ok {
		return nil, fmt.Errorf("%w: %d", core.ErrWatchNotFound, id)
	}
	return &w, nil
}

func (s newsStub) ListNewsSections(context.Context) ([]core.NewsSection, error) {
	return s.sections, nil
}

func TestNewsSourceApprovalNamesTheSource(t *testing.T) {
	service := NewPendingActionService(nil).WithNews(newsStub{
		watches:  map[int64]core.Watch{12: {ID: 12, Name: "知乎热榜", Target: "hot-lists/zhihu", Section: "热榜"}},
		sections: []core.NewsSection{{Name: "国际与财经", Position: 2, Preference: "1. 大国关系"}},
	})
	ctx := context.Background()
	args := `{"action":"promote","id":12,"reason":"一周 70 条，3 条 4 分以上"}`
	if got, err := service.toolDetail(ctx, "news_source", args); err != nil || got != "把新闻源「知乎热榜」（板块：热榜）转正，从下一份早安卡起采用它。理由：一周 70 条，3 条 4 分以上" {
		t.Fatalf("detail = %q, %v", got, err)
	}
	sections := `{"action":"set","name":"国际与财经","preference":"1. 大国关系\n2. 小国央行少报","reason":"两次少报小国央行加息"}`
	if got, err := service.toolDetail(ctx, "news_sections", sections); err != nil || got != "板块「国际与财经」的偏好改为：\n1. 大国关系\n2. 小国央行少报\n原来是：\n1. 大国关系\n理由：两次少报小国央行加息" {
		t.Fatalf("sections detail = %q, %v", got, err)
	}
	write := `{"action":"write","path":"~/.bashrc","content":"umask 077\n"}`
	if got, err := service.toolDetail(ctx, "files", write); err != nil || got != "写入 ~/.bashrc，全文如下：\n\numask 077\n" {
		t.Fatalf("files detail = %q, %v", got, err)
	}
	// The list and the phone read the description stored with the approval.
	payload, err := json.Marshal(map[string]any{"tool_name": "news_source", "arguments": args, "summary": "把新闻源「知乎热榜」（板块：热榜）转正"})
	if err != nil {
		t.Fatal(err)
	}
	summary, err := buildPendingActionSummary(core.PendingActionRecord{
		ActionID: "action_1", RunID: "run_1", Kind: core.PendingActionKindToolApproval, Status: core.PendingActionStatusPending,
		Subject: "Approve news_source", PayloadJSON: string(payload),
	}, core.RunRecord{RunID: "run_1", SessionID: "thread_1"})
	if err != nil || summary.ToolSummary != "把新闻源「知乎热榜」（板块：热榜）转正" {
		t.Fatalf("summary = %+v, %v", summary, err)
	}
}

// A plan reaches clients as plan.updated; the calls that wrote it stay out of
// the live events and of the reply's steps, and the reply keeps the latest plan.
func TestPlanReachesClientsInPlaceOfItsCalls(t *testing.T) {
	plan := func(seq int64, steps ...any) core.EventRecord {
		return core.EventRecord{RunID: "run_1", Sequence: seq, Kind: core.EventPlanUpdated, Payload: map[string]any{"steps": steps}}
	}
	step := func(label, status string) map[string]any { return map[string]any{"label": label, "status": status} }
	events := []core.EventRecord{
		{RunID: "run_1", Sequence: 1, Kind: "tool.call.started", Payload: map[string]any{"tool_call_id": "p1", "tool_name": core.PlanToolName, "arguments": `{"steps":[]}`}},
		plan(2, step("读三篇文章", "running"), step("写汇总", "pending")),
		{RunID: "run_1", Sequence: 3, Kind: "tool.call.succeeded", Payload: map[string]any{"tool_call_id": "p1", "tool_name": core.PlanToolName}},
		{RunID: "run_1", Sequence: 4, Kind: "tool.call.started", Payload: map[string]any{"tool_call_id": "w1", "tool_name": "web_fetch", "arguments": `{"url":"https://a.dev"}`}},
		{RunID: "run_1", Sequence: 5, Kind: "tool.call.succeeded", Payload: map[string]any{"tool_call_id": "w1", "tool_name": "web_fetch"}},
		{RunID: "run_1", Sequence: 6, Kind: "tool.call.failed", Payload: map[string]any{"tool_call_id": "p2", "tool_name": core.PlanToolName, "error": "2 steps are running"}},
		plan(7, map[string]any{"label": "读三篇文章", "status": "done", "note": "3/3"}, step("写汇总", "running")),
	}

	live, err := projectLiveEvents(events)
	if err != nil {
		t.Fatalf("project live events: %v", err)
	}
	var kinds []string
	for _, event := range live {
		kinds = append(kinds, event.Type)
	}
	if want := []string{core.EventPlanUpdated, "tool.call.started", "tool.call.succeeded", core.EventPlanUpdated}; fmt.Sprint(kinds) != fmt.Sprint(want) {
		t.Fatalf("live kinds = %v, want %v", kinds, want)
	}
	if got := live[3].Data.(PlanData); fmt.Sprint(got.Steps) != fmt.Sprint([]PlanStepData{{Label: "读三篇文章", Status: "done", Note: "3/3"}, {Label: "写汇总", Status: "running"}}) {
		t.Fatalf("latest live plan = %+v", got)
	}

	steps, err := toolSteps(events)
	if err != nil {
		t.Fatalf("toolSteps: %v", err)
	}
	if len(steps) != 1 || steps[0] != (ToolStep{ToolName: "web_fetch", Summary: "https://a.dev", Outcome: ToolStepSucceeded}) {
		t.Fatalf("steps = %+v", steps)
	}
	latest, err := latestPlan(events)
	if err != nil || fmt.Sprint(latest) != fmt.Sprint(live[3].Data.(PlanData).Steps) {
		t.Fatalf("latest plan = %+v, %v", latest, err)
	}
	if none, err := latestPlan(events[3:5]); none != nil || err != nil {
		t.Fatalf("plan of a run without one = %+v, %v", none, err)
	}

	for _, payload := range []map[string]any{
		{"steps": []any{}},
		{"steps": []any{step("", "running")}},
		{"steps": []any{step("读", "in_progress")}},
		{"steps": "读"},
	} {
		if _, err := ProjectRunEvent(core.EventRecord{RunID: "run_1", Sequence: 9, Kind: core.EventPlanUpdated, Payload: payload}); !errors.Is(err, ErrClientProjectionFailed) {
			t.Fatalf("payload %v err = %v", payload, err)
		}
	}
}
