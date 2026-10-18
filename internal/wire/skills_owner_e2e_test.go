package wire

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
)

// TestOwnerRollbackReachesTheNextRun has the agent change a skill, the owner
// roll the change back from the App, and the next run load the rolled-back
// content.
func TestOwnerRollbackReachesTheNextRun(t *testing.T) {
	provider := &fakeOpenAI{replies: []string{
		toolCallChunk("call_skill_1", "skill", map[string]string{"skill": "orders"}),
		textReply("按新流程导出了"),
		toolCallChunk("call_skill_2", "skill", map[string]string{"skill": "orders"}),
		textReply("按原来的流程看了最新订单"),
	}}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, "")
	ctx := context.Background()
	c, err := NewContainer(ctx, cfg)
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	defer c.Close()

	const original, changed = "Open the orders page and read the latest order.", "Export every order to a spreadsheet."
	if _, err := c.Skills().Import(ctx, []core.SkillFile{{Path: skills.SkillFileName, Content: []byte("---\nname: orders\ndescription: Check the owner's orders.\n---\n" + original + "\n")}}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := c.skillLibrary.Write(ctx, skills.WriteRequest{Markdown: "---\nname: orders\ndescription: Check the owner's orders.\n---\n" + changed + "\n", BaseRevision: 1}); err != nil {
		t.Fatal(err)
	}
	thread, err := c.threads.CreateThread(ctx, "订单")
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.runs.CreateRun(ctx, thread.ID, "", "看看订单", nil)
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, c, first.ID, "completed")
	if loaded := toolResult(t, provider.request(1), "call_skill_1"); !strings.Contains(loaded, changed) {
		t.Fatalf("the agent's revision was not loaded: %q", loaded)
	}

	phone := newPairedClient(t, c)
	var history api.SkillRevisionListResponse
	phone.do(t, httptest.NewRequest(http.MethodGet, "/v1/skills/orders/revisions", nil), http.StatusOK, &history)
	if len(history.Revisions) != 2 || history.Revisions[0].Author != "agent" {
		t.Fatalf("history = %+v", history)
	}
	var diff api.SkillDiffDTO
	phone.do(t, httptest.NewRequest(http.MethodGet, "/v1/skills/orders/diff?from=2&to=1", nil), http.StatusOK, &diff)
	if len(diff.Files) != 1 || diff.Files[0].Path != skills.SkillFileName {
		t.Fatalf("diff = %+v", diff)
	}
	stale := httptest.NewRequest(http.MethodPost, "/v1/skills/orders:rollback", strings.NewReader(`{"revision":1,"base_revision":1}`))
	phone.do(t, stale, http.StatusConflict, nil)
	var rolled api.SkillSummaryDTO
	phone.do(t, httptest.NewRequest(http.MethodPost, "/v1/skills/orders:rollback", strings.NewReader(`{"revision":1,"base_revision":2}`)), http.StatusOK, &rolled)
	if rolled.Revision != 3 || rolled.Author != "owner" || !rolled.Eligible {
		t.Fatalf("rollback = %+v", rolled)
	}

	second, err := c.runs.CreateRun(ctx, thread.ID, "", "再看看订单", nil)
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, c, second.ID, "completed")
	if loaded := toolResult(t, provider.request(3), "call_skill_2"); !strings.Contains(loaded, original) || strings.Contains(loaded, changed) {
		t.Fatalf("the next run did not load the rolled-back content: %q", loaded)
	}
}
