package news

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/settings"
	"github.com/ycvk/acorn/internal/store"
)

var testNow = time.Date(2026, 10, 22, 2, 0, 0, 0, time.UTC)

// scriptedModel replies to each call with the next reply, built from the
// call's input.
type scriptedModel struct {
	mu      sync.Mutex
	inputs  []string
	replies []func(input string) string
	// fail, when set, decides the call's error before any reply.
	fail func(ctx context.Context) error
}

func (m *scriptedModel) GenerateNews(ctx context.Context, instruction, input string, _ int) (Generation, error) {
	if m.fail != nil {
		if err := m.fail(ctx); err != nil {
			return Generation{}, err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inputs = append(m.inputs, input)
	if len(m.replies) == 0 {
		return Generation{}, errors.New("no reply scripted")
	}
	reply := m.replies[0]
	m.replies = m.replies[1:]
	return Generation{Text: reply(input), InputTokens: 1000, OutputTokens: 200, Reported: true}, nil
}

type fixedPages map[string]string

func (p fixedPages) ReadPage(_ context.Context, url string) (string, error) {
	if page, ok := p[url]; ok {
		return page, nil
	}
	return "", errors.New("HTTP 404")
}

type fixture struct {
	store    *store.Store
	model    *scriptedModel
	proc     *Processor
	clock    time.Time
	ids      map[string]int64
	settings core.Settings
}

func (f *fixture) Settings(context.Context) (core.Settings, error) { return f.settings, nil }

func newFixture(t *testing.T, dailyTokens int) *fixture {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	loc, err := time.LoadLocation("Asia/Singapore")
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{store: db, model: &scriptedModel{}, clock: testNow, ids: map[string]int64{}, settings: core.Settings{Location: loc, NewsDailyTokens: dailyTokens}}
	f.proc, err = New(Config{
		Store: db, Model: f.model, ModelName: "test-model",
		Pages: fixedPages{"https://daily.test/2026-10-22": "## 1. DeepSeek 开源推理框架 ★★★★★\n链接 https://a.test/deepseek\n## 2. 停火谈判进入第二轮 ★★★"},
		Count: func(_ context.Context, s string) (int, error) { return len([]rune(s)) / 4, nil },
		Clock: func() time.Time { return f.clock }, Settings: f,
		Ready: func() error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i, name := range []string{"国际与财经", "AI 与科技前沿"} {
		if _, err := db.SaveNewsSection(ctx, core.NewsSection{Name: name, Position: i, Preference: "1. 最重要的", UpdatedAt: testNow}); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *fixture) source(t *testing.T, name, section string, issue bool, items ...core.WatchItem) {
	t.Helper()
	ctx := context.Background()
	w, err := f.store.AddWatch(ctx, core.Watch{Name: name, Kind: core.WatchRSS, Target: "https://" + name + ".test/feed", Mode: core.WatchModeNews, Section: section, Issue: issue,
		Interval: time.Hour, Status: core.WatchActive, NextCheckAt: testNow, CreatedAt: testNow})
	if err != nil {
		t.Fatal(err)
	}
	for i := range items {
		items[i].WatchID, items[i].Status, items[i].SeenAt = w.ID, core.WatchItemPending, testNow
	}
	added, err := f.store.RecordWatchCheck(ctx, w, core.WatchCheck{At: testNow, NextCheckAt: testNow.Add(time.Hour), Status: core.WatchActive, Items: items})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range added {
		f.ids[item.Key] = item.ID
	}
}

func (f *fixture) items(t *testing.T) {
	t.Helper()
	f.source(t, "bbc", "国际与财经", false,
		core.WatchItem{Key: "b1", Title: "停火谈判开始", Summary: "双方在开罗会谈", URL: "https://bbc.test/1", PublishedAt: testNow.Add(-time.Hour)},
		core.WatchItem{Key: "b2", Title: "明星婚礼", URL: "https://bbc.test/2"})
	f.source(t, "ft", "国际与财经", false, core.WatchItem{Key: "f1", Title: "Ceasefire talks open in Cairo", URL: "https://ft.test/1"})
	f.source(t, "daily", "AI 与科技前沿", true, core.WatchItem{Key: "2026-10-22", Title: "AI 日报 10-22", URL: "https://daily.test/2026-10-22", Summary: "本期概览"})
}

func TestProcessorSortsIssuesThenItemsIntoStories(t *testing.T) {
	f := newFixture(t, 0)
	f.items(t)
	f.model.replies = []func(string) string{
		func(string) string {
			return fmt.Sprintf(`{"items":[],"entries":[{"issue":%d,"title":"DeepSeek 开源推理框架","summary":"开源了推理框架。","url":"https://a.test/deepseek","score":5,"story":"n1"},{"issue":%d,"title":"停火谈判进入第二轮","summary":"谈判继续。","score":3,"story":"n2"}],"stories":[{"ref":"n1","section":"AI 与科技前沿","title":"DeepSeek 开源推理框架","summary":"DeepSeek 开源了推理框架。"},{"ref":"n2","section":"国际与财经","title":"停火谈判","summary":"谈判在开罗进行。"}]}`, f.ids["2026-10-22"], f.ids["2026-10-22"])
		},
		func(input string) string {
			story := ""
			for _, line := range strings.Split(input, "\n") {
				if strings.HasPrefix(line, "- s") && strings.Contains(line, "停火谈判") {
					story = strings.Fields(line)[1]
				}
			}
			return fmt.Sprintf("```json\n"+`{"items":[{"id":%d,"score":4,"story":"%s"},{"id":%d,"score":5,"story":"%s"},{"id":%d,"score":1}],"stories":[{"ref":"%s","title":"停火谈判","summary":"双方在开罗谈判，已进入第二轮。","development":"BBC 与 FT 报道谈判开始。"}]}`+"\n```",
				f.ids["b1"], story, f.ids["f1"], story, f.ids["b2"], story)
		},
	}
	if err := f.proc.CatchUp(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.model.inputs) != 2 {
		t.Fatalf("model calls = %d", len(f.model.inputs))
	}
	for _, want := range []string{"Sections:\n## 国际与财经\nPreference:\n1. 最重要的\n## AI 与科技前沿\n", "--- issue page ---\n## 1. DeepSeek 开源推理框架 ★★★★★", "Recent stories:\n(none)\n"} {
		if !strings.Contains(f.model.inputs[0], want) {
			t.Fatalf("issue input lacks %q:\n%s", want, f.model.inputs[0])
		}
	}
	for _, want := range []string{"[国际与财经] 停火谈判: 谈判在开罗进行。", fmt.Sprintf("- %d [国际与财经] bbc 10-22 09:00 | 停火谈判开始 | 双方在开罗会谈", f.ids["b1"])} {
		if !strings.Contains(f.model.inputs[1], want) {
			t.Fatalf("items input lacks %q:\n%s", want, f.model.inputs[1])
		}
	}
	news, err := f.store.BriefingNewsStories(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(news.Stories) != 2 {
		t.Fatalf("stories = %+v", news.Stories)
	}
	var ceasefire core.NewsStory
	for _, story := range news.Stories {
		if story.Title == "停火谈判" {
			ceasefire = story
		}
	}
	if ceasefire.Score != 5 || len(ceasefire.Items) != 3 || ceasefire.Development != "BBC 与 FT 报道谈判开始。" || ceasefire.Summary != "双方在开罗谈判，已进入第二轮。" {
		t.Fatalf("ceasefire = %+v", ceasefire)
	}
	used, err := f.store.MemoryUsageSince(context.Background(), testNow.Add(-time.Hour), "news")
	if err != nil || used != 2400 {
		t.Fatalf("news usage = %d, %v", used, err)
	}
	if worked, err := f.proc.ProcessOne(context.Background()); worked || err != nil {
		t.Fatalf("nothing should be left: %v %v", worked, err)
	}
}

func TestProcessorGivesUpOnAnIssueWhosePageCannotBeRead(t *testing.T) {
	f := newFixture(t, 0)
	f.source(t, "daily", "AI 与科技前沿", true, core.WatchItem{Key: "gone", Title: "AI 日报 10-21", URL: "https://daily.test/gone", Summary: "本期概览"})
	err := f.proc.CatchUp(context.Background())
	if err == nil || !strings.Contains(err.Error(), "read the page of issue") || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("catch up err = %v", err)
	}
	if len(f.model.inputs) != 0 {
		t.Fatalf("model calls = %d; an issue is not sorted without its page", len(f.model.inputs))
	}
	if n, err := f.store.FailedNewsItems(context.Background(), testNow.Add(-time.Hour)); err != nil || n != 1 {
		t.Fatalf("failed = %d, %v", n, err)
	}
}

func TestProcessorReleasesABatchItsCallerStopped(t *testing.T) {
	f := newFixture(t, 0)
	f.source(t, "bbc", "国际与财经", false, core.WatchItem{Key: "b1", Title: "t"})
	// The caller stops waiting while the model call is in flight, more often
	// than a batch has attempts.
	for range 4 {
		ctx, stop := context.WithCancel(context.Background())
		f.model.fail = func(call context.Context) error {
			stop()
			<-call.Done()
			return call.Err()
		}
		if worked, err := f.proc.ProcessOne(ctx); !worked || !errors.Is(err, context.Canceled) {
			t.Fatalf("worked=%v err=%v", worked, err)
		}
	}
	if n, err := f.store.FailedNewsItems(context.Background(), testNow.Add(-time.Hour)); err != nil || n != 0 {
		t.Fatalf("failed = %d, %v", n, err)
	}
	f.model.fail = nil
	f.model.replies = []func(string) string{func(string) string {
		return fmt.Sprintf(`{"items":[{"id":%d,"score":3,"story":"n1"}],"stories":[{"ref":"n1","section":"国际与财经","title":"t"}]}`, f.ids["b1"])
	}}
	if err := f.proc.CatchUp(context.Background()); err != nil || len(f.model.inputs) != 1 {
		t.Fatalf("the batch did not go on after the stops: %v, calls %d", err, len(f.model.inputs))
	}
}

func TestProcessorGivesUpOnABatchAfterThreeBadSortings(t *testing.T) {
	f := newFixture(t, 0)
	f.source(t, "bbc", "国际与财经", false, core.WatchItem{Key: "b1", Title: "t"})
	bad := func(string) string { return `{"items":[{"id":999,"score":3,"story":"n1"}]}` }
	f.model.replies = []func(string) string{bad, bad, bad}
	err := f.proc.CatchUp(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not a new item of the batch") {
		t.Fatalf("catch up err = %v", err)
	}
	if len(f.model.inputs) != 3 {
		t.Fatalf("model calls = %d", len(f.model.inputs))
	}
	if n, err := f.store.FailedNewsItems(context.Background(), testNow.Add(-time.Hour)); err != nil || n != 1 {
		t.Fatalf("failed = %d, %v", n, err)
	}
}

func TestProcessorLeavesItemsForTheNextDayWhenTheBudgetIsSpent(t *testing.T) {
	f := newFixture(t, 1000)
	f.source(t, "bbc", "国际与财经", false, core.WatchItem{Key: "b1", Title: "t"})
	if err := f.proc.CatchUp(context.Background()); err != nil {
		t.Fatalf("a spent budget is not an error: %v", err)
	}
	if len(f.model.inputs) != 0 {
		t.Fatal("the model was called past the budget")
	}
	f.clock = time.Date(2026, 10, 22, 16, 0, 0, 0, time.UTC) // 00:00 on the 23rd in Singapore
	f.model.replies = []func(string) string{func(string) string {
		return fmt.Sprintf(`{"items":[{"id":%d,"score":1}],"stories":[]}`, f.ids["b1"])
	}}
	f.settings.NewsDailyTokens = 0
	if err := f.proc.CatchUp(context.Background()); err != nil || len(f.model.inputs) != 1 {
		t.Fatalf("the next day sorts the item: %v, calls %d", err, len(f.model.inputs))
	}
}

func TestRaisingTheNewsBudgetSortsDeferredItemsTheSameDay(t *testing.T) {
	f := newFixture(t, 0)
	ctx := context.Background()
	owner, err := settings.NewService(f.store, f.store, func() time.Time { return f.clock })
	if err != nil {
		t.Fatal(err)
	}
	f.proc.cfg.Settings = owner
	save := func(key, value string) {
		t.Helper()
		if _, err := owner.Save(ctx, settings.Change{Set: map[string]json.RawMessage{key: json.RawMessage(value)}}, ""); err != nil {
			t.Fatal(err)
		}
	}
	save(settings.KeyTimezone, `"Asia/Singapore"`)
	save(settings.KeyNewsDailyTokens, `1000`)
	f.source(t, "bbc", "国际与财经", false, core.WatchItem{Key: "b1", Title: "t"})
	if err := f.proc.CatchUp(ctx); err != nil || len(f.model.inputs) != 0 {
		t.Fatalf("past the budget: %v, calls %d", err, len(f.model.inputs))
	}
	f.model.replies = []func(string) string{func(string) string {
		return fmt.Sprintf(`{"items":[{"id":%d,"score":1}],"stories":[]}`, f.ids["b1"])
	}}
	save(settings.KeyNewsDailyTokens, `100000`)
	if err := f.proc.CatchUp(ctx); err != nil || len(f.model.inputs) != 1 {
		t.Fatalf("the raised budget sorts the deferred item today: %v, calls %d", err, len(f.model.inputs))
	}
}

func TestParseSortingChecksTheBatch(t *testing.T) {
	batch := core.NewsBatch{Items: []core.NewsBatchItem{
		{Item: core.WatchItem{ID: 1}, Section: "国际"},
		{Item: core.WatchItem{ID: 2}, Section: "国际"},
		{Item: core.WatchItem{ID: 3, URL: "https://daily.test/2026-10-22"}, Section: "AI", Issue: true},
	}}
	recent := []core.NewsStory{{ID: 7, Section: "国际", Title: "旧事"}}
	sections := []string{"国际", "AI"}
	for name, reply := range map[string]string{
		"not json":           `here you go`,
		"missing item":       `{"items":[{"id":1,"score":3,"story":"s7"}]}`,
		"issue as item":      `{"items":[{"id":1,"score":3,"story":"s7"},{"id":2,"score":1},{"id":3,"score":2,"story":"s7"}]}`,
		"score out of range": `{"items":[{"id":1,"score":6,"story":"s7"},{"id":2,"score":1}]}`,
		"no story":           `{"items":[{"id":1,"score":3},{"id":2,"score":1}]}`,
		"unknown story":      `{"items":[{"id":1,"score":3,"story":"s8"},{"id":2,"score":1}]}`,
		"undefined new":      `{"items":[{"id":1,"score":3,"story":"n1"},{"id":2,"score":1}]}`,
		"bad section":        `{"items":[{"id":1,"score":3,"story":"n1"},{"id":2,"score":1}],"stories":[{"ref":"n1","section":"体育","title":"x"}]}`,
		"entry of non-issue": `{"items":[{"id":1,"score":3,"story":"s7"},{"id":2,"score":1}],"entries":[{"issue":1,"title":"x","score":2,"story":"s7"}]}`,
		"rewrite no title":   `{"items":[{"id":1,"score":3,"story":"s7"},{"id":2,"score":1}],"stories":[{"ref":"s7","summary":"x"}]}`,
		"script link":        `{"items":[{"id":1,"score":3,"story":"s7"},{"id":2,"score":1}],"entries":[{"issue":3,"title":"x","url":"javascript:alert(1)","score":2,"story":"s7"}]}`,
	} {
		if _, err := parseSorting(reply, batch, recent, sections); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	commit, err := parseSorting(`{"items":[{"id":1,"score":4,"story":"s7"},{"id":2,"score":1}],"entries":[{"issue":3,"title":" 新 事 ","summary":"x","score":5,"story":"n1"},{"issue":3,"title":"另一件","url":"/p/2","score":2,"story":"n1"}],"stories":[{"ref":"s7","title":"旧事","summary":"更新","development":"有新进展"},{"ref":"n1","section":"AI","title":"新事","summary":"s","development":"ignored"},{"ref":"n2","section":"AI","title":"无人用"}]}`, batch, recent, sections)
	if err != nil {
		t.Fatal(err)
	}
	if len(commit.Stories) != 2 || commit.Stories[0].ID != 7 || commit.Stories[0].Development != "有新进展" || commit.Stories[1].Development != "" {
		t.Fatalf("stories = %+v", commit.Stories)
	}
	if len(commit.Scores) != 3 || commit.Scores[2].ItemID != 3 || commit.Scores[2].Score != 0 || commit.Entries[0].Title != "新 事" {
		t.Fatalf("commit = %+v", commit)
	}
	if commit.Entries[0].URL != "https://daily.test/2026-10-22" || commit.Entries[1].URL != "https://daily.test/p/2" {
		t.Fatalf("entry links = %q, %q; links resolve against the issue page", commit.Entries[0].URL, commit.Entries[1].URL)
	}
}
