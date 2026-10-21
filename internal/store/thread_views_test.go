package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

func TestThreadViewsAreListedOldestFirstAndLeaveWithTheirThread(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range []string{"eino", "other"} {
		if _, err := s.CreateSession(ctx, id, id); err != nil {
			t.Fatal(err)
		}
	}
	write := func(session, title string) core.ThreadView {
		t.Helper()
		view, err := s.SaveThreadView(ctx, core.ThreadViewWrite{
			SessionID:  session,
			RunID:      "run_1",
			Title:      title,
			Catalog:    "acorn.views.v1",
			Components: json.RawMessage(`[{"component":"Text","id":"root","text":{"path":"/v"}}]`),
			Data:       json.RawMessage(`{"v":"五个版本"}`),
		})
		if err != nil {
			t.Fatal(err)
		}
		return view
	}
	first := write("eino", "first")
	second := write("eino", "second")
	elsewhere := write("other", "elsewhere")

	views, err := s.ThreadViews(ctx, "eino")
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 || views[0].ID != first.ID || views[1].ID != second.ID {
		t.Fatalf("views = %+v", views)
	}
	if string(views[0].Data) != `{"v":"五个版本"}` || views[0].RunID != "run_1" || views[0].Catalog != "acorn.views.v1" || !views[0].CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("stored view = %+v", views[0])
	}

	if _, err := s.SaveThreadView(ctx, core.ThreadViewWrite{SessionID: "missing", RunID: "run_1", Title: "t", Catalog: "c", Components: json.RawMessage(`[]`), Data: json.RawMessage(`{}`)}); !errors.Is(err, core.ErrSessionNotFound) {
		t.Fatalf("missing thread err = %v", err)
	}
	if _, err := s.SaveThreadView(ctx, core.ThreadViewWrite{SessionID: "eino", Title: "t", Catalog: "c", Components: json.RawMessage(`[]`), Data: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("a view without its run was saved")
	}

	// The owner presses a button once; the action comes with their message.
	press := core.ViewActionWrite{ViewID: first.ID, SessionID: "eino", Key: "save/1", ComponentID: "save", Scope: []int{1}, Name: "save_note",
		Text: "整理成笔记", Selected: []core.ViewOption{{ID: "a", Label: "写摘要"}}, Context: json.RawMessage(`{"url":"https://example.com"}`)}
	message, action, err := s.SendViewAction(ctx, press)
	if err != nil {
		t.Fatal(err)
	}
	if message.Content != "整理成笔记" || message.Role != "user" || message.ID == 0 || action.MessageID != message.ID || action.Key != "save/1" {
		t.Fatalf("message = %+v action = %+v", message, action)
	}
	if _, _, err := s.SendViewAction(ctx, press); !errors.Is(err, core.ErrViewActionTaken) {
		t.Fatalf("second press err = %v, want ErrViewActionTaken", err)
	}
	if listed, err := s.ListSessionMessages(ctx, "eino", 50); err != nil || len(listed) != 1 {
		t.Fatalf("messages after two presses = %+v, %v", listed, err)
	}
	if _, _, err := s.SendViewAction(ctx, core.ViewActionWrite{ViewID: elsewhere.ID, SessionID: "eino", Key: "x", ComponentID: "x", Name: "x", Text: "x"}); !errors.Is(err, core.ErrThreadViewNotFound) {
		t.Fatalf("a view of another thread err = %v", err)
	}

	// Replacing the view's content keeps what the owner already sent.
	updated, err := s.UpdateThreadView(ctx, core.ThreadViewUpdate{SessionID: "eino", ViewID: first.ID, Title: "renamed", Components: first.Components, Data: json.RawMessage(`{"v":"六个版本"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "renamed" || string(updated.Data) != `{"v":"六个版本"}` || len(updated.Actions) != 1 || updated.Actions[0].Selected[0].Label != "写摘要" || string(updated.Actions[0].Context) != `{"url":"https://example.com"}` {
		t.Fatalf("updated = %+v", updated)
	}
	if _, err := s.UpdateThreadView(ctx, core.ThreadViewUpdate{SessionID: "other", ViewID: first.ID, Title: "t", Components: first.Components, Data: first.Data}); !errors.Is(err, core.ErrThreadViewNotFound) {
		t.Fatalf("update from another thread err = %v", err)
	}
	if views, err := s.ThreadViews(ctx, "eino"); err != nil || len(views[0].Actions) != 1 || len(views[1].Actions) != 0 {
		t.Fatalf("views with actions = %+v, %v", views, err)
	}

	if err := s.DeleteThread(ctx, "eino"); err != nil {
		t.Fatal(err)
	}
	if views, err := s.ThreadViews(ctx, "eino"); err != nil || len(views) != 0 {
		t.Fatalf("views of the deleted thread = %+v, %v", views, err)
	}
	if actions, err := s.ViewActions(ctx, "eino"); err != nil || len(actions) != 0 {
		t.Fatalf("actions of the deleted thread = %+v, %v", actions, err)
	}
	if views, err := s.ThreadViews(ctx, "other"); err != nil || len(views) != 1 || views[0].ID != elsewhere.ID {
		t.Fatalf("views of the other thread = %+v, %v", views, err)
	}
}
