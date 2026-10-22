package wire

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/workstation"
)

// TestPinnedPanelsTakeTheirTriggersViewData has the agent show the latest
// release of a tracked repository as a view fed by a package trigger. The
// owner pins it to the now page; when the trigger finds a new version the
// panel's data changes without a model call. Data the view cannot draw
// fails the trigger and leaves the panel as it was, and an unpinned view is
// no longer fed.
func TestPinnedPanelsTakeTheirTriggersViewData(t *testing.T) {
	home := t.TempDir()
	ws, err := workstation.NewServer(workstation.ServerConfig{Home: home, Token: fileTestToken, Supervisor: workstation.NewSupervisor(io.Discard)})
	if err != nil {
		t.Fatal(err)
	}
	wsServer := httptest.NewServer(ws.Handler())
	defer wsServer.Close()
	defer ws.Close()
	latest := map[string]any{"title": "eino 最新版本", "source": "repo-releases/check", "components": []any{
		map[string]any{"id": "root", "component": "Column", "children": []any{"version", "notes"}},
		map[string]any{"id": "version", "component": "Text", "variant": "title", "text": map[string]any{"path": "/latest"}},
		map[string]any{"id": "notes", "component": "Link", "label": "发布说明", "url": map[string]any{"path": "/url"}},
	}, "data": map[string]any{"latest": "v0.9.21", "url": "https://github.com/cloudwego/eino/releases/tag/v0.9.21"}}
	provider := &fakeOpenAI{replies: []string{
		toolCallChunk("load_views", "tool_search", map[string]string{"query": "select:show_view"}),
		toolCallChunk("latest", "show_view", latest),
		textReply("eino 现在是 v0.9.21，可以把这个视图固定到今天页"),
	}}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, fmt.Sprintf("workstation:\n  url: %s\n  token: %s\n", wsServer.URL, fileTestToken), briefingOff)
	clock := &testClock{now: time.Date(2026, 10, 21, 9, 7, 0, 0, time.UTC)}
	ctx := context.Background()
	c, err := buildContainer(ctx, cfg, buildOptions{clock: clock.Now})
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	defer c.Close()
	tick := func(hour, minute int) error {
		t.Helper()
		clock.Set(time.Date(2026, 10, 21, hour, minute, 0, 0, time.UTC))
		return c.WakeScheduler().Tick(ctx)
	}
	release := func(version string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(home, "latest.txt"), []byte(version), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	check := "#!/bin/bash\n" +
		"[ -f \"$HOME/latest.txt\" ] || exit 0\n" +
		"v=$(cat \"$HOME/latest.txt\")\n" +
		"if [ \"$v\" = broken ]; then echo '{\"view_data\":{\"latest\":\"v0.9.23\"}}'; exit 0; fi\n" +
		"printf '{\"view_data\":{\"latest\":\"%s\",\"url\":\"https://github.com/cloudwego/eino/releases/tag/%s\"}}\\n' \"$v\" \"$v\"\n"
	if _, err := c.Skills().Import(ctx, []core.SkillFile{
		{Path: "SKILL.md", Content: []byte("---\nname: repo-releases\ndescription: Keep the latest release of the repositories the owner tracks.\n---\nThe check trigger feeds pinned views.\n")},
		{Path: skills.ManifestFileName, Content: []byte("triggers:\n  - name: check\n    schedule: \"*/30 * * * *\"\n    run: scripts/check.sh\n    deliver: digest\n")},
		{Path: "scripts/check.sh", Content: []byte(check)},
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := tick(9, 7); err != nil {
		t.Fatal(err)
	}

	client := newPairedClient(t, c)
	thread, err := c.threads.CreateThread(ctx, "仓库版本")
	if err != nil {
		t.Fatal(err)
	}
	var run api.RunDTO
	client.postJSON(t, "/v1/threads/"+thread.ID+"/runs", map[string]any{"input": "把 eino 的最新版本做成一个视图"}, http.StatusCreated, &run)
	waitRunStatus(t, c, run.ID, "completed")
	shown, err := c.store.ThreadViews(ctx, thread.ID)
	if err != nil || len(shown) != 1 || shown[0].Source != "repo-releases/check" {
		t.Fatalf("views = %+v, %v", shown, err)
	}
	viewPath := "/v1/threads/" + thread.ID + "/views/" + shown[0].ID
	now := func() api.NowResponse {
		t.Helper()
		var page api.NowResponse
		client.do(t, httptest.NewRequest(http.MethodGet, "/v1/now", nil), http.StatusOK, &page)
		return page
	}

	// Until it is pinned, the trigger's data does not touch the view.
	release("v0.9.22")
	if err := tick(9, 30); err != nil {
		t.Fatal(err)
	}
	if page := now(); len(page.Panels) != 0 {
		t.Fatalf("panels before pinning = %+v", page.Panels)
	}
	var pinned core.ViewData
	client.postJSON(t, viewPath+":pin", nil, http.StatusOK, &pinned)
	if pinned.PinnedAt == nil || !strings.Contains(string(pinned.Data), "v0.9.21") {
		t.Fatalf("pinned = %+v", pinned)
	}
	page := now()
	if len(page.Panels) != 1 || page.Panels[0].ThreadID != thread.ID || page.Panels[0].ThreadTitle != "仓库版本" || page.Panels[0].View.ID != shown[0].ID {
		t.Fatalf("panels = %+v", page.Panels)
	}
	var listed api.MessageListResponse
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/threads/"+thread.ID+"/messages", nil), http.StatusOK, &listed)
	if last := listed.Items[len(listed.Items)-1]; len(last.Views) != 1 || last.Views[0].PinnedAt == nil || last.Views[0].Source != "repo-releases/check" {
		t.Fatalf("the reply's view as listed = %+v", last.Views)
	}
	before := page.Panels[0].UpdatedAt
	calls := provider.requestCount()

	if err := tick(10, 0); err != nil {
		t.Fatal(err)
	}
	panel := now().Panels[0]
	if !strings.Contains(string(panel.View.Data), `"latest":"v0.9.22"`) || !panel.UpdatedAt.After(before) {
		t.Fatalf("the panel did not take the new version: %+v", panel)
	}
	if n := provider.requestCount(); n != calls {
		t.Fatalf("feeding the panel called the model %d times", n-calls)
	}

	// Data the view cannot draw fails the trigger and leaves the panel.
	release("broken")
	if err := tick(10, 30); err == nil || !strings.Contains(err.Error(), "/url") {
		t.Fatalf("tick with data the view cannot draw = %v", err)
	}
	states, err := c.store.ListTriggers(ctx)
	if err != nil || len(states) != 1 || states[0].Failures != 1 || !strings.Contains(states[0].LastError, `the pinned view "eino 最新版本"`) {
		t.Fatalf("trigger states = %+v, %v", states, err)
	}
	if panel := now().Panels[0]; !strings.Contains(string(panel.View.Data), "v0.9.22") {
		t.Fatalf("rejected data reached the panel: %s", panel.View.Data)
	}

	var unpinned core.ViewData
	client.postJSON(t, viewPath+":unpin", nil, http.StatusOK, &unpinned)
	release("v0.9.24")
	if err := tick(12, 0); err != nil {
		t.Fatal(err)
	}
	if page := now(); len(page.Panels) != 0 || unpinned.PinnedAt != nil {
		t.Fatalf("after unpinning panels = %+v, view = %+v", page.Panels, unpinned)
	}
	if states, err := c.store.ListTriggers(ctx); err != nil || states[0].Failures != 0 || !states[0].LastRunAt.Equal(clock.Now()) {
		t.Fatalf("the trigger did not run after unpinning: %+v, %v", states, err)
	}
	if view, err := c.store.ThreadView(ctx, thread.ID, shown[0].ID); err != nil || !strings.Contains(string(view.Data), "v0.9.22") {
		t.Fatalf("an unpinned view was fed: %s, %v", view.Data, err)
	}
	client.postJSON(t, "/v1/threads/"+thread.ID+"/views/view_00000000000000000000000000000000:pin", nil, http.StatusNotFound, nil)
	if n := provider.requestCount(); n != calls {
		t.Fatalf("the trigger called the model %d times", n-calls)
	}
}
