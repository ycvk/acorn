package wire

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/core"
)

// TestViewActionsComeBackAsTheOwnersMessage has the agent offer a button and
// a choice in a view. The owner presses the button: their message says its
// label, a run starts on it, and the model sees which view and action sent
// it; that run updates the view. The same button cannot be sent twice, and
// actions the view does not offer are refused.
func TestViewActionsComeBackAsTheOwnersMessage(t *testing.T) {
	next := map[string]any{"title": "读完之后", "components": []any{
		map[string]any{"id": "root", "component": "Column", "children": []any{"note", "pick"}},
		map[string]any{"id": "note", "component": "Button", "label": "整理成笔记", "action": map[string]any{"name": "save_note", "context": map[string]any{"url": map[string]any{"path": "/article"}}}},
		map[string]any{"id": "pick", "component": "ChoicePicker", "options": map[string]any{"path": "/next"}, "submit": "就这个", "action": map[string]any{"name": "next_step"}},
	}, "data": map[string]any{
		"article": "https://example.com/long-read",
		"next":    []any{map[string]any{"id": "a", "label": "写摘要"}, map[string]any{"id": "b", "label": "设个提醒"}},
	}}
	provider := &fakeOpenAI{replies: []string{
		toolCallChunk("load_views", "tool_search", map[string]string{"query": "select:show_view,update_view"}),
		toolCallChunk("offer", "show_view", next),
		textReply("读完了，接下来想怎么处理？"),
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
	thread, err := c.threads.CreateThread(ctx, "长文")
	if err != nil {
		t.Fatal(err)
	}

	var first api.RunDTO
	client.postJSON(t, "/v1/threads/"+thread.ID+"/runs", map[string]any{"input": "读一下这篇长文"}, http.StatusCreated, &first)
	waitRunStatus(t, c, first.ID, "completed")
	views, err := c.store.ThreadViews(ctx, thread.ID)
	if err != nil || len(views) != 1 {
		t.Fatalf("views = %+v, %v", views, err)
	}
	viewID := views[0].ID
	// The run the button starts updates the view the first run showed.
	provider.add(
		toolCallChunk("mark", "update_view", map[string]any{"view_id": viewID, "title": "读完之后（已整理）"}),
		textReply("整理好了"),
	)
	actions := "/v1/threads/" + thread.ID + "/views/" + viewID + "/actions"

	var sent api.ViewActionResponse
	client.postJSON(t, actions, map[string]any{"component_id": "note"}, http.StatusCreated, &sent)
	if sent.Text != "整理成笔记" || sent.Run.ID == "" || len(sent.View.Actions) != 1 || sent.View.Actions[0].ComponentID != "note" {
		t.Fatalf("sent = %+v", sent)
	}
	waitRunStatus(t, c, sent.Run.ID, "completed")

	// The model sees the owner's words and, beside them, which view and action sent them.
	input := lastOwnerInput(provider.request(3))
	for _, want := range []string{"整理成笔记", `[Sent from your view "读完之后" (` + viewID + `) through note: action save_note]`, `Context: {"url":"https://example.com/long-read"}`} {
		if !strings.Contains(input, want) {
			t.Fatalf("model input %q\nwant it to contain %q", input, want)
		}
	}

	client.postJSON(t, actions, map[string]any{"component_id": "note"}, http.StatusConflict, nil)
	client.postJSON(t, actions, map[string]any{"component_id": "delete_everything"}, http.StatusBadRequest, nil)
	client.postJSON(t, actions, map[string]any{"component_id": "pick", "selected": []string{"z"}}, http.StatusBadRequest, nil)
	client.postJSON(t, actions, map[string]any{"component_id": "pick", "selected": []string{"a", "b"}}, http.StatusBadRequest, nil)
	client.postJSON(t, "/v1/threads/"+thread.ID+"/views/view_00000000000000000000000000000000/actions", map[string]any{"component_id": "note"}, http.StatusNotFound, nil)

	var listed api.MessageListResponse
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/threads/"+thread.ID+"/messages", nil), http.StatusOK, &listed)
	roles := make([]string, len(listed.Items))
	for i, item := range listed.Items {
		roles[i] = item.Role + ":" + item.Content.Text
	}
	if !slices.Equal(roles, []string{"user:读一下这篇长文", "assistant:读完了，接下来想怎么处理？", "user:整理成笔记", "assistant:整理好了"}) {
		t.Fatalf("messages = %v", roles)
	}
	shown := listed.Items[1].Views
	if len(shown) != 1 || shown[0].Title != "读完之后（已整理）" || len(shown[0].Actions) != 1 || shown[0].Actions[0].ComponentID != "note" {
		t.Fatalf("the view as listed = %+v", shown)
	}

	var detail api.RunDetailDTO
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/runs/"+sent.Run.ID+"/detail", nil), http.StatusOK, &detail)
	if !slices.ContainsFunc(detail.Events, func(e core.RunEvent) bool { return e.Type == "view.updated" }) {
		t.Fatalf("the action's run has no view.updated event: %+v", detail.Events)
	}
}
