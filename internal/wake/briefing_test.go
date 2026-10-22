package wake

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// fact records an owner fact at recorded, valid from validFrom when set.
func (h *harness) fact(t *testing.T, id, content string, recorded, validFrom time.Time) {
	t.Helper()
	ctx := context.Background()
	// The source is fresh so its extract job never counts as stuck.
	source, err := h.data.RegisterMemorySource(ctx, core.MemorySource{ID: "owner:" + id, Kind: "fixture", ObjectID: id, Version: "1", Speaker: "owner", Body: content, RecordedAt: h.now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	draft := core.MemoryDraft{Kind: "fact", Content: content, Evidence: []core.MemoryEvidence{{SourceID: source.ID, Quote: content, Relation: "supports"}}}
	if !validFrom.IsZero() {
		draft.ValidFrom = validFrom.Format(time.RFC3339)
	}
	if _, err := h.data.CommitMemory(ctx, core.MemoryMutation{Now: recorded, Changes: []core.MemoryChange{{Draft: draft, Reason: "owner said so"}}}); err != nil {
		t.Fatal(err)
	}
}

// previousBriefing records a briefing run at the given time, so the next
// briefing's window starts there.
func (h *harness) previousBriefing(t *testing.T, at time.Time) {
	t.Helper()
	ctx := context.Background()
	slot := at.In(h.settings.Location).Format("2006-01-02")
	if err := h.data.ClaimRoutine(ctx, "briefing", slot, at); err != nil {
		t.Fatal(err)
	}
	if err := h.data.SetRoutineRun(ctx, "briefing", slot, "thread_briefings", "run_previous"); err != nil {
		t.Fatal(err)
	}
}

func TestBriefingSkipsWithoutMaterialAndKeepsTheSlot(t *testing.T) {
	h := newHarnessWith(t, 20, core.DailyTime{Enabled: true, At: 0})
	ctx := context.Background()
	h.previousBriefing(t, h.now.Add(-20*time.Hour))
	// Facts that take effect later in the week are context, never material.
	h.fact(t, "later", "下周二切换数据库", h.now.Add(-21*time.Hour), h.now.Add(72*time.Hour))
	for range 2 {
		if err := h.sched.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(h.runs.starts) != 0 {
		t.Fatalf("an empty briefing started a run: %v", h.runs.starts)
	}
	if err := h.data.ClaimRoutine(ctx, "briefing", "2026-10-05", h.now); !errors.Is(err, core.ErrRoutineTaken) {
		t.Fatalf("skipped briefing released its day: %v", err)
	}
	if run, err := h.data.LatestRoutineRun(ctx, "briefing"); err != nil || run.Slot != "2026-10-04" {
		t.Fatalf("skipped briefing recorded a run: %+v %v", run, err)
	}
}

func TestBriefingCarriesCommitmentsMemoryNotesAndNight(t *testing.T) {
	h := newHarnessWith(t, 20, core.DailyTime{Enabled: true, At: 0})
	ctx := context.Background()
	h.previousBriefing(t, h.now.Add(-20*time.Hour))
	h.commit(t, "交房租", h.now.Add(6*time.Hour), "")
	h.commit(t, "下周的事", h.now.Add(72*time.Hour), "")
	h.fact(t, "coffee", "我喜欢不加糖的手冲咖啡", h.now.Add(-time.Hour), time.Time{})
	h.fact(t, "old", "我住在新加坡", h.now.Add(-21*time.Hour), time.Time{})
	h.fact(t, "switch", "Heron 切换日期定在周二凌晨", h.now.Add(-21*time.Hour), h.now.Add(72*time.Hour))
	if _, err := h.data.WriteKnowledgeNote(ctx, core.KnowledgeWrite{Path: "reading/sectors.md", Title: "板块统计", Body: "……", At: h.now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := h.data.ClaimRoutine(ctx, "night", "2026-10-05", h.now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := h.data.CreateRun(ctx, core.RunCreateParams{RunID: "run_night", Input: "[night 2026-10-05]"}); err != nil {
		t.Fatal(err)
	}
	if err := h.data.FinishRun(ctx, "run_night", core.RunStatusSucceeded, "复核了咖啡偏好，没有矛盾。", ""); err != nil {
		t.Fatal(err)
	}
	if err := h.data.SetRoutineRun(ctx, "night", "2026-10-05", "thread_thoughts", "run_night"); err != nil {
		t.Fatal(err)
	}
	if err := h.sched.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(h.runs.starts) != 1 {
		t.Fatalf("starts = %v", h.runs.starts)
	}
	input := h.runs.starts[0]
	for _, want := range []string{
		"Commitments today and occurrences waiting for an outcome:\n- #1 [15:00] 交房租\n",
		"Remembered since the last briefing:\n- fact ",
		"我喜欢不加糖的手冲咖啡",
		"Taking effect within a week:\n- 2026-10-08 fact ",
		"Last night's reflection (2026-10-05):\n复核了咖啡偏好，没有矛盾。\n",
		"Notes written since the last briefing:\n- reading/sectors.md — 板块统计\n",
	} {
		if !strings.Contains(input, want) {
			t.Fatalf("briefing input lacks %q:\n%s", want, input)
		}
	}
	for _, unwanted := range []string{"下周的事", "我住在新加坡", "Watch changes", "No watch changes"} {
		if strings.Contains(input, unwanted) {
			t.Fatalf("briefing input carries %q:\n%s", unwanted, input)
		}
	}
	sources := h.runs.sources[0]
	if !strings.Contains(strings.Join(sources, ","), "owner:coffee") || !strings.Contains(strings.Join(sources, ","), "owner:switch") || !strings.Contains(strings.Join(sources, ","), core.KnowledgeSourceID("reading/sectors.md", 1)) {
		t.Fatalf("briefing sources = %v", sources)
	}
}

func TestBriefingFactTakingEffectTodayIsMaterial(t *testing.T) {
	h := newHarnessWith(t, 20, core.DailyTime{Enabled: true, At: 0})
	h.previousBriefing(t, h.now.Add(-20*time.Hour))
	// Valid since 08:00 today, an hour before the briefing.
	h.fact(t, "today", "今天开始每天跑步", h.now.Add(-21*time.Hour), h.now.Add(-time.Hour))
	h.fact(t, "fresh", "我今天早上记下的事", h.now.Add(-30*time.Minute), time.Time{})
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.runs.starts) != 1 || !strings.Contains(h.runs.starts[0], "Taking effect within a week:\n- 2026-10-05 fact ") {
		t.Fatalf("starts = %v", h.runs.starts)
	}
	if strings.Count(h.runs.starts[0], "我今天早上记下的事") != 1 || !strings.Contains(h.runs.starts[0], "Remembered since the last briefing:\n- fact ") {
		t.Fatalf("a fact recorded today without a validity start is remembered, never upcoming:\n%s", h.runs.starts[0])
	}
}

func TestBriefingIgnoresFailedOrSilentNight(t *testing.T) {
	h := newHarnessWith(t, 20, core.DailyTime{Enabled: true, At: 0})
	ctx := context.Background()
	if err := h.data.ClaimRoutine(ctx, "night", "2026-10-05", h.now.Add(-5*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := h.data.CreateRun(ctx, core.RunCreateParams{RunID: "run_night", Input: "[night 2026-10-05]"}); err != nil {
		t.Fatal(err)
	}
	if err := h.data.FinishRun(ctx, "run_night", core.RunStatusFailed, "", "model unavailable"); err != nil {
		t.Fatal(err)
	}
	if err := h.data.SetRoutineRun(ctx, "night", "2026-10-05", "thread_thoughts", "run_night"); err != nil {
		t.Fatal(err)
	}
	if err := h.sched.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(h.runs.starts) != 0 {
		t.Fatalf("a failed night briefed: %v", h.runs.starts)
	}
}

func TestTicksReadTheBriefingTimeTheOwnerSavedLast(t *testing.T) {
	h := newHarnessWith(t, 20, core.DailyTime{Enabled: true, At: 9 * time.Hour})
	ctx := context.Background()
	h.at(8, 30)
	h.tick(t)
	if err := h.data.ClaimRoutine(ctx, "briefing", "2026-10-05", h.now); err != nil {
		t.Fatalf("the briefing ran before 09:00: %v", err)
	}
	if err := h.data.ReleaseRoutine(ctx, "briefing", "2026-10-05"); err != nil {
		t.Fatal(err)
	}
	h.settings.Briefing.At = 8 * time.Hour
	h.tick(t)
	if err := h.data.ClaimRoutine(ctx, "briefing", "2026-10-05", h.now); !errors.Is(err, core.ErrRoutineTaken) {
		t.Fatalf("moving the briefing to 08:00 at 08:30 runs it on the next tick: %v", err)
	}
}
