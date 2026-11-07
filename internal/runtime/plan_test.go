package runtime

import (
	"fmt"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

// A succeeded run closes its last plan only when the reply is all that was
// left: the last step running and every step before it ended.
func TestTheReplyClosesTheLastStepOfThePlan(t *testing.T) {
	plan := func(statuses ...string) core.EventRecord {
		steps := []any{}
		for i, status := range statuses {
			steps = append(steps, map[string]any{"label": fmt.Sprintf("第 %d 步", i+1), "status": status, "note": "n"})
		}
		return core.EventRecord{Sequence: 7, RunID: "run_1", Kind: core.EventPlanUpdated, Payload: map[string]any{"steps": steps}}
	}
	call := core.EventRecord{RunID: "run_1", Kind: "tool.call.succeeded", Payload: map[string]any{"tool_name": "web_fetch"}}
	for name, tc := range map[string]struct {
		events []core.EventRecord
		want   string
	}{
		"no plan":                     {events: []core.EventRecord{call}, want: "false"},
		"reply left":                  {events: []core.EventRecord{plan("running", "pending"), plan("done", "failed", "running"), call}, want: "[{第 1 步 done n} {第 2 步 failed n} {第 3 步 done n}]"},
		"last step already done":      {events: []core.EventRecord{plan("done", "done")}, want: "false"},
		"a step before still running": {events: []core.EventRecord{plan("running", "running")}, want: "false"},
		"a step before never begun":   {events: []core.EventRecord{plan("pending", "running")}, want: "false"},
		"only the latest plan counts": {events: []core.EventRecord{plan("done", "running"), plan("running", "pending")}, want: "false"},
	} {
		closed, ok, err := closedPlan(tc.events)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := "false"
		if ok {
			got = fmt.Sprint(closed.Steps)
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
