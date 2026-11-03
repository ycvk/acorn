package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// A follow-up waits until a tick claims it, and fires once its run started;
// a claim whose run did not start, or that was left, waits again. Only a
// waiting follow-up can be cancelled, within its own conversation or from
// any.
func TestFollowUpsWaitThenFireOnce(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range []string{"chat", "other"} {
		if _, err := s.CreateSession(ctx, id, id); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC)
	add := func(f core.FollowUp) (core.FollowUp, error) {
		f.CreatedAt = now
		if f.DeadlineAt.IsZero() {
			f.DeadlineAt = now.Add(24 * time.Hour)
		}
		return s.AddFollowUp(ctx, f)
	}
	for _, bad := range []core.FollowUp{
		{SessionID: "chat", Task: "整理"},
		{SessionID: "chat", Task: "整理", JobID: "job-00000001", Path: "book.epub"},
		{SessionID: "chat", Task: "整理", Path: "book.epub", OutputContains: "done"},
		{SessionID: "chat", Task: "整理", JobID: "job-00000001", DeadlineAt: now},
		{SessionID: "chat", JobID: "job-00000001"},
	} {
		if _, err := add(bad); err == nil {
			t.Fatalf("follow-up %+v must be refused", bad)
		}
	}
	if _, err := add(core.FollowUp{SessionID: "gone", Task: "整理", JobID: "job-00000001"}); !errors.Is(err, core.ErrSessionNotFound) {
		t.Fatalf("follow-up in a missing conversation = %v", err)
	}
	job, err := add(core.FollowUp{SessionID: "chat", SourceRunID: "run_1", Task: "下载完就整理", JobID: "job-00000001", OutputContains: "100%"})
	if err != nil {
		t.Fatal(err)
	}
	file, err := add(core.FollowUp{SessionID: "other", Task: "文件到了就读", Path: "book.epub", DeadlineAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	all, err := s.WaitingFollowUps(ctx, "")
	if err != nil || len(all) != 2 || all[0].ID != file.ID || all[1].ID != job.ID {
		t.Fatalf("waiting = %+v, %v", all, err)
	}
	size := int64(42)
	if err := s.RecordFollowUpCheck(ctx, file.ID, 0, &size, now); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordFollowUpCheck(ctx, job.ID, 1024, nil, now); err != nil {
		t.Fatal(err)
	}
	mine, err := s.WaitingFollowUps(ctx, "chat")
	if err != nil || len(mine) != 1 || mine[0].ScannedOffset != 1024 || mine[0].PathSize != nil || mine[0].SourceRunID != "run_1" {
		t.Fatalf("chat's waiting = %+v, %v", mine, err)
	}
	theirs, err := s.WaitingFollowUps(ctx, "other")
	if err != nil || len(theirs) != 1 || theirs[0].PathSize == nil || *theirs[0].PathSize != 42 {
		t.Fatalf("other's waiting = %+v, %v", theirs, err)
	}

	if err := s.ClaimFollowUp(ctx, job.ID, "job job-00000001 printed \"100%\"", now); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimFollowUp(ctx, job.ID, "again", now); !errors.Is(err, core.ErrFollowUpNotWaiting) {
		t.Fatalf("second claim = %v", err)
	}
	if err := s.ReleaseFollowUp(ctx, job.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimFollowUp(ctx, job.ID, "job job-00000001 printed \"100%\"", now); err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverFollowUpClaims(ctx, now.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.StartFollowUp(ctx, job.ID, "run_2", now); err != nil {
		t.Fatalf("a recent claim must survive recovery: %v", err)
	}
	if err := s.CancelFollowUp(ctx, "chat", job.ID, now); !errors.Is(err, core.ErrFollowUpNotWaiting) {
		t.Fatalf("cancel a fired follow-up = %v", err)
	}

	if err := s.ClaimFollowUp(ctx, file.ID, "book.epub is ready", now); err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverFollowUpClaims(ctx, now.Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.CancelFollowUp(ctx, "chat", file.ID, now); !errors.Is(err, core.ErrFollowUpNotFound) {
		t.Fatalf("cancel another conversation's follow-up = %v", err)
	}
	if err := s.CancelFollowUp(ctx, "", file.ID, now); err != nil {
		t.Fatalf("cancel a recovered follow-up from any conversation: %v", err)
	}
	if err := s.CancelFollowUp(ctx, "", file.ID, now); !errors.Is(err, core.ErrFollowUpNotWaiting) {
		t.Fatalf("cancel a cancelled follow-up = %v", err)
	}
	if err := s.CancelFollowUp(ctx, "", 99, now); !errors.Is(err, core.ErrFollowUpNotFound) {
		t.Fatalf("cancel a missing follow-up = %v", err)
	}
	if left, err := s.WaitingFollowUps(ctx, ""); err != nil || len(left) != 0 {
		t.Fatalf("left waiting = %+v, %v", left, err)
	}
	if err := s.ClaimFollowUp(ctx, 99, "nothing", now); !errors.Is(err, core.ErrFollowUpNotFound) {
		t.Fatalf("claim a missing follow-up = %v", err)
	}
}

// A conversation has one active goal at a time. A run counts only against
// the goal as it was read and within its runs; an ended goal waits for its
// announcement unless it was stopped. Deleting the conversation deletes its
// goals and follow-ups.
func TestGoalsRunWithinTheirBudget(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.CreateSession(ctx, "chat", "chat"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC)
	if _, err := s.ActiveGoal(ctx, "chat"); !errors.Is(err, core.ErrGoalNotFound) {
		t.Fatalf("goal before any = %v", err)
	}
	g, err := s.StartGoal(ctx, core.Goal{SessionID: "chat", SourceRunID: "run_1", Objective: "下完整本书并整理进知识库", MaxRuns: 2, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartGoal(ctx, core.Goal{SessionID: "chat", Objective: "另一个", MaxRuns: 2, CreatedAt: now}); !errors.Is(err, core.ErrGoalActive) {
		t.Fatalf("second active goal = %v", err)
	}
	if err := s.CountGoalRun(ctx, g.ID, 0, now); err != nil {
		t.Fatal(err)
	}
	if err := s.CountGoalRun(ctx, g.ID, 0, now); !errors.Is(err, core.ErrGoalChanged) {
		t.Fatalf("count from a stale read = %v", err)
	}
	if err := s.CountGoalRun(ctx, g.ID, 1, now); err != nil {
		t.Fatal(err)
	}
	if err := s.CountGoalRun(ctx, g.ID, 2, now); !errors.Is(err, core.ErrGoalChanged) {
		t.Fatalf("count past max_runs = %v", err)
	}
	active, err := s.ActiveGoal(ctx, "chat")
	if err != nil || active.Runs != 2 || active.SourceRunID != "run_1" {
		t.Fatalf("active = %+v, %v", active, err)
	}
	ended, err := s.EndGoal(ctx, g.ID, core.GoalDone, "全书 12 章已入库", "run_end", now)
	if err != nil || ended.State != core.GoalDone || ended.Announced || ended.EndRunID != "run_end" {
		t.Fatalf("ended = %+v, %v", ended, err)
	}
	if _, err := s.EndGoal(ctx, g.ID, core.GoalBlocked, "again", "", now); !errors.Is(err, core.ErrGoalEnded) {
		t.Fatalf("end an ended goal = %v", err)
	}
	if _, err := s.EndGoal(ctx, 99, core.GoalStopped, "the owner stopped it", "", now); !errors.Is(err, core.ErrGoalNotFound) {
		t.Fatalf("end a missing goal = %v", err)
	}
	pending, err := s.UnannouncedGoals(ctx)
	if err != nil || len(pending) != 1 || pending[0].Result != "全书 12 章已入库" || pending[0].EndRunID != "run_end" {
		t.Fatalf("unannounced = %+v, %v", pending, err)
	}
	if err := s.MarkGoalAnnounced(ctx, g.ID, now); err != nil {
		t.Fatal(err)
	}
	stopped, err := s.StartGoal(ctx, core.Goal{SessionID: "chat", Objective: "再整理一遍", MaxRuns: 3, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EndGoal(ctx, stopped.ID, core.GoalStopped, "owner asked to stop", "run_stop", now); err != nil {
		t.Fatal(err)
	}
	if pending, err := s.UnannouncedGoals(ctx); err != nil || len(pending) != 0 {
		t.Fatalf("unannounced after a stop = %+v, %v", pending, err)
	}

	if _, err := s.StartGoal(ctx, core.Goal{SessionID: "chat", Objective: "第三个", MaxRuns: 3, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFollowUp(ctx, core.FollowUp{SessionID: "chat", Task: "等", JobID: "job-00000001", CreatedAt: now, DeadlineAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteThread(ctx, "chat"); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := s.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM goals) + (SELECT COUNT(*) FROM follow_ups)`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("rows left after delete = %d, %v", left, err)
	}
}

// Upgrading to version 33 keeps the goals, with no ending run recorded.
func TestGoalEndRunsKeepTheGoals(t *testing.T) {
	dir := t.TempDir()
	raw := openRaw(t, dir)
	if _, err := raw.Exec(readLayout(t, 32) + `PRAGMA user_version = 32;
INSERT INTO goals(session_id, objective, max_runs, runs, state, result, announced, created_at, updated_at)
VALUES('chat', '四大名著', 12, 1, 'done', '四本都齐了', 1, '2026-11-03T09:02:50.000000000Z', '2026-11-03T09:17:10.000000000Z');`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	goals, err := s.queryGoals(context.Background(), `SELECT `+goalColumns+` FROM goals`)
	if err != nil || len(goals) != 1 || goals[0].Objective != "四大名著" || goals[0].Runs != 1 || !goals[0].Announced || goals[0].EndRunID != "" {
		t.Fatalf("goals after the upgrade = %+v, %v", goals, err)
	}
}

// A run notified the owner when it pushed something of its own; the pushes
// announcing its pending actions do not count.
func TestRunNotifiedOwnerCountsOnlyTheRunsOwnPushes(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC)
	for _, n := range []core.Notification{
		{Title: "需要你批准", Body: "下载要访问外网", ThreadID: "chat", RunID: "run_approval", ActionID: "action_1", SendAfter: now, CreatedAt: now},
		{Title: "四大名著齐了", Body: "四本都附上了", ThreadID: "chat", RunID: "run_told", SendAfter: now, CreatedAt: now},
	} {
		if _, err := s.QueueNotification(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	for run, want := range map[string]bool{"run_told": true, "run_approval": false, "run_quiet": false} {
		if got, err := s.RunNotifiedOwner(ctx, run); err != nil || got != want {
			t.Fatalf("RunNotifiedOwner(%s) = %v, %v; want %v", run, got, err, want)
		}
	}
}
