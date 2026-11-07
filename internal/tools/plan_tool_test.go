package tools

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/store"
)

// plan records each whole plan on the run and rejects one the app cannot show.
func TestPlanRecordsTheWholePlanOnTheRun(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if _, err := db.CreateSession(ctx, "chat", "chat"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateRun(ctx, core.RunCreateParams{RunID: "run_plan", SessionID: "chat", Input: "读三篇文章并汇总"}); err != nil {
		t.Fatal(err)
	}
	tool, err := buildPlanTool(PlanToolDeps{Store: db, Context: fixedArtifactContext{runID: "run_plan", sessionID: "chat"}})
	if err != nil {
		t.Fatal(err)
	}
	call := func(args string) (PlanOutput, error) {
		t.Helper()
		out, err := tool.(einotool.InvokableTool).InvokableRun(ctx, args)
		if err != nil {
			return PlanOutput{}, err
		}
		var got PlanOutput
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatal(err)
		}
		return got, nil
	}
	long := strings.Repeat("长", maxPlanLabelRunes+1)
	for args, want := range map[string]string{
		`{"steps":[{"label":"读","status":"running"}]}`:                                                                 "a plan has 2 to 12 steps, got 1",
		`{"steps":[{"label":" ","status":"running"},{"label":"写","status":"pending"}]}`:                                "steps[0].label is empty",
		`{"steps":[{"label":"` + long + `","status":"running"},{"label":"写","status":"pending"}]}`:                     "steps[0].label has 61 characters, at most 60",
		`{"steps":[{"label":"读","status":"running","note":"一二三四五六七八九十一二三四五六七八九十一"},{"label":"写","status":"pending"}]}`: "steps[0].note has 21 characters, at most 20",
		`{"steps":[{"label":"读","status":"in_progress"},{"label":"写","status":"pending"}]}`:                            `steps[0].status must be pending, running, done or failed, got "in_progress"`,
		`{"steps":[{"label":"读","status":"running"},{"label":"写","status":"running"}]}`:                                "2 steps are running",
	} {
		if _, err := call(args); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("plan %s err = %v, want %q", args, err, want)
		}
	}
	if _, err := call(`{"steps":[{"label":"读三篇文章","status":"running","note":"0/3"},{"label":"写汇总","status":"pending"}]}`); err != nil {
		t.Fatal(err)
	}
	got, err := call(`{"steps":[{"label":"  读三篇\n文章 ","status":"done","note":" 3/3 "},{"label":"写汇总","status":"running"},{"label":"核对引用","status":"pending"}]}`)
	if err != nil || got.Done != 1 || got.Total != 3 {
		t.Fatalf("second plan = %+v, %v", got, err)
	}
	events, err := db.LoadEvents(ctx, "run_plan")
	if err != nil {
		t.Fatal(err)
	}
	var plans []any
	for _, event := range events {
		if event.Kind == core.EventPlanUpdated {
			plans = append(plans, event.Payload)
		}
	}
	want := map[string]any{"steps": []any{
		map[string]any{"label": "读三篇 文章", "status": "done", "note": "3/3"},
		map[string]any{"label": "写汇总", "status": "running"},
		map[string]any{"label": "核对引用", "status": "pending"},
	}}
	if len(plans) != 2 || !reflect.DeepEqual(plans[1], want) {
		t.Fatalf("plan events = %#v, want two with the last %#v", plans, want)
	}
}
