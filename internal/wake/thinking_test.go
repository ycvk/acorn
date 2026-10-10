package wake

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func oldThought(t *testing.T, h *harness) {
	t.Helper()
	ctx := context.Background()
	source, err := h.data.RegisterMemorySource(ctx, core.MemorySource{ID: "thought-origin", Kind: "fixture", ObjectID: "thought-origin", Version: "1", Speaker: "owner", Body: "review my idea", RecordedAt: h.now.Add(-72 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.data.CommitMemory(ctx, core.MemoryMutation{Now: h.now, Changes: []core.MemoryChange{{Draft: core.MemoryDraft{Kind: "thought", Content: "review my idea", Evidence: []core.MemoryEvidence{{SourceID: source.ID, Quote: source.Content, Relation: "supports"}}}, Reason: "open question"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = h.data.SaveConcern(ctx, core.MemoryConcern{Title: "review my idea", State: "active", Reason: "owner asked", SourceID: source.ID, ReviewAt: h.now, UpdatedAt: h.now}, 0); err != nil {
		t.Fatal(err)
	}

}

func TestNightAndWanderShareThreadAndClaimSlots(t *testing.T) {
	h := newHarness(t, 20)
	h.sched.cfg.Thinking = Thinking{Night: Briefing{Enabled: true, At: 3 * time.Hour}, Wander: []time.Duration{10 * time.Hour, 15 * time.Hour}}
	oldThought(t, h)
	for range 2 {
		if err := h.sched.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(h.runs.starts) != 1 || !strings.Contains(h.runs.starts[0], "[night 2026-10-05]") || !strings.Contains(h.runs.starts[0], "thought open") {
		t.Fatalf("night=%v", h.runs.starts)
	}
	h.now = h.now.Add(7 * time.Hour)
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.runs.starts) != 3 || !strings.Contains(h.runs.starts[1], "[wander 2026-10-05 10:00]") || !strings.Contains(h.runs.starts[2], "[wander 2026-10-05 15:00]") {
		t.Fatalf("wander=%v", h.runs.starts)
	}
	for _, start := range h.runs.starts {
		if !strings.HasPrefix(start, "thread_thoughts|") {
			t.Fatalf("thread=%s", start)
		}
	}
	if len(h.store.wakes) != 3 {
		t.Fatalf("wakes=%v", h.store.wakes)
	}
}

func TestEmptyNightKeepsItsSlot(t *testing.T) {
	h := newHarness(t, 20)
	h.sched.cfg.Thinking.Night = Briefing{Enabled: true, At: 0}
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	oldThought(t, h)
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.runs.starts) != 0 {
		t.Fatalf("empty slot restarted: %v", h.runs.starts)
	}
}

func TestTokenBudgetDefersCommitmentSkipsNightAndAllowsBriefing(t *testing.T) {
	h := newHarnessWith(t, 20, Briefing{Enabled: true, At: 0})
	h.sched.cfg.Thinking.Night = Briefing{Enabled: true, At: 0}
	h.sched.cfg.DailyTokens = 100
	h.store.tokens = 150
	h.store.usageAt = h.now
	h.commit(t, "tomorrow if out of budget", h.now, "")
	oldThought(t, h)
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.runs.starts) != 1 || !strings.Contains(h.runs.starts[0], "[briefing") {
		t.Fatalf("over budget starts=%v", h.runs.starts)
	}
	item, err := h.data.LoadCommitment(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != "scheduled" {
		t.Fatal("budget consumed the commitment")
	}
	h.store.tokens = 0
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.runs.starts) != 2 || !strings.Contains(h.runs.starts[1], "[commitment") {
		t.Fatalf("skipped night retried: %v", h.runs.starts)
	}
	h.now = h.now.Add(24 * time.Hour)
	h.store.tokens = 150 // yesterday's calls do not consume today's budget
	h.commit(t, "next day", h.now, "")
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(h.runs.starts, "\n"), "next day") {
		t.Fatalf("next day not woken: %v", h.runs.starts)
	}
}

func TestRoutinePreparationFailureReleasesClaim(t *testing.T) {
	h := newHarness(t, 20)
	prepare := func() (routineInput, error) { return routineInput{}, errors.New("query failed") }
	if err := h.sched.runRoutine(context.Background(), "night", "2026-10-05", "Thoughts", true, h.now, prepare); err == nil {
		t.Fatal("preparation error hidden")
	}
	if err := h.data.ClaimRoutine(context.Background(), "night", "2026-10-05", h.now); err != nil {
		t.Fatalf("claim retained: %v", err)
	}
}

func TestRoutineConcurrentTicksStartOnce(t *testing.T) {
	h := newHarness(t, 20)
	h.sched.cfg.Thinking.Night = Briefing{Enabled: true, At: 0}
	oldThought(t, h)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { errs <- h.sched.Tick(context.Background()) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(h.runs.starts) != 1 {
		t.Fatalf("duplicate runs: %v", h.runs.starts)
	}
}

func TestBriefingPhoneWindowAndRetention(t *testing.T) {
	h := newHarnessWith(t, 20, Briefing{Enabled: true, At: 0})
	ctx := context.Background()
	previous := h.now.Add(-24 * time.Hour)
	if err := h.data.ClaimRoutine(ctx, "briefing", "2026-10-04", previous); err != nil {
		t.Fatal(err)
	}
	if err := h.data.SetRoutineRun(ctx, "briefing", "2026-10-04", "thread_briefings", "old"); err != nil {
		t.Fatal(err)
	}
	for _, item := range []core.PhoneNotification{
		{Key: "before", ReceivedAt: previous.Add(-time.Nanosecond)},
		{Key: "start", ReceivedAt: previous},
		{Key: "end", ReceivedAt: h.now},
		{Key: "expired", ReceivedAt: h.now.Add(-8 * 24 * time.Hour)},
	} {
		item.DeviceID = "device"
		item.Package = "bank"
		item.App = "Bank"
		item.Title = item.Key
		item.PostedAt = item.ReceivedAt
		if _, err := h.data.AddPhoneNotifications(ctx, []core.PhoneNotification{item}); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.sched.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	input := h.runs.starts[0]
	if !strings.Contains(input, `"start"`) || strings.Contains(input, `"end"`) || strings.Contains(input, `"before"`) {
		t.Fatalf("window: %s", input)
	}
	page, err := h.data.ListPhoneNotifications(ctx, time.Time{}, h.now.Add(time.Hour), 100)
	if err != nil || page.Total != 3 {
		t.Fatalf("retention: %+v %v", page, err)
	}
}

func TestZeroTokenBudgetAllowsAutonomousWake(t *testing.T) {
	h := newHarnessWith(t, 20, Briefing{})
	h.sched.cfg.DailyTokens = 0
	h.store.tokens = 10000000
	h.store.usageAt = h.now
	h.commit(t, "wake without token cap", h.now, "")
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.runs.starts) != 1 {
		t.Fatalf("starts = %v", h.runs.starts)
	}
}
