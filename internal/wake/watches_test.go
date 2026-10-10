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
	"github.com/ycvk/acorn/internal/watch"
)

// memWatches is an in-memory core.WatchStore.
type memWatches struct {
	mu      sync.Mutex
	watches []core.Watch
	items   []core.WatchItem
}

func newMemWatches() *memWatches { return &memWatches{} }

func (m *memWatches) AddWatch(_ context.Context, w core.Watch) (core.Watch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w.ID = int64(len(m.watches) + 1)
	m.watches = append(m.watches, w)
	return w, nil
}

func (m *memWatches) LoadWatch(_ context.Context, id int64) (*core.Watch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.watches[id-1]
	return &w, nil
}

func (m *memWatches) ListWatches(context.Context) ([]core.Watch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]core.Watch(nil), m.watches...), nil
}

func (m *memWatches) UpdateWatch(_ context.Context, w core.Watch) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watches[w.ID-1] = w
	return nil
}

func (m *memWatches) ListDueWatches(_ context.Context, now time.Time, limit int) ([]core.Watch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []core.Watch
	for _, w := range m.watches {
		if w.Status != core.WatchPaused && !w.NextCheckAt.After(now) && len(out) < limit {
			out = append(out, w)
		}
	}
	return out, nil
}

func (m *memWatches) ClaimDueWatch(_ context.Context, id int64, now time.Time, lease time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := &m.watches[id-1]
	if w.Status == core.WatchPaused || w.NextCheckAt.After(now) {
		return fmt.Errorf("%w: %d", core.ErrWatchNotDue, id)
	}
	w.NextCheckAt = now.Add(lease)
	return nil
}

func (m *memWatches) RecordWatchCheck(_ context.Context, from core.Watch, check core.WatchCheck) ([]core.WatchItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := &m.watches[from.ID-1]
	if w.Status != from.Status || w.Failures != from.Failures || !w.LastCheckedAt.Equal(from.LastCheckedAt) {
		return nil, fmt.Errorf("%w: %d", core.ErrWatchChanged, from.ID)
	}
	w.Status, w.NextCheckAt, w.LastCheckedAt, w.UpdatedAt = check.Status, check.NextCheckAt, check.At, check.At
	w.Failures, w.LastError, w.Snapshot = check.Failures, check.LastError, check.Snapshot
	var added []core.WatchItem
	for _, item := range check.Items {
		item.ID = int64(len(m.items) + 1)
		m.items = append(m.items, item)
		added = append(added, item)
	}
	return added, nil
}

func (m *memWatches) ListWatchItems(_ context.Context, status core.WatchItemStatus, limit int) ([]core.WatchItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []core.WatchItem
	for _, item := range m.items {
		if item.Status == status && len(out) < limit {
			out = append(out, item)
		}
	}
	return out, nil
}

func (m *memWatches) MarkWatchItems(_ context.Context, ids []int64, status core.WatchItemStatus, runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		m.items[id-1].Status, m.items[id-1].RunID = status, runID
	}
	return nil
}

// fakeChecker returns the items queued for a watch as new, stored pending.
type fakeChecker struct {
	watches *memWatches
	found   map[int64][]core.WatchItem
	failing map[int64]error
	checked []int64
	clock   func() time.Time
}

func (c *fakeChecker) Check(ctx context.Context, w core.Watch) (watch.Result, error) {
	c.checked = append(c.checked, w.ID)
	if err := c.failing[w.ID]; err != nil {
		return watch.Result{Watch: w}, err
	}
	items := c.found[w.ID]
	delete(c.found, w.ID)
	for i := range items {
		items[i].WatchID, items[i].Status, items[i].SeenAt = w.ID, core.WatchItemPending, c.clock()
	}
	check := core.WatchCheck{
		At: c.clock(), NextCheckAt: c.clock().Add(w.Interval), Status: w.Status,
		Failures: w.Failures, LastError: w.LastError, Snapshot: w.Snapshot, Items: items,
	}
	added, err := c.watches.RecordWatchCheck(ctx, w, check)
	if err != nil {
		return watch.Result{Watch: w}, err
	}
	w.LastCheckedAt, w.NextCheckAt = check.At, check.NextCheckAt
	return watch.Result{Watch: w, New: added}, nil
}

func (h *harness) watch(t *testing.T, name string, mode core.WatchMode) core.Watch {
	t.Helper()
	w, err := h.watches.AddWatch(context.Background(), core.Watch{
		Name: name, Kind: core.WatchRSS, Target: "https://x.test/feed", Mode: mode, Interval: time.Hour,
		Status: core.WatchActive, SessionID: "thread_w", NextCheckAt: h.now.Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func (h *harness) find(w core.Watch, titles ...string) {
	for _, title := range titles {
		h.checker.found[w.ID] = append(h.checker.found[w.ID], core.WatchItem{Key: title, Title: title, URL: "https://x.test/" + title, Summary: "about " + title})
	}
}

func TestDigestWatchItemsWaitForTheBriefing(t *testing.T) {
	h := newHarness(t, 20)
	w := h.watch(t, "blog", core.WatchModeDigest)
	h.find(w, "post-1")
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(h.runs.starts) != 0 {
		t.Fatalf("a digest watch started runs: %v", h.runs.starts)
	}
	pending, _ := h.watches.ListWatchItems(context.Background(), core.WatchItemPending, 10)
	if len(pending) != 1 || pending[0].Title != "post-1" {
		t.Fatalf("pending = %+v", pending)
	}
	if err := h.sched.Tick(context.Background()); err != nil || len(h.checker.checked) != 1 {
		t.Fatalf("a checked watch is not due again within its interval: %v, %v", h.checker.checked, err)
	}
}

func TestImmediateWatchWakesInItsThreadWithinTheLimit(t *testing.T) {
	h := newHarness(t, 1)
	w := h.watch(t, "phone price", core.WatchModeImmediate)
	h.find(w, "changed")
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(h.runs.starts) != 1 {
		t.Fatalf("starts = %v", h.runs.starts)
	}
	parts := strings.SplitN(h.runs.starts[0], "|", 3)
	if parts[0] != "thread_w" || parts[1] != "watch #1 phone price: 1 new" ||
		parts[2] != "[watch #1 phone price] 1 new\n- changed — https://x.test/changed\n  about changed\n" {
		t.Fatalf("start = %q", h.runs.starts[0])
	}
	if h.watches.items[0].Status != core.WatchItemWoken || h.watches.items[0].RunID != "run_1" {
		t.Fatalf("item = %+v", h.watches.items[0])
	}
	if len(h.store.wakes) != 1 {
		t.Fatalf("watch wakes must count toward the daily limit: %v", h.store.events)
	}

	h.now = h.now.Add(2 * time.Hour)
	h.find(w, "changed-again")
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(h.runs.starts) != 1 || h.watches.items[1].Status != core.WatchItemPending {
		t.Fatalf("over the limit the items must wait for the briefing: starts %v item %+v", h.runs.starts, h.watches.items[1])
	}
}

func TestWatchFailureDoesNotStopOthers(t *testing.T) {
	h := newHarness(t, 20)
	broken := h.watch(t, "broken", core.WatchModeImmediate)
	ok := h.watch(t, "ok", core.WatchModeImmediate)
	h.checker.failing = map[int64]error{broken.ID: errors.New("connection refused")}
	h.find(ok, "news")
	due := h.commit(t, "提醒", h.now.Add(-time.Minute), "")
	err := h.sched.Tick(context.Background())
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("tick err = %v", err)
	}
	item, loadErr := h.data.LoadCommitment(context.Background(), due.ID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(h.runs.starts) != 2 || item.State != "due" {
		t.Fatalf("the commitment and the healthy watch must still wake: %v", h.runs.starts)
	}
}

func TestMorningBriefingRunsOncePerDay(t *testing.T) {
	h := newHarnessWith(t, 0, Briefing{Enabled: true, At: 8 * time.Hour})
	h.now = time.Date(2026, 10, 5, 23, 0, 0, 0, time.UTC) // 07:00 on the 6th in Shanghai
	blog := h.watch(t, "blog", core.WatchModeDigest)
	h.find(blog, "post-1", "post-2")
	failing := h.watch(t, "shop", core.WatchModeDigest)
	h.watches.watches[failing.ID-1].Status = core.WatchFailing
	h.watches.watches[failing.ID-1].LastError = "HTTP 403"
	h.watches.watches[failing.ID-1].Failures = 5
	h.watches.watches[failing.ID-1].NextCheckAt = h.now.Add(time.Hour)
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(h.runs.starts) != 0 {
		t.Fatalf("briefed before 08:00: %v", h.runs.starts)
	}

	h.now = h.now.Add(time.Hour) // 08:00
	for range 2 {
		if err := h.sched.Tick(context.Background()); err != nil {
			t.Fatalf("tick: %v", err)
		}
	}
	if len(h.runs.starts) != 1 {
		t.Fatalf("briefings = %v, want one", h.runs.starts)
	}
	parts := strings.SplitN(h.runs.starts[0], "|", 3)
	want := "[briefing 2026-10-06] morning briefing\nWatch changes since the last briefing:\n\n## #1 blog (rss, 2 new)\n" +
		"- post-1 — https://x.test/post-1\n  about post-1\n- post-2 — https://x.test/post-2\n  about post-2\n" +
		"\nFailing watches:\n- #2 shop: HTTP 403 (5 failures in a row)\n"
	if parts[0] != "thread_briefings" || parts[1] != "morning briefing 2026-10-06" || parts[2] != want {
		t.Fatalf("briefing start = %q\nwant input:\n%s", h.runs.starts[0], want)
	}
	thread, err := h.data.LatestRoutineThread(context.Background(), "briefing")
	if err != nil || h.watches.items[0].Status != core.WatchItemBriefed || thread != "thread_briefings" {
		t.Fatalf("items=%+v thread=%q %v", h.watches.items, thread, err)
	}

	if !strings.Contains(strings.Join(h.store.events, ","), "run_1:"+EventBriefingFired) || len(h.store.wakes) != 0 {
		t.Fatalf("events = %v; a briefing is not a wake", h.store.events)
	}

	h.now = h.now.Add(24 * time.Hour)
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(h.runs.starts) != 2 || !strings.HasPrefix(h.runs.starts[1], "thread_briefings|") || !strings.Contains(h.runs.starts[1], "No watch changes since the last briefing.") {
		t.Fatalf("next day = %v", h.runs.starts)
	}
}

func TestBriefingStartFailureReleasesTheDay(t *testing.T) {
	h := newHarnessWith(t, 20, Briefing{Enabled: true, At: 0})
	h.runs.fail = errors.New("execution not ready")
	if err := h.sched.Tick(context.Background()); err == nil {
		t.Fatal("a failed briefing start must be reported")
	}
	h.runs.fail = nil
	if err := h.sched.Tick(context.Background()); err != nil || len(h.runs.starts) != 1 {
		t.Fatalf("retry = %v, %v", h.runs.starts, err)
	}
}
