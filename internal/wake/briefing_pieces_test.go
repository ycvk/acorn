package wake

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// focusBriefing is a briefing of 2026-10-05 that finished at finished and
// picked three stories, the first and the third as today's focus.
func (h *harness) focusBriefing(t *testing.T, finished time.Time) {
	t.Helper()
	h.section(t, "国际", 1, "")
	body := strings.Join([]string{
		"## 国际",
		"1. **停火谈判**：开始了。[BBC](https://bbc.test/a)",
		"2. **央行加息**：25 个基点。[FT](https://ft.test/b)",
		"3. **港口罢工**：第三天。[BBC](https://bbc.test/c)",
		"## 今日关注",
		"- 第 1 条：谈判决定战事走向。",
		"- 第 3 条：罢工拖住出口。",
	}, "\n")
	h.news.unrecorded = []core.RoutineOutput{{Slot: "2026-10-05", ThreadID: "thread_briefings", RunID: "run_b", Text: body, FinishedAt: finished}}
	h.news.briefed = map[string][]core.NewsStory{"run_b": {
		story(1, "国际", "停火谈判", 5, finished, "", core.WatchItem{WatchID: 2, URL: "https://bbc.test/a"}),
		story(2, "国际", "央行加息", 4, finished, "", core.WatchItem{WatchID: 3, URL: "https://ft.test/b"}),
		story(3, "国际", "港口罢工", 4, finished, "", core.WatchItem{WatchID: 2, URL: "https://bbc.test/c"}),
	}}
}

// A briefing that starts early has its focus written about at once, and is
// pushed once when the owner's time has come and no piece is being written.
func TestABriefingIsPushedOnceItsPiecesEndAndItsTimeHasCome(t *testing.T) {
	h := newHarnessWith(t, 20, core.DailyTime{At: 9 * time.Hour})
	h.at(8, 56)
	h.focusBriefing(t, h.now.Add(-30*time.Second))
	notifier := &fakeNotifier{}
	h.sched.WithNotifications(notifier)

	h.tick(t)
	if want := []string{"1|2026-10-05", "3|2026-10-05"}; !reflect.DeepEqual(h.runs.pieces, want) {
		t.Fatalf("pieces started = %v, want the focus", h.runs.pieces)
	}
	h.news.pieces = map[int64]*core.BriefingPiece{1: {Status: core.BriefingPieceReady}, 3: {Status: core.BriefingPieceReady}}
	h.tick(t)
	if len(notifier.pushed) != 0 {
		t.Fatalf("pushed before 09:00: %+v", notifier.pushed)
	}

	h.at(9, 0)
	h.news.pieces[3] = &core.BriefingPiece{Status: core.BriefingPieceWriting}
	h.tick(t)
	if len(notifier.pushed) != 0 {
		t.Fatalf("pushed while a piece is being written: %+v", notifier.pushed)
	}
	h.news.pieces[3] = &core.BriefingPiece{Status: core.BriefingPieceFailed}
	for range 2 {
		h.tick(t)
	}
	if want := []core.Notification{{Title: "早安", Body: "停火谈判\n港口罢工", ThreadID: "thread_briefings", RunID: "run_b"}}; !reflect.DeepEqual(notifier.pushed, want) {
		t.Fatalf("pushed = %+v, want the focus once", notifier.pushed)
	}
	if len(h.runs.pieces) != 2 {
		t.Fatalf("pieces were started again: %v", h.runs.pieces)
	}
}

// A piece that takes longer than pieceWait does not hold the push back.
func TestABriefingIsPushedWhenAPieceTakesTooLong(t *testing.T) {
	h := newHarnessWith(t, 20, core.DailyTime{At: 9 * time.Hour})
	h.at(9, 2)
	finished := h.now
	h.focusBriefing(t, finished)
	h.news.pieces = map[int64]*core.BriefingPiece{1: {Status: core.BriefingPieceWriting}}
	notifier := &fakeNotifier{}
	h.sched.WithNotifications(notifier)

	h.tick(t)
	h.now = finished.Add(pieceWait - time.Second)
	h.tick(t)
	if len(notifier.pushed) != 0 {
		t.Fatalf("pushed before the wait was over: %+v", notifier.pushed)
	}
	h.now = finished.Add(pieceWait)
	h.tick(t)
	if len(notifier.pushed) != 1 {
		t.Fatalf("pushed = %+v, want one after the wait", notifier.pushed)
	}
}

// A story pruned since the briefing, or whose conversation the owner is in,
// goes without a piece; any other failure to start one is reported, and
// neither is tried again.
func TestAStoryWhosePieceCannotStartGoesWithout(t *testing.T) {
	h := newHarnessWith(t, 20, core.DailyTime{At: 9 * time.Hour})
	h.at(9, 2)
	h.focusBriefing(t, h.now)
	h.runs.pieceFail = map[int64]error{1: fmt.Errorf("%w: thread_story", core.ErrThreadBusy)}
	h.tick(t)
	if want := []string{"3|2026-10-05"}; !reflect.DeepEqual(h.runs.pieces, want) {
		t.Fatalf("pieces started = %v", h.runs.pieces)
	}

	other := newHarnessWith(t, 20, core.DailyTime{At: 9 * time.Hour})
	other.at(9, 2)
	other.focusBriefing(t, other.now)
	other.runs.pieceFail = map[int64]error{1: errors.New("execution not ready")}
	err := other.sched.Tick(t.Context())
	if err == nil || !strings.Contains(err.Error(), "pieces of briefing run_b: story 1: execution not ready") {
		t.Fatalf("tick error = %v", err)
	}
	other.runs.pieceFail = nil
	other.tick(t)
	if want := []string{"3|2026-10-05"}; !reflect.DeepEqual(other.runs.pieces, want) {
		t.Fatalf("pieces started = %v, want the failed one left alone", other.runs.pieces)
	}
}

// A briefing found long after it finished is pushed without pieces.
func TestAnOldBriefingGetsNoPieces(t *testing.T) {
	h := newHarnessWith(t, 20, core.DailyTime{At: 9 * time.Hour})
	h.at(22, 0)
	h.focusBriefing(t, h.now.Add(-pieceWindow-time.Minute))
	notifier := &fakeNotifier{}
	h.sched.WithNotifications(notifier)
	h.tick(t)
	if len(h.runs.pieces) != 0 || len(notifier.pushed) != 1 {
		t.Fatalf("pieces = %v, pushed = %+v", h.runs.pieces, notifier.pushed)
	}
}

// The briefing starts briefingLead before the owner's time.
func TestTheBriefingStartsBeforeTheOwnersTime(t *testing.T) {
	h := newHarnessWith(t, 20, core.DailyTime{Enabled: true, At: 9 * time.Hour})
	h.commit(t, "today", time.Date(2026, 10, 5, 18, 0, 0, 0, h.settings.Location), "")
	h.at(8, 54)
	h.tick(t)
	if len(h.runs.starts) != 0 {
		t.Fatalf("briefed at 08:54: %v", h.runs.starts)
	}
	h.at(8, 55)
	h.tick(t)
	if len(h.runs.starts) != 1 || !strings.Contains(h.runs.starts[0], "[briefing 2026-10-05]") {
		t.Fatalf("briefing at 08:55 = %v", h.runs.starts)
	}

	midnight := newHarnessWith(t, 20, core.DailyTime{Enabled: true, At: 2 * time.Minute})
	midnight.commit(t, "today", time.Date(2026, 10, 5, 18, 0, 0, 0, midnight.settings.Location), "")
	midnight.at(0, 0)
	midnight.tick(t)
	if len(midnight.runs.starts) != 1 || !strings.Contains(midnight.runs.starts[0], "[briefing 2026-10-05]") {
		t.Fatalf("a briefing set just after midnight starts on its own day: %v", midnight.runs.starts)
	}
}
