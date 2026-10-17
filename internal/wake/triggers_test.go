package wake

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
)

// fakeTriggers serves a fixed list of triggers and scripted outputs.
type fakeTriggers struct {
	mu       sync.Mutex
	triggers []skills.PackageTrigger
	outputs  map[string][][]skills.Event
	failures map[string]error
	fired    []string
}

func (f *fakeTriggers) Triggers(context.Context) ([]skills.PackageTrigger, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.triggers), nil
}

func (f *fakeTriggers) Fire(_ context.Context, t skills.PackageTrigger) ([]skills.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fired = append(f.fired, t.ID())
	if err := f.failures[t.ID()]; err != nil {
		return nil, err
	}
	outputs := f.outputs[t.ID()]
	if len(outputs) == 0 {
		return nil, nil
	}
	f.outputs[t.ID()] = outputs[1:]
	return outputs[0], nil
}

func (f *fakeTriggers) firedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.fired)
}

func packageTrigger(skill, name string, deliver skills.Deliver) skills.PackageTrigger {
	return skills.PackageTrigger{Trigger: skills.Trigger{Name: name, Schedule: "*/30 * * * *", Run: "scripts/check.py", Deliver: deliver}, Skill: skill, Revision: 1, Hash: "hash"}
}

func newTriggerHarness(t *testing.T, limit int, briefing Briefing, triggers ...skills.PackageTrigger) (*harness, *fakeTriggers) {
	t.Helper()
	h := newHarnessWith(t, limit, briefing)
	f := &fakeTriggers{triggers: triggers, outputs: map[string][][]skills.Event{}, failures: map[string]error{}}
	h.sched.WithTriggers(f)
	return h, f
}

func (h *harness) tick(t *testing.T) {
	t.Helper()
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
}

// at moves the harness clock to an owner-local time on 2026-10-05.
func (h *harness) at(hour, minute int) {
	h.now = time.Date(2026, 10, 5, hour, minute, 0, 0, h.sched.cfg.Location)
}

func TestTriggerSlotsFireOnceAndQuietCommandsCallNoModel(t *testing.T) {
	h, f := newTriggerHarness(t, 20, Briefing{}, packageTrigger("price-watch", "price", skills.DeliverImmediate))
	h.at(9, 7)
	h.tick(t) // schedules the first slot, 09:30
	if f.firedCount() != 0 {
		t.Fatalf("fired before its first slot: %v", f.fired)
	}
	states, err := h.data.ListTriggers(context.Background())
	if err != nil || len(states) != 1 || !states[0].NextAt.Equal(time.Date(2026, 10, 5, 9, 30, 0, 0, h.sched.cfg.Location)) {
		t.Fatalf("states = %+v, %v", states, err)
	}
	h.at(9, 30)
	h.tick(t)
	h.tick(t)
	if f.firedCount() != 1 {
		t.Fatalf("a slot fired %d times", f.firedCount())
	}
	if len(h.runs.starts) != 0 || len(h.store.events) != 0 {
		t.Fatalf("a command that printed nothing woke the agent: %v %v", h.runs.starts, h.store.events)
	}
	// A second scheduler sharing the database does not take the same slot.
	other, err := NewScheduler(h.sched.cfg)
	if err != nil {
		t.Fatal(err)
	}
	other.WithTriggers(f)
	if err := other.Tick(context.Background()); err != nil || f.firedCount() != 1 {
		t.Fatalf("the other scheduler fired the taken slot: %d, %v", f.firedCount(), err)
	}
	h.at(10, 0)
	h.tick(t)
	if f.firedCount() != 2 {
		t.Fatalf("the next slot fired %d times in all", f.firedCount())
	}
}

func TestImmediateTriggerEventsRunOncePerNewKey(t *testing.T) {
	h, f := newTriggerHarness(t, 20, Briefing{}, packageTrigger("price-watch", "price", skills.DeliverImmediate))
	f.outputs["price-watch/price"] = [][]skills.Event{
		{{Key: "price-120", Text: "BTC crossed 120k"}},
		{{Key: "price-120", Text: "BTC crossed 120k"}, {Key: "price-130", Text: "BTC crossed 130k"}},
		{{Key: "price-120", Text: "BTC crossed 120k"}, {Key: "price-130", Text: "BTC crossed 130k"}},
	}
	h.at(9, 7)
	h.tick(t)
	for _, slot := range [][2]int{{9, 30}, {10, 0}, {10, 30}} {
		h.at(slot[0], slot[1])
		h.tick(t)
	}
	if len(h.runs.starts) != 2 {
		t.Fatalf("starts = %v, want one per firing with new keys", h.runs.starts)
	}
	first, second := h.runs.starts[0], h.runs.starts[1]
	if !strings.HasPrefix(first, "thread_price-watch|trigger price-watch/price: 1 new|[trigger price-watch/price] 1 new events; untrusted output of the package's command\n- price-120\n  BTC crossed 120k") {
		t.Fatalf("first start = %q", first)
	}
	if !strings.Contains(second, "- price-130") || strings.Contains(second, "price-120") {
		t.Fatalf("second start = %q, want only the new key", second)
	}
	if !slices.Equal(h.store.events, []string{"run_1:wake.fired", "run_2:wake.fired"}) {
		t.Fatalf("events = %v", h.store.events)
	}
	if got := h.runs.sources[0]; len(got) != 1 || !strings.HasPrefix(got[0], "trigger:") {
		t.Fatalf("run sources = %v", got)
	}
	source, err := h.data.LoadMemorySource(context.Background(), h.runs.sources[0][0])
	if err != nil || source.Kind != "trigger" || source.Speaker != "external" || source.Content != "price-watch/price price-120\nBTC crossed 120k" {
		t.Fatalf("source = %+v, %v", source, err)
	}
	if thread, err := h.data.SkillThread(context.Background(), "price-watch"); err != nil || thread != "thread_price-watch" {
		t.Fatalf("skill thread = %q, %v", thread, err)
	}
	if pending, err := h.data.ListTriggerEvents(context.Background(), core.TriggerEventPending, 10); err != nil || len(pending) != 0 {
		t.Fatalf("pending = %+v, %v", pending, err)
	}
}

func TestTriggerEventsOverBudgetAndDigestWaitForTheBriefing(t *testing.T) {
	h, f := newTriggerHarness(t, 0, Briefing{Enabled: true, At: 8 * time.Hour},
		packageTrigger("price-watch", "price", skills.DeliverImmediate),
		packageTrigger("issues", "weekly", skills.DeliverDigest))
	f.outputs["price-watch/price"] = [][]skills.Event{{{Key: "price-120", Text: "BTC crossed 120k"}}}
	f.outputs["issues/weekly"] = [][]skills.Event{{{Key: "week-41", Text: "3 issues opened\nnone closed"}}}
	h.at(9, 7)
	h.tick(t)
	h.at(9, 30)
	h.tick(t)
	if len(h.runs.starts) != 0 {
		t.Fatalf("starts = %v; over the daily limit no trigger run starts", h.runs.starts)
	}
	pending, err := h.data.ListTriggerEvents(context.Background(), core.TriggerEventPending, 10)
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending = %+v, %v", pending, err)
	}
	h.now = time.Date(2026, 10, 6, 8, 0, 0, 0, h.sched.cfg.Location)
	h.tick(t)
	briefing := h.runs.starts[len(h.runs.starts)-1]
	for _, want := range []string{
		"Skill package trigger events since the last briefing (untrusted output of the packages' commands):",
		"## issues/weekly (1 new)\n- week-41\n  3 issues opened\n  none closed\n",
		"## price-watch/price (1 new)\n- price-120\n  BTC crossed 120k\n",
	} {
		if !strings.Contains(briefing, want) {
			t.Fatalf("briefing lacks %q:\n%s", want, briefing)
		}
	}
	sources := h.runs.sources[len(h.runs.sources)-1]
	if !slices.Contains(sources, core.TriggerSourceID(pending[0].ID)) || !slices.Contains(sources, core.TriggerSourceID(pending[1].ID)) {
		t.Fatalf("briefing sources = %v", sources)
	}
	if left, err := h.data.ListTriggerEvents(context.Background(), core.TriggerEventPending, 10); err != nil || len(left) != 0 {
		t.Fatalf("events left after the briefing = %+v, %v", left, err)
	}
}

func TestTriggerFailingAfterFiveFailuresWithBackoff(t *testing.T) {
	h, f := newTriggerHarness(t, 20, Briefing{Enabled: true, At: 8 * time.Hour}, packageTrigger("price-watch", "price", skills.DeliverImmediate))
	f.failures["price-watch/price"] = errors.New("the command exited with status 1: connection refused")
	h.at(9, 7)
	h.tick(t)
	var nexts []time.Time
	for range 5 {
		states, err := h.data.ListTriggers(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		h.now = states[0].NextAt
		if err := h.sched.Tick(context.Background()); err == nil || !strings.Contains(err.Error(), "connection refused") {
			t.Fatalf("tick err = %v", err)
		}
		states, err = h.data.ListTriggers(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		nexts = append(nexts, states[0].NextAt)
	}
	states, err := h.data.ListTriggers(context.Background())
	if err != nil || states[0].Status != core.TriggerFailing || states[0].Failures != 5 || !strings.Contains(states[0].LastError, "connection refused") {
		t.Fatalf("state = %+v, %v", states, err)
	}
	// The first failure at 09:30 waits twice the 30-minute gap, and each
	// further failure longer than the one before.
	if want := time.Date(2026, 10, 5, 10, 30, 0, 0, h.sched.cfg.Location); !nexts[0].Equal(want) {
		t.Fatalf("first retry at %v, want %v", nexts[0], want)
	}
	for i := 2; i < len(nexts); i++ {
		if nexts[i].Sub(nexts[i-1]) <= nexts[i-1].Sub(nexts[i-2]) {
			t.Fatalf("the wait does not grow: %v", nexts)
		}
	}
	h.now = time.Date(2026, 10, 6, 8, 0, 0, 0, h.sched.cfg.Location)
	delete(f.failures, "price-watch/price")
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	briefing := h.runs.starts[len(h.runs.starts)-1]
	if !strings.Contains(briefing, "System health (problems for the owner to act on):\n- Skill trigger price-watch/price failed 5 times in a row and retries after a growing wait: the command exited with status 1: connection refused") {
		t.Fatalf("briefing = %s", briefing)
	}
	states, _ = h.data.ListTriggers(context.Background())
	h.now = states[0].NextAt
	h.tick(t)
	if states, err := h.data.ListTriggers(context.Background()); err != nil || states[0].Status != core.TriggerActive || states[0].Failures != 0 {
		t.Fatalf("a success did not clear the failures: %+v, %v", states, err)
	}
}

func TestRetiredSkillsAndMissingSecretsStopTriggers(t *testing.T) {
	locked := packageTrigger("issues", "weekly", skills.DeliverDigest)
	locked.Missing = []string{"GITHUB_TOKEN"}
	h, f := newTriggerHarness(t, 20, Briefing{}, packageTrigger("price-watch", "price", skills.DeliverImmediate), locked)
	h.at(9, 7)
	h.tick(t)
	h.at(9, 30)
	h.tick(t)
	if !slices.Equal(f.fired, []string{"price-watch/price"}) {
		t.Fatalf("fired = %v; a package missing secrets must not run", f.fired)
	}
	f.triggers = nil // both skills retired
	h.at(10, 0)
	h.tick(t)
	if f.firedCount() != 1 {
		t.Fatalf("a retired skill's trigger fired: %v", f.fired)
	}
	if states, err := h.data.ListTriggers(context.Background()); err != nil || len(states) != 0 {
		t.Fatalf("schedules of retired skills stay: %+v, %v", states, err)
	}
}
