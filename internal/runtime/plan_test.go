package runtime

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

// A succeeded run closes what its reply ended in its last plan: the steps
// still running, and a pending last step marked as the reply's. Steps that
// never began stay as they are.
func TestTheReplyClosesThePlan(t *testing.T) {
	plan := func(statuses ...string) core.EventRecord {
		steps := []any{}
		for i, status := range statuses {
			step := map[string]any{"label": fmt.Sprintf("第 %d 步", i+1), "note": "n"}
			step["status"], step["reply"] = strings.TrimSuffix(status, "+reply"), strings.HasSuffix(status, "+reply")
			steps = append(steps, step)
		}
		return core.EventRecord{Sequence: 7, RunID: "run_1", Kind: core.EventPlanUpdated, Payload: map[string]any{"steps": steps}}
	}
	call := core.EventRecord{RunID: "run_1", Kind: "tool.call.succeeded", Payload: map[string]any{"tool_name": "web_fetch"}}
	for name, tc := range map[string]struct {
		events []core.EventRecord
		want   string
	}{
		"no plan":                       {events: []core.EventRecord{call}, want: "false"},
		"last step running":             {events: []core.EventRecord{plan("running", "pending"), plan("done", "failed", "running"), call}, want: "done failed done"},
		"reply step left pending":       {events: []core.EventRecord{plan("done", "running", "pending+reply"), call}, want: "done done done"},
		"steps running at once":         {events: []core.EventRecord{plan("running", "running")}, want: "done done"},
		"a step never begun":            {events: []core.EventRecord{plan("pending", "running", "pending+reply")}, want: "pending done done"},
		"a pending step not the reply":  {events: []core.EventRecord{plan("done", "pending")}, want: "false"},
		"every step already ended":      {events: []core.EventRecord{plan("done", "failed", "done+reply")}, want: "false"},
		"a failed reply step stays":     {events: []core.EventRecord{plan("done", "failed+reply")}, want: "false"},
		"only the latest plan counts":   {events: []core.EventRecord{plan("done", "running"), plan("done", "done")}, want: "false"},
		"the latest plan is the closed": {events: []core.EventRecord{plan("done", "done"), plan("done", "running")}, want: "done done"},
	} {
		closed, ok, err := closedPlan(tc.events)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := "false"
		if ok {
			statuses := make([]string, 0, len(closed.Steps))
			for i, step := range closed.Steps {
				if step.Label != fmt.Sprintf("第 %d 步", i+1) || step.Note != "n" {
					t.Fatalf("%s: step %d = %+v", name, i, step)
				}
				statuses = append(statuses, step.Status)
			}
			got = strings.Join(statuses, " ")
		}
		if got != tc.want {
			t.Fatalf("%s: closed plan = %s, want %s", name, got, tc.want)
		}
	}
	bad := core.EventRecord{Sequence: 9, RunID: "run_1", Kind: core.EventPlanUpdated, Payload: map[string]any{"steps": "读"}}
	if _, _, err := closedPlan([]core.EventRecord{bad}); err == nil {
		t.Fatal("a plan event that is not a plan closed without an error")
	}
}
