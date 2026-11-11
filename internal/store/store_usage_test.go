package store

import (
	"context"
	"testing"
	"time"
)

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
