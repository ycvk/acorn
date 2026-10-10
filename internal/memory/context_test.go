package memory

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

func TestHistoryUsesBudgetAndStableSourceIDs(t *testing.T) {
	e, db := testEngine(t, &protocolModel{}, 0)
	ctx := context.Background()
	if _, err := db.CreateSession(ctx, "history", "history"); err != nil {
		t.Fatal(err)
	}
	var last int64
	for i := range 20 {
		m, err := db.AppendSessionMessage(ctx, "history", i+1, "user", fmt.Sprintf("第%d条：请保留完整上下文。", i), "")
		if err != nil {
			t.Fatal(err)
		}
		last = m.ID
	}
	got, err := e.History(ctx, "history", last, 8192)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 20 || len(got.SourceIDs) != 20 {
		t.Fatalf("history=%+v", got)
	}
	request, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: "forget-history", Kind: "request", ObjectID: "forget-history", Version: "1", Speaker: "owner", Body: "忘记第0条", RecordedAt: e.cfg.Clock()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ForgetMemory(ctx, core.MemoryForget{SourceIDs: got.SourceIDs[:1], RequestSourceID: request.ID, Reason: "owner request", Now: e.cfg.Clock()}); err != nil {
		t.Fatal(err)
	}
	visible, err := e.History(ctx, "history", last, 8192)
	if err != nil {
		t.Fatal(err)
	}
	if len(visible.Messages) != 19 || strings.Contains(fmt.Sprint(visible.Messages), "第0条") {
		t.Fatalf("excluded history=%+v", visible)
	}
}

func TestHistoryChunksLongOlderMessageAndKeepsCurrentInput(t *testing.T) {
	model := &protocolModel{}
	e, db := testEngine(t, model, 0)
	ctx := context.Background()
	if _, err := db.CreateSession(ctx, "long-history", "history"); err != nil {
		t.Fatal(err)
	}
	long := "START_MARKER" + strings.Repeat("长期历史材料", 5000) + "END_MARKER"
	if _, err := db.AppendSessionMessage(ctx, "long-history", 1, "user", long, ""); err != nil {
		t.Fatal(err)
	}
	latest, err := db.AppendSessionMessage(ctx, "long-history", 2, "user", "继续上面的事情", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.History(ctx, "long-history", latest.ID, 3000)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary == nil || got.Summary.ThroughMessageID != latest.ID-1 || len(got.Messages) != 1 {
		t.Fatalf("history %+v", got)
	}
	if model.calls < 2 || !strings.Contains(strings.Join(model.seen, "\n"), "START_MARKER") || !strings.Contains(strings.Join(model.seen, "\n"), "END_MARKER") {
		t.Fatal("long source was not fully processed")
	}
	current := strings.Repeat("当前输入", 3000)
	if _, err := db.CreateSession(ctx, "large-current", "current"); err != nil {
		t.Fatal(err)
	}
	msg, err := db.AppendSessionMessage(ctx, "large-current", 1, "user", current, "")
	if err != nil {
		t.Fatal(err)
	}
	got, err = e.History(ctx, "large-current", msg.ID, 12000)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != current {
		t.Fatal("current input was clipped")
	}
}
