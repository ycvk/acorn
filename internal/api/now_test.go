package api

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ycvk/acorn/internal/core"
)

var nowTestTime = time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC)

type nowStoreFake struct {
	commitments []core.Commitment
	occurrences []core.CommitmentOccurrence
	concerns    []core.MemoryConcern
	watches     []core.Watch
	settled     []core.CommitmentSettlement
	settleErr   error
	briefing    core.RoutineOutput
	routine     string
	panels      []core.Panel
	stories     map[string][]core.BriefingStory
	reactions   map[int64]core.StoryReaction
	goals       []core.Goal
	followUps   []core.FollowUp
	sessions    map[string]string
	ended       []core.Goal
	endErr      error
	cancelled   []int64
	cancelErr   error
}

func (f *nowStoreFake) ActiveGoals(context.Context) ([]core.Goal, error) {
	return f.goals, nil
}

func (f *nowStoreFake) EndGoal(_ context.Context, id int64, state, result, endRunID string, now time.Time) (core.Goal, error) {
	g := core.Goal{ID: id, State: state, Result: result, EndRunID: endRunID, UpdatedAt: now}
	f.ended = append(f.ended, g)
	return g, f.endErr
}

func (f *nowStoreFake) WaitingFollowUps(_ context.Context, sessionID string) ([]core.FollowUp, error) {
	if sessionID != "" {
		return nil, fmt.Errorf("the now page lists the follow-ups of every conversation, got %q", sessionID)
	}
	return f.followUps, nil
}

func (f *nowStoreFake) CancelFollowUp(_ context.Context, sessionID string, id int64, _ time.Time) error {
	if sessionID != "" {
		return fmt.Errorf("the owner cancels a follow-up of any conversation, got %q", sessionID)
	}
	f.cancelled = append(f.cancelled, id)
	return f.cancelErr
}

func (f *nowStoreFake) LoadSession(_ context.Context, sessionID string) (*core.SessionRecord, error) {
	title, ok := f.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", core.ErrSessionNotFound, sessionID)
	}
	return &core.SessionRecord{SessionID: sessionID, Title: title}, nil
}

func (f *nowStoreFake) BriefingStories(_ context.Context, runID string) ([]core.BriefingStory, error) {
	return f.stories[runID], nil
}

func (f *nowStoreFake) ReactToNewsStory(_ context.Context, storyID int64, reaction core.NewsReaction, deviceID string, at time.Time) error {
	if storyID != 41 {
		return fmt.Errorf("%w: %d", core.ErrNewsStoryNotFound, storyID)
	}
	f.reactions[storyID] = core.StoryReaction{StoryID: storyID, Reaction: reaction, DeviceID: deviceID, At: at}
	return nil
}

func (f *nowStoreFake) ClearNewsReaction(_ context.Context, storyID int64) error {
	delete(f.reactions, storyID)
	return nil
}

func (f *nowStoreFake) Panels(context.Context) ([]core.Panel, error) {
	return f.panels, nil
}

func (f *nowStoreFake) ListCommitments(context.Context, bool) ([]core.Commitment, error) {
	return f.commitments, nil
}

func (f *nowStoreFake) ListDueOccurrences(context.Context) ([]core.CommitmentOccurrence, error) {
	return f.occurrences, nil
}

func (f *nowStoreFake) SettleCommitment(_ context.Context, change core.CommitmentSettlement) error {
	f.settled = append(f.settled, change)
	return f.settleErr
}

func (f *nowStoreFake) ListConcerns(context.Context, bool) ([]core.MemoryConcern, error) {
	return f.concerns, nil
}

func (f *nowStoreFake) ListWatches(context.Context) ([]core.Watch, error) {
	return f.watches, nil
}

func (f *nowStoreFake) LoadWatch(_ context.Context, id int64) (*core.Watch, error) {
	for _, w := range f.watches {
		if w.ID == id {
			return &w, nil
		}
	}
	return nil, fmt.Errorf("%w: %d", core.ErrWatchNotFound, id)
}

func (f *nowStoreFake) UpdateWatch(_ context.Context, w core.Watch) error {
	for i := range f.watches {
		if f.watches[i].ID == w.ID {
			f.watches[i] = w
			return nil
		}
	}
	return fmt.Errorf("%w: %d", core.ErrWatchNotFound, w.ID)
}

func (f *nowStoreFake) LatestRoutineOutput(_ context.Context, routine string) (core.RoutineOutput, error) {
	f.routine = routine
	return f.briefing, nil
}

func newNowRouter(t *testing.T, store *nowStoreFake) http.Handler {
	t.Helper()
	service, err := NewNowService(store, func() time.Time { return nowTestTime })
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		deviceAuth: newDeviceAuthTestService(&deviceAuthHandlerStub{}),
		now:        service,
		logger:     slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	}
	router := chi.NewRouter()
	server.registerRoutes(router)
	return router
}

func TestNowListsBriefingCommitmentsConcernsAndFailingWatchesFirst(t *testing.T) {
	store := &nowStoreFake{
		commitments: []core.Commitment{
			{ID: 1, Content: "看 X", State: "due", SessionID: "thread_a", WakeAt: nowTestTime.Add(-time.Hour)},
			{ID: 2, Content: "周报", State: "scheduled", SessionID: "thread_b", WakeAt: nowTestTime.Add(time.Hour), Recurrence: "0 9 * * 1"},
		},
		occurrences: []core.CommitmentOccurrence{{ID: 9, CommitmentID: 1, DueAt: nowTestTime.Add(-time.Hour), State: "due", RunID: "run_x"}},
		concerns: []core.MemoryConcern{
			{ID: "concern_1", Title: "准备 Go 面试", State: "waiting", Reason: "等面试时间", ThreadID: "thread_goals", UpdatedAt: nowTestTime},
			{ID: "concern_2", Title: "整理 tokio 变更", Outcome: "一份 Markdown，列出 v1.4 到 v1.7 的不兼容变更", State: "active", Reason: "读完了 v1.5", ThreadID: "thread_tokio", ReviewAt: nowTestTime.Add(time.Hour), UpdatedAt: nowTestTime},
		},
		watches: []core.Watch{
			{ID: 1, Name: "tokio", Kind: core.WatchGitHub, Mode: core.WatchModeDigest, Status: core.WatchActive, Interval: time.Hour, NextCheckAt: nowTestTime},
			{ID: 2, Name: "price", Kind: core.WatchWeb, Mode: core.WatchModeImmediate, Status: core.WatchFailing, Interval: 30 * time.Minute, NextCheckAt: nowTestTime, LastCheckedAt: nowTestTime, LastError: "selector matched nothing", Failures: 3},
			{ID: 3, Name: "AI225", Kind: core.WatchRSS, Mode: core.WatchModeNews, Section: "AI 与科技前沿", Issue: true, Trial: true, Status: core.WatchActive, Interval: time.Hour, NextCheckAt: nowTestTime},
		},
		briefing: core.RoutineOutput{Slot: "2026-10-11", ThreadID: "thread_briefings", RunID: "run_briefing", Text: "## 今天\n交房租", FinishedAt: nowTestTime},
		stories: map[string][]core.BriefingStory{"run_briefing": {
			{StoryID: 41, Section: "国际与财经", Title: "菲律宾央行加息", Summary: "加息 25 个基点。", URL: "https://news.test/ph", Sources: []int64{3}, Reaction: core.NewsReactionLess},
			{StoryID: 42, Section: "国际与财经", Title: "普京提议中美俄三方会谈", URL: "https://news.test/talks", Focus: true, Sources: []int64{3, 4}},
		}},
		panels: []core.Panel{{ThreadTitle: "仓库版本", View: core.ThreadView{ID: "view_1", SessionID: "thread_repos", Title: "eino 最新版本", Catalog: "acorn.views.v1",
			Components: []byte(`[{"id":"root","component":"Text","text":{"path":"/latest"}}]`), Data: []byte(`{"latest":"v0.9.22"}`), Source: "repo-releases/check",
			CreatedAt: nowTestTime.Add(-2 * time.Hour), UpdatedAt: nowTestTime.Add(-time.Minute), PinnedAt: nowTestTime.Add(-time.Hour)}}},
	}
	router := newNowRouter(t, store)

	rec := performClientRequest(router, http.MethodGet, "/v1/now", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got NowResponse
	decodeClientTestJSON(t, rec, &got)
	want := NowBriefingDTO{ThreadID: "thread_briefings", RunID: "run_briefing", Date: "2026-10-11", Body: "## 今天\n交房租", FinishedAt: nowTestTime, Stories: []NowBriefingStoryDTO{
		{StoryID: 41, Section: "国际与财经", Title: "菲律宾央行加息", Summary: "加息 25 个基点。", URL: "https://news.test/ph", Reaction: "less"},
		{StoryID: 42, Section: "国际与财经", Title: "普京提议中美俄三方会谈", URL: "https://news.test/talks", Focus: true},
	}, Parts: []NowBriefingPartDTO{{Heading: "今天", Body: "交房租"}}}
	if store.routine != "briefing" || got.Briefing == nil || !reflect.DeepEqual(*got.Briefing, want) {
		t.Fatalf("briefing = %+v from routine %q", got.Briefing, store.routine)
	}
	if len(got.Panels) != 1 || got.Panels[0].ThreadID != "thread_repos" || got.Panels[0].ThreadTitle != "仓库版本" || !got.Panels[0].UpdatedAt.Equal(nowTestTime.Add(-time.Minute)) ||
		got.Panels[0].View.Source != "repo-releases/check" || got.Panels[0].View.PinnedAt == nil || string(got.Panels[0].View.Data) != `{"latest":"v0.9.22"}` {
		t.Fatalf("panels = %+v", got.Panels)
	}
	if len(got.Commitments) != 2 || len(got.Commitments[0].Occurrences) != 1 || got.Commitments[0].Occurrences[0].RunID != "run_x" {
		t.Fatalf("commitments = %+v", got.Commitments)
	}
	if got.Commitments[1].Recurrence != "0 9 * * 1" {
		t.Fatalf("recurring commitment = %+v", got.Commitments[1])
	}
	wantConcerns := []NowConcernDTO{
		{ID: "concern_1", ThreadID: "thread_goals", Title: "准备 Go 面试", State: "waiting", Reason: "等面试时间", UpdatedAt: nowTestTime},
		{ID: "concern_2", ThreadID: "thread_tokio", Title: "整理 tokio 变更", Outcome: "一份 Markdown，列出 v1.4 到 v1.7 的不兼容变更", State: "active", Reason: "读完了 v1.5", ReviewAt: optionalUTC(nowTestTime.Add(time.Hour)), UpdatedAt: nowTestTime},
	}
	if !reflect.DeepEqual(got.Concerns, wantConcerns) {
		t.Fatalf("concerns = %+v, want %+v", got.Concerns, wantConcerns)
	}
	if strings.Count(rec.Body.String(), `"outcome"`) != 1 {
		t.Fatalf("a concern without an outcome must omit it: %s", rec.Body.String())
	}
	if len(got.Watches) != 3 || got.Watches[0].ID != 2 || got.Watches[0].IntervalSeconds != 1800 || got.Watches[1].LastCheckedAt != nil {
		t.Fatalf("watches = %+v", got.Watches)
	}
	if news := got.Watches[2]; news.Mode != "news" || news.Section != "AI 与科技前沿" || !news.Issue || !news.Trial || got.Watches[1].Section != "" || got.Watches[1].Trial {
		t.Fatalf("news watch = %+v", news)
	}
	if !strings.Contains(rec.Body.String(), `"occurrences":[]`) {
		t.Fatalf("a commitment without occurrences must carry an empty list: %s", rec.Body.String())
	}
}

func TestNowWithoutBriefingOmitsIt(t *testing.T) {
	router := newNowRouter(t, &nowStoreFake{})
	rec := performClientRequest(router, http.MethodGet, "/v1/now", "")
	if rec.Code != http.StatusOK || rec.Body.String() != `{"panels":[],"commitments":[],"concerns":[],"goals":[],"follow_ups":[],"watches":[]}` {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestNowFailsOnAnOccurrenceWithoutActiveCommitment(t *testing.T) {
	store := &nowStoreFake{occurrences: []core.CommitmentOccurrence{{ID: 9, CommitmentID: 4, State: "due"}}}
	rec := performClientRequest(newNowRouter(t, store), http.MethodGet, "/v1/now", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCancelCommitmentSettlesAsCancel(t *testing.T) {
	store := &nowStoreFake{}
	router := newNowRouter(t, store)
	rec := performClientRequest(router, http.MethodPost, "/v1/commitments/7:cancel", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	want := core.CommitmentSettlement{CommitmentID: 7, Action: "cancel", Now: nowTestTime}
	if len(store.settled) != 1 || store.settled[0] != want {
		t.Fatalf("settled = %+v", store.settled)
	}

	for name, tc := range map[string]struct {
		err    error
		status int
		code   string
	}{
		"missing": {core.ErrCommitmentNotFound, http.StatusNotFound, "commitment_not_found"},
		"ended":   {fmt.Errorf("%w: commitment 7 is cancelled", core.ErrCommitmentEnded), http.StatusConflict, "commitment_ended"},
	} {
		store.settleErr = tc.err
		rec := performClientRequest(router, http.MethodPost, "/v1/commitments/7:cancel", "")
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.code) {
			t.Errorf("%s: status = %d body=%s", name, rec.Code, rec.Body.String())
		}
	}
	if rec := performClientRequest(router, http.MethodPost, "/v1/commitments/abc:cancel", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("bad id status = %d", rec.Code)
	}
	if rec := performClientRequestWithoutAuth(router, http.MethodPost, "/v1/commitments/7:cancel", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d", rec.Code)
	}
}

// The now page lists active goals and waiting follow-ups with their
// conversations' titles, and says what each follow-up waits for.
func TestNowListsGoalsAndFollowUps(t *testing.T) {
	store := &nowStoreFake{
		goals: []core.Goal{{ID: 3, SessionID: "thread_books", Objective: "下完整本书并整理进知识库", MaxRuns: 10, Runs: 4, State: core.GoalActive, CreatedAt: nowTestTime}},
		followUps: []core.FollowUp{
			{ID: 7, SessionID: "thread_books", Task: "文件到了就读", Path: "books/go.epub", DeadlineAt: nowTestTime.Add(time.Hour), CreatedAt: nowTestTime},
			{ID: 8, SessionID: "thread_build", Task: "编译完看结果", JobID: "job-00000001", DeadlineAt: nowTestTime.Add(2 * time.Hour), CreatedAt: nowTestTime},
			{ID: 9, SessionID: "thread_build", Task: "下载到一半就检查", JobID: "job-00000002", OutputContains: "50%", DeadlineAt: nowTestTime.Add(3 * time.Hour), CreatedAt: nowTestTime},
		},
		sessions: map[string]string{"thread_books": "整理 Go 书", "thread_build": "编译 acorn"},
	}
	rec := performClientRequest(newNowRouter(t, store), http.MethodGet, "/v1/now", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got NowResponse
	decodeClientTestJSON(t, rec, &got)
	wantGoals := []NowGoalDTO{{ID: 3, ThreadID: "thread_books", ThreadTitle: "整理 Go 书", Objective: "下完整本书并整理进知识库", Runs: 4, MaxRuns: 10, CreatedAt: nowTestTime}}
	if !reflect.DeepEqual(got.Goals, wantGoals) {
		t.Fatalf("goals = %+v, want %+v", got.Goals, wantGoals)
	}
	wantFollowUps := []NowFollowUpDTO{
		{ID: 7, ThreadID: "thread_books", ThreadTitle: "整理 Go 书", Task: "文件到了就读", WaitsFor: "path", Path: "books/go.epub", DeadlineAt: nowTestTime.Add(time.Hour), CreatedAt: nowTestTime},
		{ID: 8, ThreadID: "thread_build", ThreadTitle: "编译 acorn", Task: "编译完看结果", WaitsFor: "job_end", JobID: "job-00000001", DeadlineAt: nowTestTime.Add(2 * time.Hour), CreatedAt: nowTestTime},
		{ID: 9, ThreadID: "thread_build", ThreadTitle: "编译 acorn", Task: "下载到一半就检查", WaitsFor: "job_output", JobID: "job-00000002", OutputContains: "50%", DeadlineAt: nowTestTime.Add(3 * time.Hour), CreatedAt: nowTestTime},
	}
	if !reflect.DeepEqual(got.FollowUps, wantFollowUps) {
		t.Fatalf("follow-ups = %+v, want %+v", got.FollowUps, wantFollowUps)
	}

	store.sessions = map[string]string{"thread_books": "整理 Go 书"}
	if rec := performClientRequest(newNowRouter(t, store), http.MethodGet, "/v1/now", ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("a follow-up whose conversation cannot be read must fail the page: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

// The owner stops a goal as stopped, with no run of its own, and cancels a
// waiting follow-up of any conversation.
func TestStopGoalAndCancelFollowUp(t *testing.T) {
	store := &nowStoreFake{}
	router := newNowRouter(t, store)
	if rec := performClientRequest(router, http.MethodPost, "/v1/goals/3:stop", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("stop status = %d body=%s", rec.Code, rec.Body.String())
	}
	want := core.Goal{ID: 3, State: core.GoalStopped, Result: "the owner stopped it", UpdatedAt: nowTestTime}
	if len(store.ended) != 1 || !reflect.DeepEqual(store.ended[0], want) {
		t.Fatalf("ended = %+v", store.ended)
	}
	if rec := performClientRequest(router, http.MethodPost, "/v1/follow-ups/7:cancel", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("cancel status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !reflect.DeepEqual(store.cancelled, []int64{7}) {
		t.Fatalf("cancelled = %v", store.cancelled)
	}

	store.endErr, store.cancelErr = fmt.Errorf("%w: #3", core.ErrGoalNotFound), fmt.Errorf("%w: #7", core.ErrFollowUpNotFound)
	for path, code := range map[string]string{"/v1/goals/3:stop": "goal_not_found", "/v1/follow-ups/7:cancel": "follow_up_not_found"} {
		if rec := performClientRequest(router, http.MethodPost, path, ""); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), code) {
			t.Errorf("%s: status = %d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	store.endErr, store.cancelErr = fmt.Errorf("%w: #3 is done", core.ErrGoalEnded), fmt.Errorf("%w: #7 is fired", core.ErrFollowUpNotWaiting)
	for path, code := range map[string]string{"/v1/goals/3:stop": "goal_ended", "/v1/follow-ups/7:cancel": "follow_up_not_waiting"} {
		if rec := performClientRequest(router, http.MethodPost, path, ""); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), code) {
			t.Errorf("%s: status = %d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	for _, path := range []string{"/v1/goals/x:stop", "/v1/follow-ups/0:cancel"} {
		if rec := performClientRequest(router, http.MethodPost, path, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d", path, rec.Code)
		}
	}
	if rec := performClientRequestWithoutAuth(router, http.MethodPost, "/v1/goals/3:stop", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d", rec.Code)
	}
}

func TestPauseAndResumeWatch(t *testing.T) {
	store := &nowStoreFake{watches: []core.Watch{{ID: 3, Name: "price", Status: core.WatchFailing, Failures: 4, LastError: "timeout", NextCheckAt: nowTestTime.Add(time.Hour)}}}
	router := newNowRouter(t, store)

	if rec := performClientRequest(router, http.MethodPost, "/v1/watches/3:pause", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("pause status = %d body=%s", rec.Code, rec.Body.String())
	}
	if w := store.watches[0]; w.Status != core.WatchPaused || w.UpdatedAt != nowTestTime || w.Failures != 4 {
		t.Fatalf("paused watch = %+v", w)
	}
	if rec := performClientRequest(router, http.MethodPost, "/v1/watches/3:resume", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("resume status = %d body=%s", rec.Code, rec.Body.String())
	}
	if w := store.watches[0]; w.Status != core.WatchActive || w.Failures != 0 || w.LastError != "" || w.NextCheckAt != nowTestTime {
		t.Fatalf("resumed watch = %+v", w)
	}
	rec := performClientRequest(router, http.MethodPost, "/v1/watches/8:pause", "")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "watch_not_found") {
		t.Fatalf("missing watch status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestReactToBriefingStory(t *testing.T) {
	store := &nowStoreFake{reactions: map[int64]core.StoryReaction{}}
	router := newNowRouter(t, store)

	if rec := performClientRequest(router, http.MethodPut, "/v1/news/stories/41/reaction", `{"reaction":"less"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("react status = %d body=%s", rec.Code, rec.Body.String())
	}
	want := core.StoryReaction{StoryID: 41, Reaction: core.NewsReactionLess, DeviceID: "device_test", At: nowTestTime}
	if got := store.reactions[41]; !reflect.DeepEqual(got, want) {
		t.Fatalf("reaction = %+v", got)
	}
	if rec := performClientRequest(router, http.MethodPut, "/v1/news/stories/41/reaction", `{"reaction":"more"}`); rec.Code != http.StatusNoContent || store.reactions[41].Reaction != core.NewsReactionMore {
		t.Fatalf("changed reaction status = %d, reaction %+v", rec.Code, store.reactions[41])
	}
	if rec := performClientRequest(router, http.MethodDelete, "/v1/news/stories/41/reaction", ""); rec.Code != http.StatusNoContent || len(store.reactions) != 0 {
		t.Fatalf("clear status = %d, reactions %+v", rec.Code, store.reactions)
	}
	for _, tc := range []struct {
		path, body string
		status     int
		code       string
	}{
		{"/v1/news/stories/41/reaction", `{"reaction":"like"}`, http.StatusBadRequest, "reaction must be more or less"},
		{"/v1/news/stories/41/reaction", `{}`, http.StatusBadRequest, "reaction must be more or less"},
		{"/v1/news/stories/x/reaction", `{"reaction":"more"}`, http.StatusBadRequest, "story_id must be a positive integer"},
		{"/v1/news/stories/7/reaction", `{"reaction":"more"}`, http.StatusNotFound, "story_not_found"},
	} {
		rec := performClientRequest(router, http.MethodPut, tc.path, tc.body)
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.code) {
			t.Errorf("%s %s: status = %d body=%s", tc.path, tc.body, rec.Code, rec.Body.String())
		}
	}
	if rec := performClientRequestWithoutAuth(router, http.MethodPut, "/v1/news/stories/41/reaction", `{"reaction":"more"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d", rec.Code)
	}
}
