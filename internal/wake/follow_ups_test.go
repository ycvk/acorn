package wake

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/workstation"
)

// fakeWorkstation serves the jobs and paths a test sets; a search finds the
// text in the jobs listed in printed and otherwise moves 100 bytes on.
type fakeWorkstation struct {
	mu       sync.Mutex
	jobs     map[string]workstation.Job
	printed  map[string]bool
	paths    map[string]workstation.StatResult
	searches []int64
}

func newFakeWorkstation() *fakeWorkstation {
	return &fakeWorkstation{jobs: map[string]workstation.Job{}, printed: map[string]bool{}, paths: map[string]workstation.StatResult{}}
}

func (w *fakeWorkstation) setJob(id, state string, code ...int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	job := workstation.Job{ID: id, State: state}
	if len(code) > 0 {
		job.ExitCode = &code[0]
	}
	w.jobs[id] = job
}

func (w *fakeWorkstation) setPath(path string, size int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.paths[path] = workstation.StatResult{Path: "/home/agent/" + path, Exists: true, Size: size}
}

func (w *fakeWorkstation) Job(_ context.Context, id string) (workstation.Job, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	job, ok := w.jobs[id]
	if !ok {
		return workstation.Job{}, &workstation.HTTPError{Path: "/v1/jobs/" + id, Status: http.StatusNotFound, Message: "job not found: " + id}
	}
	return job, nil
}

func (w *fakeWorkstation) FindJobOutput(ctx context.Context, id, _ string, offset int64) (workstation.JobFind, error) {
	job, err := w.Job(ctx, id)
	if err != nil {
		return workstation.JobFind{}, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.searches = append(w.searches, offset)
	return workstation.JobFind{Job: job, Found: w.printed[id], NextOffset: offset + 100}, nil
}

func (w *fakeWorkstation) Stat(_ context.Context, path string) (workstation.StatResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if st, ok := w.paths[path]; ok {
		return st, nil
	}
	return workstation.StatResult{Path: "/home/agent/" + path}, nil
}

func (h *harness) conversation(t *testing.T, id string) {
	t.Helper()
	if _, err := h.data.CreateSession(context.Background(), id, id); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) followUp(t *testing.T, f core.FollowUp) core.FollowUp {
	t.Helper()
	f.CreatedAt = h.now.Add(-time.Hour)
	if f.DeadlineAt.IsZero() {
		f.DeadlineAt = h.now.Add(23 * time.Hour)
	}
	added, err := h.data.AddFollowUp(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	return added
}

// finish ends a run the way the executor records it.
func (h *harness) finish(t *testing.T, runID string, status core.RunStatus, failure string) {
	t.Helper()
	if _, _, err := h.data.FinishRun(context.Background(), core.RunEnd{RunID: runID, Status: status, Error: failure, Event: "run." + string(status)}); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) lastStart(t *testing.T, n int) string {
	t.Helper()
	if len(h.runs.starts) != n {
		t.Fatalf("starts = %q, want %d", h.runs.starts, n)
	}
	if n == 0 {
		return ""
	}
	return h.runs.starts[n-1]
}

// A follow-up wakes its conversation once, when its job ended, printed the
// text or its file stopped growing, after the conversation's run ended. At
// its deadline it wakes the conversation with how things stand.
func TestFollowUpsWakeTheirConversationOnce(t *testing.T) {
	h := newHarness(t, 20)
	h.runs.records = h.data
	for _, id := range []string{"download", "server", "book", "late", "gone"} {
		h.conversation(t, id)
	}
	h.workstation.setJob("job-00000001", workstation.JobRunning)
	h.followUp(t, core.FollowUp{SessionID: "download", Task: "下载完就整理", JobID: "job-00000001"})
	h.tick(t)
	h.lastStart(t, 0)
	h.workstation.setJob("job-00000001", workstation.JobExited, 0)
	h.tick(t)
	if got := h.lastStart(t, 1); got != "download|follow-up #1: job job-00000001 exited with code 0|[follow_up #1, set 2026-10-05 08:00] 下载完就整理\nWhat happened: job job-00000001 exited with code 0" {
		t.Fatalf("start = %q", got)
	}
	h.tick(t)
	h.lastStart(t, 1)
	if len(h.store.events) != 1 || h.store.events[0] != "run_1:wake.fired" {
		t.Fatalf("events = %v", h.store.events)
	}

	// A conversation busy with its run is left until the run ends.
	h.workstation.setJob("job-00000002", workstation.JobRunning)
	h.followUp(t, core.FollowUp{SessionID: "download", Task: "服务起来后测一下", JobID: "job-00000002", OutputContains: "ready"})
	h.tick(t)
	h.tick(t)
	if fmt.Sprint(h.workstation.searches) != "[0 100]" {
		t.Fatalf("searches started at %v, want each where the last stopped", h.workstation.searches)
	}
	h.workstation.printed["job-00000002"] = true
	h.tick(t)
	h.lastStart(t, 1)
	h.finish(t, "run_1", core.RunStatusSucceeded, "")
	h.tick(t)
	if got := h.lastStart(t, 2); !strings.HasSuffix(got, "What happened: job job-00000002 printed \"ready\"") {
		t.Fatalf("start = %q", got)
	}

	h.followUp(t, core.FollowUp{SessionID: "book", Task: "书到了就读", Path: "book.epub"})
	h.tick(t)
	h.workstation.setPath("book.epub", 10)
	h.tick(t)
	h.lastStart(t, 2)
	h.tick(t)
	if got := h.lastStart(t, 3); !strings.HasSuffix(got, "What happened: /home/agent/book.epub is ready: 10 bytes, unchanged since the previous check") {
		t.Fatalf("start = %q", got)
	}

	h.workstation.setJob("job-00000003", workstation.JobRunning)
	h.followUp(t, core.FollowUp{SessionID: "late", Task: "编译完就装", JobID: "job-00000003", DeadlineAt: h.now.Add(time.Minute)})
	h.tick(t)
	h.lastStart(t, 3)
	h.now = h.now.Add(2 * time.Minute)
	h.tick(t)
	if got := h.lastStart(t, 4); !strings.HasSuffix(got, "What happened: its deadline 2026-10-05 09:01 passed; job job-00000003 is still running") {
		t.Fatalf("start = %q", got)
	}

	h.followUp(t, core.FollowUp{SessionID: "gone", Task: "看结果", JobID: "job-0000000f"})
	h.tick(t)
	if got := h.lastStart(t, 5); !strings.HasSuffix(got, "What happened: job job-0000000f is no longer on the workstation") {
		t.Fatalf("start = %q", got)
	}
	if waiting, err := h.data.WaitingFollowUps(context.Background(), ""); err != nil || len(waiting) != 0 {
		t.Fatalf("still waiting = %+v, %v", waiting, err)
	}
}

// Without a workstation a follow-up wakes its conversation at its deadline.
func TestFollowUpsWithoutAWorkstationWaitForTheirDeadline(t *testing.T) {
	h := newHarness(t, 20)
	h.sched.cfg.Workstation = nil
	h.conversation(t, "chat")
	h.followUp(t, core.FollowUp{SessionID: "chat", Task: "看下载", JobID: "job-00000001", DeadlineAt: h.now.Add(time.Hour)})
	h.tick(t)
	h.lastStart(t, 0)
	h.now = h.now.Add(time.Hour)
	h.tick(t)
	if got := h.lastStart(t, 1); !strings.HasSuffix(got, "What happened: its deadline passed; no workstation is configured to check it") {
		t.Fatalf("start = %q", got)
	}
}

// A goal continues its conversation after each run until its runs run out,
// waiting while a follow-up of the conversation waits. A goal that ended
// done or blocked is pushed once its last run finished; a failed run blocks
// the goal unless Acorn stopped it by restarting.
func TestGoalsContinueUntilTheyEnd(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, 20)
	notifier := &fakeNotifier{}
	h.sched.WithNotifications(notifier)
	h.runs.records = h.data
	h.conversation(t, "chat")
	if err := h.data.CreateRun(ctx, core.RunCreateParams{RunID: "run_set", SessionID: "chat", TurnIndex: 1, Input: "把这本书下完"}); err != nil {
		t.Fatal(err)
	}
	g, err := h.data.StartGoal(ctx, core.Goal{SessionID: "chat", SourceRunID: "run_set", Objective: "把这本书下完并整理进知识库", MaxRuns: 2, CreatedAt: h.now})
	if err != nil {
		t.Fatal(err)
	}
	h.tick(t)
	h.lastStart(t, 0)
	h.finish(t, "run_set", core.RunStatusSucceeded, "")
	h.tick(t)
	if got := h.lastStart(t, 1); !strings.HasPrefix(got, "chat|goal #1: 把这本书下完并整理进知识库|[goal #1, run 1 of 2] Objective: 把这本书下完并整理进知识库\nKeep working toward the objective") {
		t.Fatalf("start = %q", got)
	}
	h.tick(t)
	h.lastStart(t, 1)
	h.finish(t, "run_1", core.RunStatusSucceeded, "")
	h.workstation.setJob("job-00000001", workstation.JobRunning)
	wait := h.followUp(t, core.FollowUp{SessionID: "chat", Task: "下载完继续", JobID: "job-00000001"})
	h.tick(t)
	h.lastStart(t, 1)
	if err := h.data.CancelFollowUp(ctx, "chat", wait.ID, h.now); err != nil {
		t.Fatal(err)
	}
	h.tick(t)
	if got := h.lastStart(t, 2); !strings.Contains(got, "[goal #1, run 2 of 2]") {
		t.Fatalf("start = %q", got)
	}
	h.finish(t, "run_2", core.RunStatusSucceeded, "")
	h.tick(t)
	h.lastStart(t, 2)
	if len(notifier.pushed) != 1 || notifier.pushed[0].Title != "目标需要你" || notifier.pushed[0].Body != "used all 2 runs without reaching it" || notifier.pushed[0].ThreadID != "chat" || notifier.pushed[0].RunID != "run_2" {
		t.Fatalf("pushed = %+v", notifier.pushed)
	}
	if _, err := h.data.ActiveGoal(ctx, "chat"); err == nil {
		t.Fatalf("goal #%d still active", g.ID)
	}

	done, err := h.data.StartGoal(ctx, core.Goal{SessionID: "chat", Objective: "再整理一遍", MaxRuns: 3, CreatedAt: h.now})
	if err != nil {
		t.Fatal(err)
	}
	h.tick(t)
	h.lastStart(t, 3)
	if _, err := h.data.EndGoal(ctx, done.ID, core.GoalDone, "整理好了，共 12 篇", h.now); err != nil {
		t.Fatal(err)
	}
	h.tick(t)
	if len(notifier.pushed) != 1 {
		t.Fatalf("pushed while the run goes = %+v", notifier.pushed)
	}
	h.finish(t, "run_3", core.RunStatusSucceeded, "")
	h.tick(t)
	h.tick(t)
	if len(notifier.pushed) != 2 || notifier.pushed[1].Title != "目标完成" || notifier.pushed[1].Body != "整理好了，共 12 篇" {
		t.Fatalf("pushed = %+v", notifier.pushed)
	}

	if _, err := h.data.StartGoal(ctx, core.Goal{SessionID: "chat", Objective: "第三件", MaxRuns: 3, CreatedAt: h.now}); err != nil {
		t.Fatal(err)
	}
	if err := h.data.CreateRun(ctx, core.RunCreateParams{RunID: "run_restart", SessionID: "chat", TurnIndex: 103, Input: "x"}); err != nil {
		t.Fatal(err)
	}
	h.finish(t, "run_restart", core.RunStatusFailed, core.AbandonedRunError)
	h.tick(t)
	if got := h.lastStart(t, 4); !strings.Contains(got, "The last run stopped when Acorn restarted") {
		t.Fatalf("start = %q", got)
	}
	h.finish(t, "run_4", core.RunStatusFailed, "model unavailable")
	h.tick(t)
	h.lastStart(t, 4)
	if len(notifier.pushed) != 3 || notifier.pushed[2].Body != "run run_4 failed: model unavailable" {
		t.Fatalf("pushed = %+v", notifier.pushed)
	}
}
