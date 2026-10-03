package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/watch"
)

// toolWatchStore keeps watches and pending items in memory.
type toolWatchStore struct {
	core.WatchStore
	watches []core.Watch
	pending []core.WatchItem
}

func (s *toolWatchStore) AddWatch(_ context.Context, w core.Watch) (core.Watch, error) {
	w.ID = int64(len(s.watches) + 1)
	s.watches = append(s.watches, w)
	return w, nil
}

func (s *toolWatchStore) LoadWatch(_ context.Context, id int64) (*core.Watch, error) {
	if id < 1 || int(id) > len(s.watches) {
		return nil, fmt.Errorf("%w: %d", core.ErrWatchNotFound, id)
	}
	w := s.watches[id-1]
	return &w, nil
}

func (s *toolWatchStore) UpdateWatch(_ context.Context, w core.Watch) error {
	s.watches[w.ID-1] = w
	return nil
}

func (s *toolWatchStore) ListWatches(context.Context) ([]core.Watch, error) {
	return s.watches, nil
}

func (s *toolWatchStore) ListWatchItems(context.Context, core.WatchItemStatus, int) ([]core.WatchItem, error) {
	return s.pending, nil
}

// fakeWatchChecker validates everything but "bad" targets and fetches fixed items.
type fakeWatchChecker struct {
	store    *toolWatchStore
	fetchErr error
	fetched  []core.Watch
}

func (c *fakeWatchChecker) Validate(kind core.WatchKind, target, _ string) error {
	if strings.Contains(target, "bad") || kind == "email" {
		return errors.New("unavailable source")
	}
	return nil
}

func (c *fakeWatchChecker) Fetch(_ context.Context, w core.Watch) (watch.Fetched, error) {
	c.fetched = append(c.fetched, w)
	if c.fetchErr != nil {
		return watch.Fetched{}, c.fetchErr
	}
	if w.Kind == core.WatchWeb {
		return watch.Fetched{Snapshot: "¥2,999"}, nil
	}
	return watch.Fetched{Items: []core.WatchItem{{Title: "a"}, {Title: "b"}, {Title: "c"}, {Title: "d"}}}, nil
}

func (c *fakeWatchChecker) Apply(_ context.Context, w core.Watch, f watch.Fetched) (watch.Result, error) {
	w.LastCheckedAt, w.NextCheckAt = watchToolNow, watchToolNow.Add(w.Interval)
	w.Snapshot = f.Snapshot
	c.store.watches[w.ID-1] = w
	return watch.Result{Watch: w, Baseline: len(f.Items)}, nil
}

var watchToolNow = time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)

func newWatchToolsForTest(t *testing.T) (*toolWatchStore, *fakeWatchChecker, map[string]einotool.BaseTool) {
	t.Helper()
	store := &toolWatchStore{}
	checker := &fakeWatchChecker{store: store}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	deps := WatchToolDeps{Store: store, Checker: checker, Context: fixedArtifactContext{runID: "run_1", sessionID: "thread_7", callID: "call_1"}, Clock: func() time.Time { return watchToolNow }, Location: loc}
	tools := map[string]einotool.BaseTool{}
	for name, build := range map[string]func(WatchToolDeps) (einotool.BaseTool, error){
		"watch_create": buildWatchCreateTool, "watch_update": buildWatchUpdateTool, "watch_list": buildWatchListTool,
	} {
		tool, err := build(deps)
		if err != nil {
			t.Fatal(err)
		}
		tools[name] = tool
	}
	return store, checker, tools
}

func TestWatchCreateTakesABaseline(t *testing.T) {
	store, _, tools := newWatchToolsForTest(t)
	out, err := runPresenceTool(t, tools["watch_create"], `{"name":"Go releases","kind":"github","target":"golang/go"}`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	w := store.watches[0]
	if w.Selector != "releases" || w.Mode != core.WatchModeDigest || w.Interval != time.Hour || w.SessionID != "thread_7" || w.Status != core.WatchActive {
		t.Fatalf("watch = %+v", w)
	}
	for _, want := range []string{`"baseline_items":4`, `"sample":["a","b","c"]`, `"next_check":"2026-10-03 Sat 10:00"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("output lacks %s: %s", want, out)
		}
	}
	out, err = runPresenceTool(t, tools["watch_create"], `{"name":"phone","kind":"web","target":"https://shop.test/p","selector":".price","mode":"immediate","every":"30m"}`)
	if err != nil || !strings.Contains(out, `"sample":["¥2,999"]`) || store.watches[1].Mode != core.WatchModeImmediate || store.watches[1].Interval != 30*time.Minute {
		t.Fatalf("web create = %s, %v (%+v)", out, err, store.watches[1])
	}
}

func TestWatchCreateRejects(t *testing.T) {
	store, checker, tools := newWatchToolsForTest(t)
	for name, args := range map[string]string{
		"unavailable kind": `{"name":"x","kind":"email","target":"me"}`,
		"bad target":       `{"name":"x","kind":"rss","target":"https://bad.test"}`,
		"short interval":   `{"name":"x","kind":"rss","target":"https://ok.test","every":"5m"}`,
		"bad mode":         `{"name":"x","kind":"rss","target":"https://ok.test","mode":"loud"}`,
		"no name":          `{"kind":"rss","target":"https://ok.test"}`,
	} {
		if _, err := runPresenceTool(t, tools["watch_create"], args); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	checker.fetchErr = errors.New("HTTP 403")
	if _, err := runPresenceTool(t, tools["watch_create"], `{"name":"x","kind":"rss","target":"https://ok.test"}`); err == nil || !strings.Contains(err.Error(), "was not created: HTTP 403") {
		t.Fatalf("failed first check err = %v", err)
	}
	if len(store.watches) != 0 {
		t.Fatalf("rejected watches were stored: %+v", store.watches)
	}
}

func TestWatchUpdateAndList(t *testing.T) {
	store, checker, tools := newWatchToolsForTest(t)
	if _, err := runPresenceTool(t, tools["watch_create"], `{"name":"phone","kind":"web","target":"https://shop.test/p","selector":".price"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := runPresenceTool(t, tools["watch_update"], `{"id":1,"status":"paused","every":"2h","mode":"immediate"}`); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if w := store.watches[0]; w.Status != core.WatchPaused || w.Interval != 2*time.Hour || w.Mode != core.WatchModeImmediate {
		t.Fatalf("paused = %+v", w)
	}
	store.watches[0].Failures, store.watches[0].LastError = 3, "HTTP 500"
	if _, err := runPresenceTool(t, tools["watch_update"], `{"id":1,"status":"active"}`); err != nil {
		t.Fatal(err)
	}
	if w := store.watches[0]; w.Status != core.WatchActive || w.Failures != 0 || !w.NextCheckAt.Equal(watchToolNow) {
		t.Fatalf("resumed = %+v", w)
	}
	if _, err := runPresenceTool(t, tools["watch_update"], `{"id":1,"selector":".sale-price"}`); err != nil {
		t.Fatal(err)
	}
	if last := checker.fetched[len(checker.fetched)-1]; last.Selector != ".sale-price" || !store.watches[0].LastCheckedAt.Equal(watchToolNow) {
		t.Fatalf("selector change must re-baseline: %+v", last)
	}
	if _, err := runPresenceTool(t, tools["watch_update"], `{"id":9,"name":"x"}`); !errors.Is(err, core.ErrWatchNotFound) {
		t.Fatalf("missing watch err = %v", err)
	}

	store.pending = []core.WatchItem{{WatchID: 1}, {WatchID: 1}}
	out, err := runPresenceTool(t, tools["watch_list"], `{}`)
	if err != nil || !strings.Contains(out, `"pending_for_briefing":2`) || !strings.Contains(out, `"selector":".sale-price"`) {
		t.Fatalf("list = %s, %v", out, err)
	}
}
