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
		},
		briefing: core.RoutineOutput{Slot: "2026-10-11", ThreadID: "thread_briefings", RunID: "run_briefing", Text: "## 今天\n交房租", FinishedAt: nowTestTime},
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
	want := NowBriefingDTO{ThreadID: "thread_briefings", RunID: "run_briefing", Date: "2026-10-11", Body: "## 今天\n交房租", FinishedAt: nowTestTime}
	if store.routine != "briefing" || got.Briefing == nil || *got.Briefing != want {
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
	if len(got.Watches) != 2 || got.Watches[0].ID != 2 || got.Watches[0].IntervalSeconds != 1800 || got.Watches[1].LastCheckedAt != nil {
		t.Fatalf("watches = %+v", got.Watches)
	}
	if !strings.Contains(rec.Body.String(), `"occurrences":[]`) {
		t.Fatalf("a commitment without occurrences must carry an empty list: %s", rec.Body.String())
	}
}

func TestNowWithoutBriefingOmitsIt(t *testing.T) {
	router := newNowRouter(t, &nowStoreFake{})
	rec := performClientRequest(router, http.MethodGet, "/v1/now", "")
	if rec.Code != http.StatusOK || rec.Body.String() != `{"panels":[],"commitments":[],"concerns":[],"watches":[]}` {
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
