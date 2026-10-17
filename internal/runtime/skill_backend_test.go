package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
)

func testSkillSnapshot() *skills.Snapshot {
	return &skills.Snapshot{Skills: []skills.View{
		{Spec: skills.Spec{Name: "capture-to-note", Description: "Turn a capture into a note. Use when the owner shares a link.", Instruction: "Fetch the link, then knowledge_write.", Revision: 3, Markdown: "---\nname: capture-to-note\n---\nFetch the link, then knowledge_write.\n"}, Eligible: true},
		{Spec: skills.Spec{Name: "pdf", Description: "Fill PDF forms.", Instruction: "Run scripts/fill.py.", Revision: 2, Markdown: "---\nname: pdf\n---\nRun scripts/fill.py.\n", Files: []core.SkillFileInfo{{Path: "references/forms.md"}, {Path: "scripts/fill.py"}}}, Eligible: true},
		{Spec: skills.Spec{Name: "web-browser-research", Description: "Browse.", Instruction: "Use the browser."}, Eligible: false},
	}}
}

func TestSkillBackendServesEligibleSkills(t *testing.T) {
	ctx := context.Background()
	backend := newSkillBackend(testSkillSnapshot(), "/home/agent/skills")
	matters, err := backend.List(ctx)
	if err != nil || len(matters) != 2 || matters[0].Name != "capture-to-note" || matters[1].Name != "pdf" {
		t.Fatalf("list = %+v, %v", matters, err)
	}
	if matters[0].Description != "Turn a capture into a note. Use when the owner shares a link." {
		t.Fatalf("description = %q", matters[0].Description)
	}
	if _, err := backend.Get(ctx, "web-browser-research"); err == nil {
		t.Fatal("an ineligible skill must not load")
	}
	pdf, err := backend.Get(ctx, "pdf")
	if err != nil {
		t.Fatal(err)
	}
	content, err := backend.content(ctx, pdf, "")
	if err != nil || !strings.Contains(content, "Skill pdf, revision 2. Its files are in /home/agent/skills/pdf on your workstation: references/forms.md, scripts/fill.py.") || !strings.HasSuffix(content, "Run scripts/fill.py.\n") {
		t.Fatalf("pdf content = %q, %v", content, err)
	}
}

func TestSkillToolLoadsSkillsThroughTheToolChain(t *testing.T) {
	ctx := context.Background()
	store := newMemPresenceStore()
	skillHandler, err := newSkillMiddleware(ctx, testSkillSnapshot(), "/home/agent/skills")
	if err != nil {
		t.Fatal(err)
	}
	present := newTestPresenceMiddleware(t, store)
	model := &scriptedModel{replies: []*schema.AgenticMessage{
		toolCallReply("call_0", "skill", `{"skill":"capture-to-note"}`),
		toolCallReply("call_1", "skill", `{"skill":"made-up"}`),
		assistantMessage("done", nil),
	}}
	agent, err := adk.NewTypedChatModelAgent(ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
		Name:        "skill_test",
		Description: "skill test agent",
		Instruction: "You are Acorn.",
		Model:       model,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools:               []einotool.BaseTool{&echoTool{}},
			ExecuteSequentially: true,
			UnknownToolsHandler: unknownToolResult,
		}},
		Handlers:      []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]{skillHandler, present, newToolErrorMiddleware(newFailedToolCalls())},
		MaxIterations: 5,
	})
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	runner := adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{Agent: agent})
	if _, err := drainEvents(t, runner.Run(ctx, []adk.AgenticMessage{schema.UserAgenticMessage("[capture] https://example.com")})); err != nil {
		t.Fatalf("an unknown skill failed the run: %v", err)
	}

	var loaded string
	for _, msg := range model.inputs[1] {
		if msg.Role == schema.AgenticRoleTypeUser && toolResultID(msg) == "call_0" {
			loaded = messageText(msg)
		}
	}
	if !strings.Contains(loaded, "Skill capture-to-note, revision 3.\n\n---\nname: capture-to-note\n---\nFetch the link, then knowledge_write.") {
		t.Fatalf("skill tool result = %q", loaded)
	}
	assertLastToolMessage(t, model, `skill "made-up" is not available`)

	for _, content := range store.snapshots {
		if !strings.Contains(content, "You are Acorn.") || !strings.Contains(content, "The skill tool lists the skills you have") {
			t.Fatalf("snapshot lacks the final instruction:\n%s", content)
		}
	}
	if len(store.snapshots) == 0 {
		t.Fatal("no snapshot recorded")
	}
}

type fakeSyncLibrary struct {
	synced *skills.Snapshot
	err    error
}

func (l *fakeSyncLibrary) Scan(context.Context) (*skills.ScanResult, error) {
	return &skills.ScanResult{}, nil
}

func (l *fakeSyncLibrary) Sync(_ context.Context, _ skills.PackageHost, snapshot skills.Snapshot) (string, error) {
	l.synced = &snapshot
	return "/home/agent/skills", l.err
}

func (l *fakeSyncLibrary) Serve(_ context.Context, _ skills.ServerHost, snapshot skills.Snapshot) (skills.Snapshot, error) {
	return snapshot, nil
}

type nopPackageHost struct{}

func (nopPackageHost) SkillPackages(context.Context) (string, map[string]string, error) {
	return "", nil, nil
}

func (nopPackageHost) PutSkillPackage(context.Context, string, string, string, []core.SkillFile) error {
	return nil
}

func (nopPackageHost) DeleteSkillPackage(context.Context, string) error { return nil }

func TestSkillSyncFailureTakesOnlySkillsWithFilesOutOfTheRun(t *testing.T) {
	ctx := context.Background()
	snapshot := *testSkillSnapshot()
	library := &fakeSyncLibrary{}
	if dir, out := syncSkillPackages(ctx, library, nil, snapshot); dir != "" || library.synced != nil || !out.Skills[1].Eligible {
		t.Fatalf("no host: dir = %q, synced = %v", dir, library.synced)
	}
	if dir, out := syncSkillPackages(ctx, library, nopPackageHost{}, snapshot); dir != "/home/agent/skills" || library.synced == nil || !out.Skills[1].Eligible {
		t.Fatalf("synced: dir = %q, out = %+v", dir, out)
	}
	library.err = errors.New("workstation down")
	dir, out := syncSkillPackages(ctx, library, nopPackageHost{}, snapshot)
	if dir != "" || !out.Skills[0].Eligible || out.Skills[1].Eligible || out.Skills[1].DisabledReasons[0] != "workstation_sync_failed" {
		t.Fatalf("failed sync: dir = %q, out = %+v", dir, out)
	}
	if !snapshot.Skills[1].Eligible {
		t.Fatal("sync changed the snapshot it was given")
	}
}
