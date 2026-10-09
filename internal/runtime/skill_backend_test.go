package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/skills"
)

func testSkillSnapshot() *skills.Snapshot {
	return &skills.Snapshot{Skills: []skills.View{
		{Spec: skills.Spec{ID: "skill.capture.to.note", Name: "Capture To Note", Summary: "Turn a capture into a note.", TriggerHints: []string{"[capture]", "save this link"}, Instruction: "Fetch the link, then knowledge_write.", Path: "/skills/capture_to_note"}, Eligible: true},
		{Spec: skills.Spec{ID: "skill.web.browser.research", Summary: "Browse.", Instruction: "Use the browser."}, Eligible: false},
	}}
}

func TestSkillBackendServesEligibleSkills(t *testing.T) {
	backend := newSkillBackend(testSkillSnapshot())
	matters, err := backend.List(context.Background())
	if err != nil || len(matters) != 1 || matters[0].Name != "skill.capture.to.note" {
		t.Fatalf("list = %+v, %v", matters, err)
	}
	if !strings.Contains(matters[0].Description, "Turn a capture into a note. Use for: [capture]; save this link.") {
		t.Fatalf("description = %q", matters[0].Description)
	}
	if _, err := backend.Get(context.Background(), "skill.web.browser.research"); err == nil {
		t.Fatal("an ineligible skill must not load")
	}
}

func TestSkillToolLoadsSkillsThroughTheToolChain(t *testing.T) {
	ctx := context.Background()
	store := newMemPresenceStore()
	skillHandler, err := newSkillMiddleware(ctx, testSkillSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	present := newTestPresenceMiddleware(t, store)
	model := &scriptedModel{replies: []*schema.AgenticMessage{
		toolCallReply("call_0", "skill", `{"skill":"skill.capture.to.note"}`),
		toolCallReply("call_1", "skill", `{"skill":"skill.made.up"}`),
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
	if !strings.Contains(loaded, "Fetch the link, then knowledge_write.") {
		t.Fatalf("skill tool result = %q", loaded)
	}
	assertLastToolMessage(t, model, `skill "skill.made.up" is not available`)

	for _, content := range store.snapshots {
		if !strings.Contains(content, "You are Acorn.") || !strings.Contains(content, "The skill tool lists the skills you have") {
			t.Fatalf("snapshot lacks the final instruction:\n%s", content)
		}
	}
	if len(store.snapshots) == 0 {
		t.Fatal("no snapshot recorded")
	}
}
