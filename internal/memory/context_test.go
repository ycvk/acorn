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
	got, err := e.History(ctx, "history", 0, last, 8192)
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
	visible, err := e.History(ctx, "history", 0, last, 8192)
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
	got, err := e.History(ctx, "long-history", 0, latest.ID, 3000)
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
	got, err = e.History(ctx, "large-current", 0, msg.ID, 12000)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != current {
		t.Fatal("current input was clipped")
	}
}

func TestThreadSummaryBudgetScalesWithHistoryBudget(t *testing.T) {
	for budget, want := range map[int]int{2048: 512, 8191: 1024, 8192: 1024, 32768: 4096, 65536: 8192, 131072: 8192} {
		if got := threadSummaryBudget(budget); got != want {
			t.Errorf("threadSummaryBudget(%d)=%d want %d", budget, got, want)
		}
	}
}

func TestHistorySummaryUsesScaledBudgetAndRejectsOverflow(t *testing.T) {
	ctx := context.Background()
	older := strings.Repeat("更早的讨论", 12000)
	for _, tc := range []struct {
		name    string
		summary string
		wantErr string
	}{
		{name: "within budget", summary: strings.Repeat("摘要", 2000)},
		{name: "over budget", summary: strings.Repeat("摘要", 4100), wantErr: "thread summary exceeds 4096-token budget"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &protocolModel{summary: tc.summary}
			e, db := testEngine(t, model, 0)
			if _, err := db.CreateSession(ctx, "scaled", "history"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.AppendSessionMessage(ctx, "scaled", 1, "user", older, ""); err != nil {
				t.Fatal(err)
			}
			latest, err := db.AppendSessionMessage(ctx, "scaled", 2, "user", "接着说", "")
			if err != nil {
				t.Fatal(err)
			}
			got, err := e.History(ctx, "scaled", 0, latest.ID, 32768)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err=%v want %q", err, tc.wantErr)
				}
			} else if err != nil || got.Summary == nil || got.Summary.Content != tc.summary {
				t.Fatalf("history=%+v err=%v", got, err)
			}
			if len(model.maxOutputs) == 0 {
				t.Fatal("summary was not generated")
			}
			for _, n := range model.maxOutputs {
				if n != 4096-64 {
					t.Fatalf("max output %d, want %d", n, 4096-64)
				}
			}
		})
	}
}

func TestHistoryAfterLeavesOutEarlierMessagesAndTheSummary(t *testing.T) {
	e, db := testEngine(t, &protocolModel{}, 0)
	ctx := context.Background()
	if _, err := db.CreateSession(ctx, "standalone", "briefings"); err != nil {
		t.Fatal(err)
	}
	var last int64
	for i := range 3 {
		m, err := db.AppendSessionMessage(ctx, "standalone", i+1, "wake", fmt.Sprintf("[briefing 2026-10-0%d] morning briefing", i+1), "")
		if err != nil {
			t.Fatal(err)
		}
		last = m.ID
	}
	got, err := e.History(ctx, "standalone", last-1, last, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 1 || got.Messages[0].ID != last || got.Summary != nil || len(got.SourceIDs) != 1 {
		t.Fatalf("standalone history=%+v", got)
	}
}
