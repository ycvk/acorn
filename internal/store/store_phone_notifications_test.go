package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestPhoneNotificationWindowsAndDeduplication(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	start := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	var items []core.PhoneNotification
	for i := range 4 {
		at := start.Add(time.Duration(i) * time.Hour)
		items = append(items, core.PhoneNotification{DeviceID: "phone", Key: "key", Package: "bank", App: "Bank", Text: "transfer", PostedAt: at, ReceivedAt: at})
	}
	before := append([]core.PhoneNotification(nil), items...)
	if n, err := s.AddPhoneNotifications(ctx, items); err != nil || n != 4 {
		t.Fatalf("insert %d, %v", n, err)
	}
	if !reflect.DeepEqual(items, before) {
		t.Fatal("store mutated caller input")
	}
	if n, err := s.AddPhoneNotifications(ctx, items); err != nil || n != 0 {
		t.Fatalf("duplicate %d, %v", n, err)
	}
	page, err := s.ListPhoneNotifications(ctx, start, start.Add(3*time.Hour), 2)
	if err != nil || page.Total != 3 || len(page.Items) != 2 || !page.Items[0].ReceivedAt.Equal(start.Add(2*time.Hour)) {
		t.Fatalf("page %+v, %v", page, err)
	}
	next, err := s.ListPhoneNotifications(ctx, start.Add(3*time.Hour), start.Add(4*time.Hour), 2)
	if err != nil || next.Total != 1 || !next.Items[0].ReceivedAt.Equal(start.Add(3*time.Hour)) {
		t.Fatalf("next window %+v, %v", next, err)
	}
	if n, err := s.PrunePhoneNotifications(ctx, start.Add(time.Hour)); err != nil || n != 1 {
		t.Fatalf("prune %d, %v", n, err)
	}
	counts, err := s.PhoneNotificationCounts(ctx, start)
	if err != nil || len(counts) != 1 || counts[0].Count != 3 {
		t.Fatalf("counts %+v, %v", counts, err)
	}
}

func TestUsageCountsCallsOnTheirDayAndClassifiesWholeRun(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	rows := []struct {
		run, kind, payload string
		at                 time.Time
	}{
		{"auto", "wake.fired", `{}`, at.Add(-time.Hour)},
		{"auto", "model.usage", `{"reported":true,"total_tokens":50}`, at.Add(-time.Minute)},
		{"auto", "model.usage", `{"reported":true,"total_tokens":120}`, at},
		{"auto", "model.usage", `{"reported":false}`, at},
		{"owner", "model.usage", `{"reported":true,"total_tokens":200}`, at},
		{"brief", "briefing.fired", `{}`, at},
		{"brief", "model.usage", `{"reported":true,"total_tokens":300}`, at},
	}
	for _, row := range rows {
		if _, err := s.db.Exec(`INSERT INTO events(run_id,kind,payload_json,created_at) VALUES(?,?,?,?)`, row.run, row.kind, row.payload, formatTimestamp(row.at)); err != nil {
			t.Fatal(err)
		}
	}
	report, err := s.UsageReport(ctx, at)
	if err != nil || report.AutonomousTokens != 120 || report.TotalTokens != 620 || report.UnreportedCalls != 1 {
		t.Fatalf("usage %+v, %v", report, err)
	}
	if total, err := s.SumAutonomousTokensSince(ctx, at); err != nil || total != 120 {
		t.Fatalf("budget %d, %v", total, err)
	}
}
