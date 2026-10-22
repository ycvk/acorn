package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/watch"
	"github.com/ycvk/acorn/internal/webaccess"
)

// toolFeedDiscoverer returns a fixed discovery and records the pages asked.
type toolFeedDiscoverer struct {
	found webaccess.FeedDiscovery
	pages []string
}

func (d *toolFeedDiscoverer) DiscoverFeeds(_ context.Context, page string) (webaccess.FeedDiscovery, error) {
	d.pages = append(d.pages, page)
	return d.found, nil
}

// toolNewsSearcher returns fixed stories and records the queries.
type toolNewsSearcher struct {
	stories []core.NewsStory
	queries []core.NewsQuery
}

func (s *toolNewsSearcher) SearchNewsStories(_ context.Context, query core.NewsQuery) ([]core.NewsStory, error) {
	s.queries = append(s.queries, query)
	return s.stories, nil
}

// toolWatchStore keeps watches, pending items and news sections in memory.
type toolWatchStore struct {
	core.WatchStore
	watches  []core.Watch
	pending  []core.WatchItem
	sections []core.NewsSection
}

func (s *toolWatchStore) ListNewsSections(context.Context) ([]core.NewsSection, error) {
	return slices.Clone(s.sections), nil
}

func (s *toolWatchStore) SaveNewsSection(_ context.Context, section core.NewsSection) (core.NewsSection, error) {
	for i := range s.sections {
		if s.sections[i].Name == section.Name {
			s.sections[i] = section
			return section, nil
		}
	}
	s.sections = append(s.sections, section)
	return section, nil
}

func (s *toolWatchStore) RenameNewsSection(_ context.Context, from, to string, _ time.Time) error {
	for i := range s.sections {
		if s.sections[i].Name == from {
			s.sections[i].Name = to
			for j := range s.watches {
				if s.watches[j].Section == from {
					s.watches[j].Section = to
				}
			}
			return nil
		}
	}
	return fmt.Errorf("%w: %q", core.ErrNewsSectionNotFound, from)
}

func (s *toolWatchStore) DeleteNewsSection(_ context.Context, name string) error {
	for _, w := range s.watches {
		if w.Section == name {
			return fmt.Errorf("%w: %q has %s", core.ErrNewsSectionInUse, name, w.Name)
		}
	}
	s.sections = slices.DeleteFunc(s.sections, func(section core.NewsSection) bool { return section.Name == name })
	return nil
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

// Apply writes only the check fields, as the real checker does.
func (c *fakeWatchChecker) Apply(_ context.Context, w core.Watch, f watch.Fetched) (watch.Result, error) {
	stored := &c.store.watches[w.ID-1]
	stored.LastCheckedAt, stored.NextCheckAt = watchToolNow, watchToolNow.Add(w.Interval)
	stored.Snapshot = f.Snapshot
	return watch.Result{Watch: *stored, Baseline: len(f.Items)}, nil
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
	deps := WatchToolDeps{Store: store, News: &toolNewsSearcher{}, Checker: checker, Context: fixedArtifactContext{runID: "run_1", sessionID: "thread_7", callID: "call_1"}, Clock: func() time.Time { return watchToolNow }, Location: loc}
	tools := map[string]einotool.BaseTool{}
	for name, build := range map[string]func(WatchToolDeps) (einotool.BaseTool, error){
		"watch_create": buildWatchCreateTool, "watch_update": buildWatchUpdateTool, "watch_list": buildWatchListTool,
		"news_sections": buildNewsSectionsTool,
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

func TestNewsSectionsAndNewsSources(t *testing.T) {
	store, _, tools := newWatchToolsForTest(t)
	if _, err := runPresenceTool(t, tools["watch_create"], `{"name":"中新网要闻","kind":"rss","target":"https://n.test/importnews.xml","mode":"news"}`); err == nil || !strings.Contains(err.Error(), "needs a section") {
		t.Fatalf("news without a section err = %v", err)
	}
	if _, err := runPresenceTool(t, tools["news_sections"], `{"action":"set","name":"国内要闻","preference":"1. 政策与民生"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := runPresenceTool(t, tools["news_sections"], `{"action":"set","name":"AI"}`); err != nil {
		t.Fatal(err)
	}
	if got := store.sections; len(got) != 2 || got[0].Position != 1 || got[1].Position != 2 || got[0].Preference != "1. 政策与民生" {
		t.Fatalf("sections = %+v", got)
	}
	if _, err := runPresenceTool(t, tools["watch_create"], `{"name":"AI225","kind":"rss","target":"https://ai225.test/rss.xml","mode":"news","section":"AI","issue":true}`); err != nil {
		t.Fatal(err)
	}
	if w := store.watches[0]; w.Mode != core.WatchModeNews || w.Section != "AI" || !w.Issue {
		t.Fatalf("news source = %+v", w)
	}
	if _, err := runPresenceTool(t, tools["watch_create"], `{"name":"x","kind":"rss","target":"https://x.test","section":"AI"}`); err == nil {
		t.Fatal("a digest watch takes no section")
	}

	out, err := runPresenceTool(t, tools["news_sections"], `{"action":"set","name":"AI","rename":"AI 与科技前沿","preference":"1. 模型与 agent\n2. 开发工具"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"name":"AI 与科技前沿"`, `"preference":"1. 模型与 agent\n2. 开发工具"`, `"name":"AI225"`, `"issue":true`} {
		if !strings.Contains(out, want) {
			t.Fatalf("output lacks %s: %s", want, out)
		}
	}
	if store.watches[0].Section != "AI 与科技前沿" {
		t.Fatalf("the source did not move with the rename: %+v", store.watches[0])
	}
	if _, err := runPresenceTool(t, tools["news_sections"], `{"action":"remove","name":"AI 与科技前沿"}`); !errors.Is(err, core.ErrNewsSectionInUse) {
		t.Fatalf("remove in use err = %v", err)
	}
	if _, err := runPresenceTool(t, tools["news_sections"], `{"action":"set","name":"体育","rename":"运动"}`); !errors.Is(err, core.ErrNewsSectionNotFound) {
		t.Fatalf("rename missing err = %v", err)
	}

	if _, err := runPresenceTool(t, tools["watch_update"], `{"id":1,"mode":"digest"}`); err != nil {
		t.Fatal(err)
	}
	if w := store.watches[0]; w.Mode != core.WatchModeDigest || w.Section != "" || w.Issue {
		t.Fatalf("a digest watch keeps no section: %+v", w)
	}
	if _, err := runPresenceTool(t, tools["watch_update"], `{"id":1,"section":"国内要闻"}`); err == nil {
		t.Fatal("a digest watch cannot take a section")
	}
	if _, err := runPresenceTool(t, tools["watch_update"], `{"id":1,"mode":"news","section":"国内要闻","issue":false}`); err != nil {
		t.Fatal(err)
	}
	if w := store.watches[0]; w.Mode != core.WatchModeNews || w.Section != "国内要闻" || w.Issue {
		t.Fatalf("back to news = %+v", w)
	}
	if _, err := runPresenceTool(t, tools["news_sections"], `{"action":"remove","name":"AI 与科技前沿"}`); err != nil {
		t.Fatalf("remove an empty section: %v", err)
	}
	if _, err := runPresenceTool(t, tools["news_sections"], `{"action":"drop"}`); err == nil {
		t.Fatal("an unknown action must fail")
	}
}

func TestNewsSearchListsStoriesWithTheirItems(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	searcher := &toolNewsSearcher{stories: []core.NewsStory{{
		Title: "停火谈判", Section: "国际", Score: 5, Summary: "双方在开罗谈判。", Development: "进入第二轮。",
		FirstSeen: watchToolNow.Add(-48 * time.Hour), UpdatedAt: watchToolNow.Add(-time.Hour),
		Items: []core.NewsStoryItem{
			{WatchItem: core.WatchItem{Title: "停火谈判进入第二轮", URL: "https://bbc.test/1", SeenAt: watchToolNow.Add(-2 * time.Hour)}, Source: "BBC", Score: 5},
		},
	}}}
	store := &toolWatchStore{sections: []core.NewsSection{{Name: "国际"}, {Name: "AI"}}}
	tool, err := buildNewsSearchTool(WatchToolDeps{Store: store, News: searcher, Clock: func() time.Time { return watchToolNow }, Location: loc})
	if err != nil {
		t.Fatal(err)
	}
	out, err := runPresenceTool(t, tool, `{"query":"停火","section":"国际"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"title":"停火谈判"`, `"development":"进入第二轮。"`, `"first_seen":"2026-10-01 Thu 09:00"`, `"source":"BBC"`, `"url":"https://bbc.test/1"`, `"published":"2026-10-03 Sat 07:00"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("news_search output lacks %s: %s", want, out)
		}
	}
	if q := searcher.queries[0]; q.Text != "停火" || q.Section != "国际" || q.Limit != 10 || !q.Since.Equal(watchToolNow.Add(-72*time.Hour)) {
		t.Fatalf("query = %+v", q)
	}
	if _, err := runPresenceTool(t, tool, `{"days":14,"limit":30}`); err != nil || !searcher.queries[1].Since.Equal(watchToolNow.Add(-14*24*time.Hour)) || searcher.queries[1].Section != "" {
		t.Fatalf("widest search: %v %+v", err, searcher.queries[1])
	}
	for input, want := range map[string]string{
		`{"section":"体育"}`: "the sections are 国际, AI",
		`{"days":15}`:      "days 15 must be from 1 to 14",
		`{"limit":31}`:     "limit 31 must be from 1 to 30",
	} {
		if _, err := runPresenceTool(t, tool, input); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s err = %v", input, err)
		}
	}
	if len(searcher.queries) != 2 {
		t.Fatalf("rejected searches reached the store: %+v", searcher.queries)
	}
}

func TestFeedDiscoverListsTheFeedsFound(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	discoverer := &toolFeedDiscoverer{found: webaccess.FeedDiscovery{Tried: 9, Feeds: []webaccess.DiscoveredFeed{
		{URL: "https://blog.test/feed", FoundBy: "link", Title: "Blog", Entries: 20, PerDay: 2.345, Latest: watchToolNow, Samples: []string{"a", "b"}},
		{URL: "https://blog.test/old.xml", FoundBy: "path", Entries: 3},
	}}}
	tool, err := buildFeedDiscoverTool(WatchToolDeps{Feeds: discoverer, Location: loc})
	if err != nil {
		t.Fatal(err)
	}
	out, err := runPresenceTool(t, tool, `{"url":" https://blog.test/ "}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"url":"https://blog.test/feed","found_by":"link","title":"Blog","entries":20,"per_day":2.3,"latest":"2026-10-03 Sat 09:00","samples":["a","b"]`, `"tried":9`} {
		if !strings.Contains(out, want) {
			t.Fatalf("feed_discover output lacks %s: %s", want, out)
		}
	}
	if strings.Contains(out, "note") || discoverer.pages[0] != "https://blog.test/" {
		t.Fatalf("output %s, pages %v", out, discoverer.pages)
	}
	discoverer.found = webaccess.FeedDiscovery{Tried: 12}
	if out, err := runPresenceTool(t, tool, `{"url":"https://nofeed.test"}`); err != nil || !strings.Contains(out, `"feeds":[]`) || !strings.Contains(out, "rsshub:/route") {
		t.Fatalf("no feed: %s, %v", out, err)
	}
}
