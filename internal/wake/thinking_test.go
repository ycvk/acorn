package wake

import (
	"context"
	"errors"
	"fmt"
	"slices"
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
	if _, err := h.data.CreateSession(ctx, "thread_idea", "idea"); err != nil {
		t.Fatal(err)
	}
	if _, err = h.data.SaveConcern(ctx, core.MemoryConcern{Title: "review my idea", State: "active", Reason: "owner asked", SourceID: source.ID, ThreadID: "thread_idea", ReviewAt: h.now, UpdatedAt: h.now}, 0); err != nil {
		t.Fatal(err)
	}

}

// concernIn creates a concern in a new conversation, last revised at.
func (h *harness) concernIn(t *testing.T, thread, title string, at time.Time) core.MemoryConcern {
	t.Helper()
	ctx := context.Background()
	if _, err := h.data.CreateSession(ctx, thread, title); err != nil {
		t.Fatal(err)
	}
	return h.reviseConcern(t, core.MemoryConcern{Title: title, State: "active", Reason: "owner asked", ThreadID: thread}, "owner asked", at)
}

// reviseConcern saves c's next revision with reason, at time at.
func (h *harness) reviseConcern(t *testing.T, c core.MemoryConcern, reason string, at time.Time) core.MemoryConcern {
	t.Helper()
	ctx := context.Background()
	id := fmt.Sprintf("%s-%d", c.ThreadID, c.Revision)
	source, err := h.data.RegisterMemorySource(ctx, core.MemorySource{ID: id, Kind: "fixture", ObjectID: id, Version: "1", Speaker: "owner", Body: reason, RecordedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	next := c
	next.Reason, next.SourceID, next.UpdatedAt = reason, source.ID, at
	saved, err := h.data.SaveConcern(ctx, next, c.Revision)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

// wanderAt moves the clock to an owner-local time of 2026-10-05 plus days
// and ticks.
func (h *harness) wanderAt(t *testing.T, days, hour int) {
	t.Helper()
	h.now = time.Date(2026, 10, 5+days, hour-8, 0, 0, 0, time.UTC)
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestWanderTakesTheConcernRevisedLongestAgoInItsConversation(t *testing.T) {
	h := newHarness(t, 20)
	h.settings.Wander = []time.Duration{10 * time.Hour, 15 * time.Hour}
	a := h.concernIn(t, "thread_a", "跟进 A 项目", h.now.Add(-2*time.Hour))
	b := h.concernIn(t, "thread_b", "跟进 B 项目", h.now.Add(-time.Hour))
	h.wanderAt(t, 0, 10)
	if len(h.runs.starts) != 1 || !strings.HasPrefix(h.runs.starts[0], "thread_a|idle thought 2026-10-05 10:00|[wander 2026-10-05 10:00] Advance concern "+a.ID+" rev 1 [active]: 跟进 A 项目\nLatest reason: owner asked") {
		t.Fatalf("first slot = %v", h.runs.starts)
	}
	if want := []string{a.SourceID}; !slices.Equal(h.runs.sources[0], want) {
		t.Fatalf("sources = %v, want %v", h.runs.sources[0], want)
	}
	a = h.reviseConcern(t, a, "看过 v1.4 的发布说明", h.now)
	h.wanderAt(t, 0, 15)
	if len(h.runs.starts) != 2 || !strings.HasPrefix(h.runs.starts[1], "thread_b|idle thought 2026-10-05 15:00|[wander 2026-10-05 15:00] Advance concern "+b.ID) {
		t.Fatalf("second slot = %v", h.runs.starts)
	}
	h.reviseConcern(t, b, "B 还没有新版本", h.now)
	h.wanderAt(t, 1, 10)
	if len(h.runs.starts) != 3 || !strings.HasPrefix(h.runs.starts[2], "thread_a|idle thought 2026-10-06 10:00|[wander 2026-10-06 10:00] Advance concern "+a.ID+" rev 2 [active]: 跟进 A 项目\nLatest reason: 看过 v1.4 的发布说明") {
		t.Fatalf("next day = %v", h.runs.starts)
	}
	if routine, err := h.data.LatestRoutineRun(context.Background(), "wander"); err != nil || routine.ThreadID != "thread_a" {
		t.Fatalf("wander routine = %+v, %v", routine, err)
	}
}

// A concern with an outcome is advanced with its outcome, review time and work
// directory; one without an outcome gets neither line.
func TestWanderWorksTowardAConcernsOutcome(t *testing.T) {
	h := newHarness(t, 20)
	h.settings.Wander = []time.Duration{10 * time.Hour, 15 * time.Hour}
	a := h.concernIn(t, "thread_a", "整理 A 项目的版本变化", h.now.Add(-2*time.Hour))
	a.Outcome = "一份 Markdown，列出 v1.4 到 v1.7 的不兼容变更和升级步骤"
	a.ReviewAt = time.Date(2026, 10, 5, 1, 30, 0, 0, time.UTC)
	a = h.reviseConcern(t, a, "列完了 v1.4 的变更", h.now.Add(-2*time.Hour))
	b := h.concernIn(t, "thread_b", "跟进 B 项目", h.now.Add(-time.Hour))
	h.wanderAt(t, 0, 10)
	want := "thread_a|idle thought 2026-10-05 10:00|[wander 2026-10-05 10:00] Advance concern " + a.ID + " rev 2 [active]: 整理 A 项目的版本变化\nOutcome: 一份 Markdown，列出 v1.4 到 v1.7 的不兼容变更和升级步骤\nLatest reason: 列完了 v1.4 的变更\nReview at: 2026-10-05 09:30\nWork directory: ~/work/" + a.ID + "/ on your workstation\n"
	if len(h.runs.starts) != 1 || !strings.HasPrefix(h.runs.starts[0], want) || !strings.Contains(h.runs.starts[0], "deliver it in this conversation, notify the owner once and set the concern to waiting; only the owner's confirmation resolves it") {
		t.Fatalf("slot of a concern with an outcome = %q", h.runs.starts)
	}
	a.ReviewAt = h.now.Add(48 * time.Hour)
	h.reviseConcern(t, a, "列完了 v1.5 的变更", h.now)
	h.wanderAt(t, 0, 15)
	if len(h.runs.starts) != 2 || !strings.Contains(h.runs.starts[1], "Advance concern "+b.ID+" rev 1 [active]: 跟进 B 项目\nLatest reason: owner asked\nReview at: not set\nThis is the conversation") || strings.Contains(h.runs.starts[1], "Work directory") {
		t.Fatalf("slot of a concern without an outcome = %q", h.runs.starts)
	}
}

func TestWanderSkipsAConcernWhoseConversationIsBusy(t *testing.T) {
	h := newHarness(t, 20)
	h.settings.Wander = []time.Duration{10 * time.Hour, 15 * time.Hour, 20 * time.Hour}
	ctx := context.Background()
	h.concernIn(t, "thread_a", "跟进 A 项目", h.now.Add(-2*time.Hour))
	b := h.concernIn(t, "thread_b", "跟进 B 项目", h.now.Add(-time.Hour))
	if err := h.data.CreateRun(ctx, core.RunCreateParams{RunID: "run_a", SessionID: "thread_a", TurnIndex: 1, Input: "帮我看看 A"}); err != nil {
		t.Fatal(err)
	}
	h.wanderAt(t, 0, 10)
	if len(h.runs.starts) != 1 || !strings.HasPrefix(h.runs.starts[0], "thread_b|") {
		t.Fatalf("a running conversation was not skipped: %v", h.runs.starts)
	}
	h.reviseConcern(t, b, "B 等待回复", h.now)
	if err := h.data.MarkInterrupted(ctx, "run_a", ""); err != nil {
		t.Fatal(err)
	}
	h.wanderAt(t, 0, 15)
	if len(h.runs.starts) != 2 || !strings.HasPrefix(h.runs.starts[1], "thread_b|") {
		t.Fatalf("a conversation waiting for approval was not skipped: %v", h.runs.starts)
	}
	if err := h.data.FinishRun(ctx, "run_a", core.RunStatusSucceeded, "好了", ""); err != nil {
		t.Fatal(err)
	}
	h.wanderAt(t, 0, 20)
	if len(h.runs.starts) != 3 || !strings.HasPrefix(h.runs.starts[2], "thread_a|") {
		t.Fatalf("a finished conversation was skipped: %v", h.runs.starts)
	}
}

func TestWanderLeavesConcernsOfADeletedConversation(t *testing.T) {
	h := newHarness(t, 20)
	h.settings.Wander = []time.Duration{10 * time.Hour}
	h.concernIn(t, "thread_a", "跟进 A 项目", h.now.Add(-time.Hour))
	if err := h.data.DeleteThread(context.Background(), "thread_a"); err != nil {
		t.Fatal(err)
	}
	h.wanderAt(t, 0, 10)
	if len(h.runs.starts) != 0 {
		t.Fatalf("a concern of a deleted conversation was advanced: %v", h.runs.starts)
	}
}

func TestWanderReleasesItsSlotWhenTheConversationIsGone(t *testing.T) {
	h := newHarness(t, 20)
	h.settings.Wander = []time.Duration{10 * time.Hour}
	h.concernIn(t, "thread_a", "跟进 A 项目", h.now.Add(-time.Hour))
	h.runs.fail = core.ErrSessionNotFound
	h.now = time.Date(2026, 10, 5, 2, 0, 0, 0, time.UTC)
	if err := h.sched.Tick(context.Background()); !errors.Is(err, core.ErrSessionNotFound) {
		t.Fatalf("tick error = %v", err)
	}
	if err := h.data.ClaimRoutine(context.Background(), "wander", "2026-10-05 10:00", h.now); err != nil {
		t.Fatalf("slot kept after the run could not start: %v", err)
	}
}

func TestNightRunsInThoughtsAndWanderInTheConcernsConversation(t *testing.T) {
	h := newHarness(t, 20)
	h.settings.Night, h.settings.Wander = core.DailyTime{Enabled: true, At: 3 * time.Hour}, []time.Duration{10 * time.Hour, 15 * time.Hour}
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
	for i, start := range h.runs.starts {
		thread := "thread_idea|"
		if i == 0 {
			thread = "thread_thoughts|"
		}
		if !strings.HasPrefix(start, thread) {
			t.Fatalf("start %d thread=%s", i, start)
		}
	}
	if len(h.store.wakes) != 3 {
		t.Fatalf("wakes=%v", h.store.wakes)
	}
}

func TestEmptyNightKeepsItsSlot(t *testing.T) {
	h := newHarness(t, 20)
	h.settings.Night = core.DailyTime{Enabled: true, At: 0}
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
	h := newHarnessWith(t, 20, core.DailyTime{Enabled: true, At: 0})
	h.settings.Night = core.DailyTime{Enabled: true, At: 0}
	h.settings.WakeDailyTokens = 100
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
	if err := h.sched.runRoutine(context.Background(), "night", "2026-10-05", "Thoughts", true, h.now, h.settings, prepare); err == nil {
		t.Fatal("preparation error hidden")
	}
	if err := h.data.ClaimRoutine(context.Background(), "night", "2026-10-05", h.now); err != nil {
		t.Fatalf("claim retained: %v", err)
	}
}

func TestRoutineConcurrentTicksStartOnce(t *testing.T) {
	h := newHarness(t, 20)
	h.settings.Night = core.DailyTime{Enabled: true, At: 0}
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
	h := newHarnessWith(t, 20, core.DailyTime{Enabled: true, At: 0})
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
	h := newHarnessWith(t, 20, core.DailyTime{})
	h.settings.WakeDailyTokens = 0
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

func TestNightListsSkillsTheAgentWroteOrLoaded(t *testing.T) {
	h := newHarness(t, 20)
	h.settings.Night = core.DailyTime{Enabled: true, At: 0}
	// Tool call events carry the wall clock time.
	h.now = time.Now().UTC().Add(time.Minute)
	ctx := context.Background()
	for _, w := range []core.SkillWrite{
		{ID: "orders", Markdown: "orders", Author: core.SkillAuthorAgent, At: h.now.Add(-time.Hour)},
		{ID: "morning-briefing", Markdown: "briefing", Author: core.SkillAuthorRelease, At: h.now.Add(-time.Hour)},
		{ID: "old", Markdown: "old", Author: core.SkillAuthorAgent, At: h.now.Add(-72 * time.Hour)},
	} {
		if _, err := h.data.WriteSkill(ctx, w); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.data.CreateRun(ctx, core.RunCreateParams{RunID: "run_s", Input: "orders"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.data.AppendEvent(ctx, "run_s", "tool.call.started", map[string]any{"tool_name": "skill", "arguments": `{"skill":"old"}`}); err != nil {
		t.Fatal(err)
	}
	if err := h.sched.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(h.runs.starts) != 1 {
		t.Fatalf("starts = %v", h.runs.starts)
	}
	night := h.runs.starts[0]
	for _, want := range []string{"- skill orders rev 1 [in use] written=true loads=0", "- skill old rev 1 [in use] written=false loads=1"} {
		if !strings.Contains(night, want) {
			t.Fatalf("night input lacks %q:\n%s", want, night)
		}
	}
	if strings.Contains(night, "morning-briefing") {
		t.Fatalf("a release write is listed:\n%s", night)
	}
}

func TestNightReviewsNewsSourcesOnSundayAndAfterATrialWeek(t *testing.T) {
	monday := time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC) // 09:00 on Monday in Shanghai
	bbc := core.NewsSourceStats{Watch: core.Watch{ID: 3, Name: "BBC 中文", Section: "国际与财经", CreatedAt: monday.Add(-30 * 24 * time.Hour)},
		Items: 40, Scored: 38, ScoreSum: 114, High: 12, Shared: 19, Picked: 6, Focus: 2, More: 1, Less: 3}
	trial := core.NewsSourceStats{Watch: core.Watch{ID: 12, Name: "知乎热榜", Section: "热榜", Trial: true, CreatedAt: monday.Add(-7*24*time.Hour - time.Hour)},
		Items: 70, Scored: 70, ScoreSum: 140, High: 3, Shared: 7}
	for _, tc := range []struct {
		name      string
		now       time.Time
		stats     []core.NewsSourceStats
		review    bool
		reactions bool
	}{
		{name: "a trial week ends", now: monday, stats: []core.NewsSourceStats{bbc, trial}, review: true, reactions: true},
		{name: "sunday", now: monday.Add(-24 * time.Hour), stats: []core.NewsSourceStats{bbc}, review: true},
		{name: "another day", now: monday, stats: []core.NewsSourceStats{bbc}, review: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, 20)
			h.settings.Night = core.DailyTime{Enabled: true, At: 0}
			h.now = tc.now
			h.news.stats = tc.stats
			if tc.reactions {
				h.section(t, "国际与财经", 1, "1. 大国关系")
				h.news.reactions = []core.StoryReaction{
					{StoryID: 9, Reaction: core.NewsReactionLess, Section: "国际与财经", Title: "印尼央行降息", At: tc.now.Add(-time.Hour)},
					{StoryID: 8, Reaction: core.NewsReactionLess, Section: "国际与财经", Title: "菲律宾央行加息", At: tc.now.Add(-24 * time.Hour)},
					{StoryID: 7, Reaction: core.NewsReactionMore, Section: "热榜", Title: "高铁票改签新规", At: tc.now.Add(-48 * time.Hour)},
				}
			}
			if err := h.sched.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !tc.review {
				if len(h.runs.starts) != 0 {
					t.Fatalf("a night without material started: %v", h.runs.starts)
				}
				return
			}
			if len(h.runs.starts) != 1 {
				t.Fatalf("starts = %v", h.runs.starts)
			}
			night := h.runs.starts[0]
			for _, want := range []string{
				"News sources over the last 7 days.",
				"- news source #3 BBC 中文 [国际与财经] in the briefing: 40 items (5.7 a day), average score 3.0, 12 scored 4 or 5, 50% share a story with another source; briefings picked 6 stories with its items, 2 of them in today's focus; the owner marked 1 more and 3 less of their kind\n",
			} {
				if !strings.Contains(night, want) {
					t.Fatalf("night input lacks %q:\n%s", want, night)
				}
			}
			reactions := "propose rewriting the section's preference with news_sections set"
			if tc.reactions {
				for _, want := range []string{
					reactions,
					"\n## 国际与财经\nPreference:\n1. 大国关系\n- less of this kind: 印尼央行降息 (10-05)\n- less of this kind: 菲律宾央行加息 (10-04)\n## 热榜\n- more of this kind: 高铁票改签新规 (10-03)\n",
				} {
					if !strings.Contains(night, want) {
						t.Fatalf("night input lacks %q:\n%s", want, night)
					}
				}
				if len(h.news.reactionsSince) != 1 || !h.news.reactionsSince[0].Equal(tc.now.Add(-7*24*time.Hour)) {
					t.Fatalf("reactions since %v", h.news.reactionsSince)
				}
			} else if strings.Contains(night, reactions) {
				t.Fatalf("a week without reactions asks for preferences:\n%s", night)
			}
			if len(tc.stats) == 2 && !strings.Contains(night, "- news source #12 知乎热榜 [热榜] on trial since 09-28: 70 items (10.0 a day), average score 2.0, 3 scored 4 or 5, 10% share a story") {
				t.Fatalf("night input lacks the trial source:\n%s", night)
			}
			if len(h.news.since) != 1 || !h.news.since[0].Equal(tc.now.Add(-7*24*time.Hour)) {
				t.Fatalf("stats since %v", h.news.since)
			}
		})
	}
}
