package wake

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/store"
)

// memStore is an in-memory PresenceStore and EventAppender. Wakes are counted
// from appended wake.fired events, like the SQLite store does.
type memStore struct {
	mu      sync.Mutex
	items   []core.MemoryItem
	wakes   []time.Time
	clock   func() time.Time
	events  []string
	tokens  int
	usageAt time.Time
}

func (s *memStore) AddMemoryItem(_ context.Context, item core.MemoryItem) (core.MemoryItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item.ID = int64(len(s.items) + 1)
	s.items = append(s.items, item)
	return item, nil
}

func (s *memStore) ListMemoryItems(_ context.Context, statuses []core.MemoryStatus) ([]core.MemoryItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.MemoryItem
	for _, item := range s.items {
		for _, status := range statuses {
			if item.Status == status {
				out = append(out, item)
			}
		}
	}
	return out, nil
}

func (s *memStore) LoadMemoryItem(_ context.Context, id int64) (*core.MemoryItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.items[id-1]
	return &item, nil
}

func (s *memStore) UpdateMemoryItem(_ context.Context, item core.MemoryItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[item.ID-1] = item
	return nil
}

func (s *memStore) ListDueCommitments(_ context.Context, now time.Time) ([]core.MemoryItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.MemoryItem
	for _, item := range s.items {
		if item.Kind == core.MemoryCommitment && item.Status == core.MemoryActive && !item.WakeAt.After(now) {
			out = append(out, item)
		}
	}
	return out, nil
}

func (s *memStore) ClaimDueCommitment(_ context.Context, id int64, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := &s.items[id-1]
	if item.Status != core.MemoryActive || item.WakeAt.After(now) {
		return fmt.Errorf("%w: %d", core.ErrMemoryItemNotDue, id)
	}
	item.Status = core.MemoryWoken
	return nil
}

func (s *memStore) SearchExperience(context.Context, string, int) ([]core.ExperienceHit, error) {
	return nil, nil
}

func (s *memStore) SaveContextSnapshot(context.Context, string, string) error { return nil }

func (s *memStore) CountWakesSince(_ context.Context, since time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, at := range s.wakes {
		if !at.Before(since) {
			n++
		}
	}
	return n, nil
}

func (s *memStore) AppendEvent(_ context.Context, runID, kind string, _ any) (core.EventRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, runID+":"+kind)
	if kind == core.EventWakeFired {
		s.wakes = append(s.wakes, s.clock())
	}
	return core.EventRecord{}, nil
}

type fakeRuns struct {
	mu     sync.Mutex
	starts []string // threadID|wake|input
	fail   error
}

func (r *fakeRuns) StartRoutineRun(_ context.Context, threadID, title, wake, input string) (string, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return "", "", r.fail
	}
	if threadID == "" {
		threadID = "thread_" + strings.ToLower(title)
	}
	r.starts = append(r.starts, threadID+"|"+wake+"|"+input)
	return threadID, fmt.Sprintf("run_%d", len(r.starts)), nil
}

func (r *fakeRuns) StartWakeRun(_ context.Context, threadID, wake, input string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return "", r.fail
	}
	r.starts = append(r.starts, threadID+"|"+wake+"|"+input)
	return fmt.Sprintf("run_%d", len(r.starts)), nil
}

type harness struct {
	store   *memStore
	runs    *fakeRuns
	watches *memWatches
	checker *fakeChecker
	sched   *Scheduler
	now     time.Time
	data    *store.Store
}

func newHarness(t *testing.T, limit int) *harness {
	t.Helper()
	return newHarnessWith(t, limit, Briefing{})
}

func newHarnessWith(t *testing.T, limit int, briefing Briefing) *harness {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{runs: &fakeRuns{}, watches: newMemWatches(), now: time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC)}
	h.data, err = store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := h.data.Close(); err != nil {
			t.Error(err)
		}
	})
	h.store = &memStore{clock: func() time.Time { return h.now }}
	h.checker = &fakeChecker{watches: h.watches, found: map[int64][]core.WatchItem{}, clock: func() time.Time { return h.now }}
	h.sched, err = NewScheduler(Config{
		Store: h.store, Events: h.store, Runs: h.runs, Watches: h.watches, Checker: h.checker,
		Clock: func() time.Time { return h.now }, Location: loc, DailyLimit: limit,
		MaxChecksPerTick: 5, Briefing: briefing, Interval: time.Second,
		DailyTokens: 300000, Routines: h.data, PhoneNotifications: h.data,
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *harness) commit(t *testing.T, content string, wakeAt time.Time, recurrence string) core.MemoryItem {
	t.Helper()
	item, err := h.store.AddMemoryItem(context.Background(), core.MemoryItem{
		Kind: core.MemoryCommitment, Status: core.MemoryActive, Content: content,
		SessionID: "thread_1", WakeAt: wakeAt, Recurrence: recurrence, CreatedAt: h.now.Add(-72 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestTickWakesDueCommitmentOnce(t *testing.T) {
	h := newHarness(t, 20)
	due := h.commit(t, "提醒 owner 看 X", h.now.Add(-time.Minute), "")
	h.commit(t, "later", h.now.Add(time.Hour), "")
	for range 2 {
		if err := h.sched.Tick(context.Background()); err != nil {
			t.Fatalf("tick: %v", err)
		}
	}
	if len(h.runs.starts) != 1 {
		t.Fatalf("starts = %v, want exactly one", h.runs.starts)
	}
	start := h.runs.starts[0]
	if !strings.HasPrefix(start, "thread_1|commitment #1: 提醒 owner 看 X|[commitment #1, made 2026-10-02 09:00] 提醒 owner 看 X") {
		t.Fatalf("start = %q", start)
	}
	if got, _ := h.store.LoadMemoryItem(context.Background(), due.ID); got.Status != core.MemoryWoken {
		t.Fatalf("status = %s", got.Status)
	}
	if len(h.store.events) != 1 || h.store.events[0] != "run_1:wake.fired" {
		t.Fatalf("events = %v", h.store.events)
	}
}

func TestTickRespectsDailyLimitPerLocalDay(t *testing.T) {
	h := newHarness(t, 1)
	h.commit(t, "a", h.now.Add(-time.Minute), "")
	h.commit(t, "b", h.now.Add(-time.Minute), "")
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(h.runs.starts) != 1 {
		t.Fatalf("starts = %v, want the limit of 1", h.runs.starts)
	}
	h.now = h.now.Add(16 * time.Hour) // 01:00 the next day in Shanghai
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(h.runs.starts) != 2 {
		t.Fatalf("deferred commitment did not wake the next day: %v", h.runs.starts)
	}

	off := newHarness(t, 0)
	off.commit(t, "a", off.now.Add(-time.Minute), "")
	if err := off.sched.Tick(context.Background()); err != nil || len(off.runs.starts) != 0 {
		t.Fatalf("daily_limit 0 must disable wakes: starts=%v err=%v", off.runs.starts, err)
	}
}

func TestTickSchedulesNextRecurrence(t *testing.T) {
	h := newHarness(t, 20)
	h.commit(t, "早安简报", h.now.Add(-time.Minute), "0 8 * * *")
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	items, _ := h.store.ListMemoryItems(context.Background(), []core.MemoryStatus{core.MemoryActive})
	if len(items) != 1 || items[0].Recurrence != "0 8 * * *" {
		t.Fatalf("next occurrence = %+v", items)
	}
	if want := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC); !items[0].WakeAt.Equal(want) {
		t.Fatalf("next wake = %v, want %v (08:00 Shanghai tomorrow)", items[0].WakeAt, want)
	}
}

func TestTickRetriesWhenRunCannotStart(t *testing.T) {
	h := newHarness(t, 20)
	item := h.commit(t, "x", h.now.Add(-time.Minute), "")
	h.runs.fail = errors.New("thread store down")
	err := h.sched.Tick(context.Background())
	if err == nil || !strings.Contains(err.Error(), "thread store down") {
		t.Fatalf("tick error = %v", err)
	}
	got, _ := h.store.LoadMemoryItem(context.Background(), item.ID)
	if got.Status != core.MemoryActive || !got.WakeAt.Equal(h.now.Add(retryDelay)) {
		t.Fatalf("commitment = %+v, want active and moved by %v", got, retryDelay)
	}
	if len(h.store.events) != 0 {
		t.Fatalf("no wake may be recorded for a run that did not start: %v", h.store.events)
	}
}

func TestTickAppliesDecay(t *testing.T) {
	h := newHarness(t, 20)
	if _, err := h.store.AddMemoryItem(context.Background(), core.MemoryItem{
		Kind: core.MemoryThought, Status: core.MemoryActive, Content: "t", ExpiresAt: h.now.Add(-time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if got, _ := h.store.LoadMemoryItem(context.Background(), 1); got.Status != core.MemoryResting {
		t.Fatalf("status = %s, want resting", got.Status)
	}
}

func TestNewSchedulerRequiresDependencies(t *testing.T) {
	if _, err := NewScheduler(Config{}); err == nil {
		t.Fatal("empty config must fail")
	}
}

type countingFlusher struct{ calls int }

func (f *countingFlusher) FlushDue(context.Context) error { f.calls++; return nil }

func TestTickFlushesQueuedNotifications(t *testing.T) {
	h := newHarness(t, 20)
	flusher := &countingFlusher{}
	h.sched.WithNotifications(flusher)
	if err := h.sched.Tick(context.Background()); err != nil || flusher.calls != 1 {
		t.Fatalf("flush calls = %d err=%v", flusher.calls, err)
	}
}

func (s *memStore) SumAutonomousTokensSince(_ context.Context, since time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.usageAt.Before(since) {
		return 0, nil
	}
	return s.tokens, nil
}
func (s *memStore) UsageReport(ctx context.Context, since time.Time) (core.UsageReport, error) {
	n, err := s.SumAutonomousTokensSince(ctx, since)
	return core.UsageReport{AutonomousTokens: n}, err
}
