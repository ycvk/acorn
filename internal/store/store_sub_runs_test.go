package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func startSubRun(t *testing.T, s *Store, start core.SubRunStart) core.SubRun {
	t.Helper()
	if start.Limit == 0 {
		start.Limit = 3
	}
	if start.CreatedAt.IsZero() {
		start.CreatedAt = time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC)
	}
	if start.Input == "" {
		start.Input = "[brief]\nObjective: " + start.Brief.Objective
	}
	sub, err := s.StartSubRun(context.Background(), start)
	if err != nil {
		t.Fatalf("start sub-run %+v: %v", start, err)
	}
	return sub
}

func endRun(t *testing.T, s *Store, runID string, status core.RunStatus) {
	t.Helper()
	event := "run.completed"
	if status == core.RunStatusFailed {
		event = "run.failed"
	}
	if _, _, err := s.FinishRun(context.Background(), core.RunEnd{RunID: runID, Status: status, Output: "done", Event: event}); err != nil {
		t.Fatal(err)
	}
}

// A run delegates tasks in its conversation and turn, up to its limit. Each
// attempt reports once to the parent; a reported attempt is accepted or sent
// back, and a redo is the task's next attempt. A sub-run cannot delegate.
func TestSubRunsReportOnceAndAreReviewed(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.CreateSession(ctx, "chat", "读文章"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRun(ctx, core.RunCreateParams{RunID: "parent", SessionID: "chat", TurnIndex: 2, Input: "读这两篇"}); err != nil {
		t.Fatal(err)
	}
	first := startSubRun(t, s, core.SubRunStart{RunID: "sub_1", ParentRunID: "parent", Brief: core.SubRunBrief{Objective: "读第一篇", Acceptance: []string{"三条要点"}}})
	second := startSubRun(t, s, core.SubRunStart{RunID: "sub_2", ParentRunID: "parent", Brief: core.SubRunBrief{Objective: "读第二篇"}})
	if first.Task != 1 || first.Attempt != 1 || second.Task != 2 || second.Attempt != 1 {
		t.Fatalf("tasks = %+v, %+v", first, second)
	}
	run, err := s.LoadRun(ctx, "sub_1")
	if err != nil || run.SessionID != "chat" || run.TurnIndex != 2 || run.Status != core.RunStatusRunning || run.Input != "[brief]\nObjective: 读第一篇" {
		t.Fatalf("sub-run's run = %+v, %v", run, err)
	}
	if _, err := s.StartSubRun(ctx, core.SubRunStart{RunID: "sub_x", ParentRunID: "sub_1", Brief: core.SubRunBrief{Objective: "再派"}, Input: "x", Limit: 3, CreatedAt: time.Now()}); err == nil {
		t.Fatal("a sub-run delegated")
	}
	if _, err := s.AcceptSubRun(ctx, "parent", 1); !errors.Is(err, core.ErrSubRunRunning) {
		t.Fatalf("accept a running attempt = %v", err)
	}
	if reports, err := s.TakeSubRunReports(ctx, "parent"); err != nil || len(reports) != 0 {
		t.Fatalf("reports before any ended = %+v, %v", reports, err)
	}

	if err := s.SubmitSubRun(ctx, "sub_1", core.SubRunSubmission{Summary: "初稿"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SubmitSubRun(ctx, "sub_1", core.SubRunSubmission{Summary: "三条要点", Evidence: []string{"读了全文"}, Limitations: []string{}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SubmitSubRun(ctx, "parent", core.SubRunSubmission{Summary: "x"}); !errors.Is(err, core.ErrSubRunNotFound) {
		t.Fatalf("submit from the parent = %v", err)
	}
	endRun(t, s, "sub_1", core.RunStatusSucceeded)
	if err := s.SubmitSubRun(ctx, "sub_1", core.SubRunSubmission{Summary: "晚了"}); err == nil {
		t.Fatal("an ended sub-run submitted")
	}
	reports, err := s.TakeSubRunReports(ctx, "parent")
	if err != nil || len(reports) != 1 || reports[0].RunID != "sub_1" || !reports[0].Delivered || reports[0].Submission == nil || reports[0].Submission.Summary != "三条要点" || reports[0].Output != "done" {
		t.Fatalf("reports = %+v, %v", reports, err)
	}
	if again, err := s.TakeSubRunReports(ctx, "parent"); err != nil || len(again) != 0 {
		t.Fatalf("reports taken twice = %+v, %v", again, err)
	}

	if _, err := s.StartSubRun(ctx, core.SubRunStart{RunID: "sub_2b", ParentRunID: "parent", Task: 2, Brief: second.Brief, Input: "x", Feedback: "再读", Limit: 3, CreatedAt: time.Now()}); !errors.Is(err, core.ErrSubRunRunning) {
		t.Fatalf("redo of a running attempt = %v", err)
	}
	redo := startSubRun(t, s, core.SubRunStart{RunID: "sub_1b", ParentRunID: "parent", Task: 1, Brief: first.Brief, Feedback: "缺出处"})
	if redo.Task != 1 || redo.Attempt != 2 {
		t.Fatalf("redo = %+v", redo)
	}
	if _, err := s.StartSubRun(ctx, core.SubRunStart{RunID: "sub_y", ParentRunID: "parent", Brief: core.SubRunBrief{Objective: "第四次"}, Input: "x", Limit: 3, CreatedAt: time.Now()}); !errors.Is(err, core.ErrSubRunLimit) {
		t.Fatalf("delegate past the limit = %v", err)
	}
	endRun(t, s, "sub_1b", core.RunStatusSucceeded)
	accepted, err := s.AcceptSubRun(ctx, "parent", 1)
	if err != nil || accepted.RunID != "sub_1b" || accepted.Verdict != core.SubRunAccepted {
		t.Fatalf("accept = %+v, %v", accepted, err)
	}
	if _, err := s.AcceptSubRun(ctx, "parent", 1); !errors.Is(err, core.ErrSubRunReviewed) {
		t.Fatalf("accept twice = %v", err)
	}
	if _, err := s.AcceptSubRun(ctx, "parent", 9); !errors.Is(err, core.ErrSubRunNotFound) {
		t.Fatalf("accept a missing task = %v", err)
	}
	all, err := s.SubRuns(ctx, "parent")
	if err != nil || len(all) != 3 {
		t.Fatalf("sub-runs = %+v, %v", all, err)
	}
	if all[0].RunID != "sub_1" || all[0].Verdict != core.SubRunRedo || all[0].Feedback != "缺出处" || all[1].RunID != "sub_1b" || all[2].RunID != "sub_2" || all[2].Reported() {
		t.Fatalf("sub-runs by task and attempt = %+v", all)
	}
}

// Sub-runs are not the conversation's runs: the latest run, the active run
// that owner messages wait for and the run lists leave them out. Deleting the
// conversation deletes them.
func TestSubRunsStayOutOfTheConversation(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.CreateSession(ctx, "chat", "读文章"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRun(ctx, core.RunCreateParams{RunID: "parent", SessionID: "chat", TurnIndex: 1, Input: "读"}); err != nil {
		t.Fatal(err)
	}
	startSubRun(t, s, core.SubRunStart{RunID: "sub_done", ParentRunID: "parent", Brief: core.SubRunBrief{Objective: "读第一篇"}})
	startSubRun(t, s, core.SubRunStart{RunID: "sub_running", ParentRunID: "parent", Brief: core.SubRunBrief{Objective: "读第二篇"}})
	endRun(t, s, "sub_done", core.RunStatusSucceeded)

	latest, err := s.LoadLatestRunForSession(ctx, "chat")
	if err != nil || latest == nil || latest.RunID != "parent" {
		t.Fatalf("latest run = %+v, %v", latest, err)
	}
	latestBySession, err := s.LoadLatestRunsForSessions(ctx, []string{"chat"})
	if err != nil || latestBySession["chat"] == nil || latestBySession["chat"].RunID != "parent" {
		t.Fatalf("latest runs = %+v, %v", latestBySession, err)
	}
	active, err := s.ListActiveRuns(ctx, 10)
	if err != nil || len(active) != 1 || active[0].RunID != "parent" {
		t.Fatalf("active runs = %+v, %v", active, err)
	}
	endRun(t, s, "sub_running", core.RunStatusFailed)
	endRun(t, s, "parent", core.RunStatusSucceeded)
	terminal, err := s.ListRecentTerminalRuns(ctx, 10)
	if err != nil || len(terminal) != 1 || terminal[0].RunID != "parent" {
		t.Fatalf("terminal runs = %+v, %v", terminal, err)
	}

	if err := s.CreateRun(ctx, core.RunCreateParams{RunID: "next", SessionID: "chat", TurnIndex: 2, Input: "再读"}); err != nil {
		t.Fatal(err)
	}
	startSubRun(t, s, core.SubRunStart{RunID: "sub_next", ParentRunID: "next", Brief: core.SubRunBrief{Objective: "读第三篇"}})
	sent, err := s.SendOwnerMessage(ctx, core.OwnerMessage{SessionID: "chat", Content: "顺便看看评论", RunID: "unused"})
	if err != nil || sent.Queued == nil || sent.Started != nil || sent.RunID != "next" {
		t.Fatalf("message while the parent runs = %+v, %v", sent, err)
	}
	endRun(t, s, "sub_next", core.RunStatusSucceeded)
	endRun(t, s, "next", core.RunStatusSucceeded)

	if err := s.DeleteThread(ctx, "chat"); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sub_runs`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("sub-runs left after deleting the conversation = %d, %v", left, err)
	}
}

// The model usage of a sub-run counts toward the autonomous budget when the
// run that delegated it woke itself.
func TestSubRunsOfWokenRunsAreAutonomous(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.CreateSession(ctx, "chat", "夜思"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"woken", "owner"} {
		if err := s.CreateRun(ctx, core.RunCreateParams{RunID: id, SessionID: "chat", Input: id}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AppendEvent(ctx, "woken", core.EventWakeFired, map[string]any{"routine": "night"}); err != nil {
		t.Fatal(err)
	}
	startSubRun(t, s, core.SubRunStart{RunID: "sub_woken", ParentRunID: "woken", Brief: core.SubRunBrief{Objective: "查"}})
	startSubRun(t, s, core.SubRunStart{RunID: "sub_owner", ParentRunID: "owner", Brief: core.SubRunBrief{Objective: "查"}})
	for run, tokens := range map[string]int{"woken": 100, "sub_woken": 20, "owner": 1000, "sub_owner": 3} {
		if _, err := s.AppendEvent(ctx, run, core.EventModelUsage, map[string]any{"reported": true, "total_tokens": tokens}); err != nil {
			t.Fatal(err)
		}
	}
	report, err := s.UsageReport(ctx, time.Now().Add(-time.Hour))
	if err != nil || report.AutonomousTokens != 120 || report.TotalTokens != 1123 {
		t.Fatalf("usage = %+v, %v", report, err)
	}
	if err := s.ReserveMemoryUsage(ctx, core.MemoryUsage{ID: "m1", Operation: "recall", Budget: "autonomous", InputTokens: 30, CreatedAt: time.Now()}, 140, time.Now().Add(-time.Hour)); !errors.Is(err, core.ErrMemoryBudget) {
		t.Fatalf("reserve past the budget that counts sub-runs = %v", err)
	}
}
