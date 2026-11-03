package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/store"
)

type fakeDelegator struct {
	attempts []core.SubRun
	limit    int
	briefs   []core.SubRunBrief
	redos    []string
}

func (d *fakeDelegator) Delegate(_ context.Context, brief core.SubRunBrief) (core.SubRun, error) {
	d.briefs = append(d.briefs, brief)
	sub := core.SubRun{RunID: "sub", Task: len(d.briefs), Attempt: 1, Brief: brief, Status: core.RunStatusRunning}
	d.attempts = append(d.attempts, sub)
	return sub, nil
}

func (d *fakeDelegator) Accept(_ context.Context, task int) (core.SubRun, error) {
	for i := len(d.attempts) - 1; i >= 0; i-- {
		if d.attempts[i].Task == task {
			d.attempts[i].Verdict = core.SubRunAccepted
			return d.attempts[i], nil
		}
	}
	return core.SubRun{}, core.ErrSubRunNotFound
}

func (d *fakeDelegator) Redo(_ context.Context, task int, feedback string) (core.SubRun, error) {
	d.redos = append(d.redos, feedback)
	sub := core.SubRun{RunID: "redo", Task: task, Attempt: 2, Brief: d.briefs[task-1], Status: core.RunStatusRunning}
	d.attempts = append(d.attempts, sub)
	return sub, nil
}

// SubRuns lists the attempts by task and attempt, as the store does.
func (d *fakeDelegator) SubRuns(context.Context) ([]core.SubRun, int, error) {
	attempts := slices.Clone(d.attempts)
	slices.SortStableFunc(attempts, func(a, b core.SubRun) int { return cmp.Or(a.Task-b.Task, a.Attempt-b.Attempt) })
	return attempts, d.limit - len(d.attempts), nil
}

// delegate checks the brief before it reaches the runtime, and shows the
// latest attempt of each task.
func TestDelegateStartsReviewsAndListsTasks(t *testing.T) {
	delegator := &fakeDelegator{limit: 16}
	tool, err := buildDelegateTool(DelegateToolDeps{Delegator: delegator})
	if err != nil {
		t.Fatal(err)
	}
	call := func(args string) (DelegateOutput, error) {
		t.Helper()
		out, err := tool.(einotool.InvokableTool).InvokableRun(context.Background(), args)
		if err != nil {
			return DelegateOutput{}, err
		}
		var got DelegateOutput
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatal(err)
		}
		return got, nil
	}
	for args, want := range map[string]string{
		`{"action":"start","objective":"  "}`:              "objective is required",
		`{"action":"start","objective":"读","block":["["]}`: "block pattern",
		`{"action":"accept"}`:                              "task is required",
		`{"action":"redo","task":1}`:                       "task and feedback are required",
		`{"action":"wait"}`:                                "action must be",
	} {
		if _, err := call(args); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("delegate %s err = %v, want %q", args, err, want)
		}
	}
	started, err := call(`{"action":"start","objective":" 读第一篇 ","deliverables":["三条要点",""],"acceptance":["每条带原文出处"],"block":["files"]}`)
	if err != nil || len(started.Tasks) != 1 || started.Tasks[0] != (DelegateTask{Task: 1, Attempt: 1, State: "running", Objective: "读第一篇"}) || started.Remaining != 15 || started.Note == "" {
		t.Fatalf("start = %+v, %v", started, err)
	}
	want := core.SubRunBrief{Objective: "读第一篇", Deliverables: []string{"三条要点"}, Acceptance: []string{"每条带原文出处"}, Block: []string{"files"}}
	if !reflect.DeepEqual(delegator.briefs[0], want) {
		t.Fatalf("brief = %+v, want %+v", delegator.briefs[0], want)
	}
	if _, err := call(`{"action":"start","objective":"读第二篇"}`); err != nil {
		t.Fatal(err)
	}
	delegator.attempts[0].Status = core.RunStatusSucceeded
	delegator.attempts[1].Status = core.RunStatusFailed
	redo, err := call(`{"action":"redo","task":1,"feedback":"缺出处"}`)
	if err != nil || redo.Tasks[0].Attempt != 2 || redo.Tasks[0].State != "running" || delegator.redos[0] != "缺出处" {
		t.Fatalf("redo = %+v, %v", redo, err)
	}
	delegator.attempts[2].Status = core.RunStatusSucceeded
	if _, err := call(`{"action":"accept","task":1}`); err != nil {
		t.Fatal(err)
	}
	status, err := call(`{"action":"status"}`)
	wantTasks := []DelegateTask{{Task: 1, Attempt: 2, State: "accepted", Objective: "读第一篇"}, {Task: 2, Attempt: 1, State: "failed", Objective: "读第二篇"}}
	if err != nil || !reflect.DeepEqual(status.Tasks, wantTasks) || status.Remaining != 13 {
		t.Fatalf("status = %+v, %v", status, err)
	}
}

// submit records what the current sub-run hands back; a run that is not a
// sub-run cannot submit.
func TestSubmitRecordsTheSubRunsResult(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.CreateSession(ctx, "chat", "chat"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateRun(ctx, core.RunCreateParams{RunID: "parent", SessionID: "chat", Input: "读"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.StartSubRun(ctx, core.SubRunStart{RunID: "sub", ParentRunID: "parent", Brief: core.SubRunBrief{Objective: "读第一篇"}, Input: "读第一篇", Limit: 16, CreatedAt: followUpNow}); err != nil {
		t.Fatal(err)
	}
	submit := func(runID, args string) error {
		tool, err := buildSubmitTool(SubmitToolDeps{Store: db, Context: fixedArtifactContext{runID: runID, sessionID: "chat"}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = tool.(einotool.InvokableTool).InvokableRun(ctx, args)
		return err
	}
	if err := submit("sub", `{"summary":" "}`); err == nil || !strings.Contains(err.Error(), "summary is required") {
		t.Fatalf("empty summary err = %v", err)
	}
	if err := submit("parent", `{"summary":"x"}`); !errors.Is(err, core.ErrSubRunNotFound) {
		t.Fatalf("submit from the parent = %v", err)
	}
	if err := submit("sub", `{"summary":"三条要点","evidence":["读了全文 https://example.com/a"],"limitations":["没有评论区"]}`); err != nil {
		t.Fatal(err)
	}
	attempts, err := db.SubRuns(ctx, "parent")
	want := &core.SubRunSubmission{Summary: "三条要点", Evidence: []string{"读了全文 https://example.com/a"}, Limitations: []string{"没有评论区"}}
	if err != nil || len(attempts) != 1 || !reflect.DeepEqual(attempts[0].Submission, want) {
		t.Fatalf("attempts = %+v, %v", attempts, err)
	}
}
