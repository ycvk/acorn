package wire

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/api"
)

// TestViewsShowUnderTheReply has the agent load show_view, get a view
// rejected with what to fix, show the fixed one and reply. The thread lists
// the view under the reply, the run's events announce it, and deleting the
// thread deletes it.
func TestViewsShowUnderTheReply(t *testing.T) {
	broken := map[string]any{"title": "eino 最近的版本", "components": []any{
		map[string]any{"id": "root", "component": "Table", "columns": []any{"版本"}, "rows": map[string]any{"path": "/missing"}},
	}}
	fixed := map[string]any{"title": "eino 最近的版本", "components": []any{
		map[string]any{"id": "root", "component": "Column", "children": []any{"table"}},
		map[string]any{"id": "table", "component": "Table", "columns": []any{"版本", "要点"}, "rows": map[string]any{"path": "/releases"}},
	}, "data": map[string]any{"releases": []any{[]any{"v0.9.21", "summarization 可以只清理不压缩"}}}}
	provider := &fakeOpenAI{replies: []string{
		toolCallChunk("load_views", "tool_search", map[string]string{"query": "select:show_view"}),
		toolCallChunk("broken_view", "show_view", broken),
		toolCallChunk("fixed_view", "show_view", fixed),
		textReply("整理成了一张表"),
	}}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, "")
	c, err := NewContainer(context.Background(), cfg)
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	defer c.Close()
	client := newPairedClient(t, c)
	ctx := context.Background()
	thread, err := c.threads.CreateThread(ctx, "eino")
	if err != nil {
		t.Fatal(err)
	}

	var run api.RunDTO
	client.postJSON(t, "/v1/threads/"+thread.ID+"/runs", map[string]any{"input": "比较一下 eino 最近几个版本"}, http.StatusCreated, &run)
	waitRunStatus(t, c, run.ID, "completed")

	if rejected := toolResult(t, provider.request(2), "broken_view"); !strings.Contains(rejected, "/missing") || !strings.Contains(rejected, "finds nothing") {
		t.Fatalf("the model saw %q for the broken view", rejected)
	}

	var listed api.MessageListResponse
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/threads/"+thread.ID+"/messages", nil), http.StatusOK, &listed)
	if len(listed.Items) != 2 {
		t.Fatalf("messages = %+v", listed.Items)
	}
	reply := listed.Items[1]
	if reply.Content.Text != "整理成了一张表" || len(reply.Views) != 1 {
		t.Fatalf("reply = %+v", reply)
	}
	view := reply.Views[0]
	if view.Title != "eino 最近的版本" || view.Catalog != "acorn.views.v1" || !strings.Contains(string(view.Data), "v0.9.21") || !strings.Contains(string(view.Components), `"component":"Table"`) {
		t.Fatalf("view = %+v (components %s, data %s)", view, view.Components, view.Data)
	}

	var detail api.RunDetailDTO
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/runs/"+run.ID+"/detail", nil), http.StatusOK, &detail)
	var announced []string
	for _, event := range detail.Events {
		if event.Type != "view.shown" {
			continue
		}
		raw, err := json.Marshal(event.Data)
		if err != nil {
			t.Fatal(err)
		}
		var data struct {
			View struct {
				ID string `json:"id"`
			} `json:"view"`
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			t.Fatal(err)
		}
		announced = append(announced, data.View.ID)
	}
	if len(announced) != 1 || announced[0] != view.ID {
		t.Fatalf("view.shown events announce %v, want [%s]", announced, view.ID)
	}

	client.do(t, httptest.NewRequest(http.MethodDelete, "/v1/threads/"+thread.ID, nil), http.StatusNoContent, nil)
	if views, err := c.store.ThreadViews(ctx, thread.ID); err != nil || len(views) != 0 {
		t.Fatalf("views after the thread was deleted = %+v, %v", views, err)
	}
}
