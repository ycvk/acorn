package wake

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestBriefingHealthIsSilentWhenAllIsWell(t *testing.T) {
	h := newHarness(t, 0)
	h.sched.cfg.Backups = func() (time.Time, error) { return h.now.Add(-20 * time.Hour), nil }
	got, err := h.sched.briefingHealth(context.Background(), h.now)
	if err != nil || got != "" {
		t.Fatalf("health = %q, %v; want nothing", got, err)
	}
}

func TestBriefingHealthReportsBackups(t *testing.T) {
	for name, tc := range map[string]struct {
		latest time.Duration
		err    error
		want   string
	}{
		"stale":   {latest: 50 * time.Hour, want: "50 hours ago"},
		"missing": {want: "no snapshot has been taken yet"},
		"broken":  {err: errors.New("permission denied"), want: "cannot be read: permission denied"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, 0)
			h.sched.cfg.Backups = func() (time.Time, error) {
				if tc.latest == 0 {
					return time.Time{}, tc.err
				}
				return h.now.Add(-tc.latest), nil
			}
			got, err := h.sched.briefingHealth(context.Background(), h.now)
			if err != nil || !strings.Contains(got, "System health") || !strings.Contains(got, tc.want) {
				t.Fatalf("health = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestBriefingHealthReportsStuckAndFailedMemoryJobs(t *testing.T) {
	h := newHarness(t, 0)
	ctx := context.Background()
	old := h.now.Add(-30 * time.Hour)
	for _, id := range []string{"owner:a", "owner:b"} {
		if _, err := h.data.RegisterMemorySource(ctx, core.MemorySource{ID: id, Kind: "import", ObjectID: id, Version: "1", Speaker: "owner", Body: "我喜欢清淡", RecordedAt: old}); err != nil {
			t.Fatal(err)
		}
	}
	later := h.now
	for attempt := 0; attempt < 3; attempt++ {
		job, err := h.data.ClaimMemoryJob(ctx, later, time.Minute, "extract")
		if err != nil || job == nil {
			t.Fatalf("claim attempt %d: %+v %v", attempt, job, err)
		}
		if err := h.data.FailMemoryJob(ctx, *job, "model unavailable", later); err != nil {
			t.Fatal(err)
		}
		later = later.Add(time.Hour)
	}
	got, err := h.sched.briefingHealth(ctx, h.now)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1 jobs failed after all retries; last error: model unavailable", "1 jobs are waiting, the oldest since 2026-10-04 03:00"} {
		if !strings.Contains(got, want) {
			t.Fatalf("health = %q, want %q", got, want)
		}
	}
}

func TestBriefingInputCarriesHealthProblems(t *testing.T) {
	h := newHarness(t, 0)
	h.sched.cfg.Backups = func() (time.Time, error) { return time.Time{}, nil }
	in, err := h.sched.prepareBriefing(context.Background(), "2026-10-05", h.now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(in.input, "[briefing 2026-10-05]") || !strings.Contains(in.input, "System health") || !strings.Contains(in.input, "no snapshot has been taken yet") {
		t.Fatalf("briefing input = %q", in.input)
	}
}
