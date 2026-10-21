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

	if err := s.DeleteThread(ctx, "eino"); err != nil {
		t.Fatal(err)
	}
	if views, err := s.ThreadViews(ctx, "eino"); err != nil || len(views) != 0 {
		t.Fatalf("views of the deleted thread = %+v, %v", views, err)
	}
	if views, err := s.ThreadViews(ctx, "other"); err != nil || len(views) != 1 || views[0].ID != elsewhere.ID {
		t.Fatalf("views of the other thread = %+v, %v", views, err)
	}
}
