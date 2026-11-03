package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/store"
)

// goal starts one goal per conversation and ends it with a result.
func TestGoalStartsAndEndsTheConversationsGoal(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.CreateSession(context.Background(), "chat", "chat"); err != nil {
		t.Fatal(err)
	}
	tool, err := buildGoalTool(GoalToolDeps{Store: db, Context: fixedArtifactContext{runID: "run_goal", sessionID: "chat"}, Clock: func() time.Time { return followUpNow }})
	if err != nil {
		t.Fatal(err)
	}
	call := func(args string) (GoalOutput, error) {
		t.Helper()
		out, err := tool.(einotool.InvokableTool).InvokableRun(context.Background(), args)
		if err != nil {
			return GoalOutput{}, err
		}
		var got GoalOutput
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatal(err)
		}
		return got, nil
	}
	for args, want := range map[string]string{
		`{"action":"start"}`: "objective is required",
		`{"action":"start","objective":"x","max_runs":51}`: "between 1 and 50",
		`{"action":"done","result":"x"}`:                   "no active goal",
		`{"action":"later"}`:                               "action must be",
	} {
		if _, err := call(args); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("goal %s err = %v, want %q", args, err, want)
		}
	}
	started, err := call(`{"action":"start","objective":"把这本书下完并整理进知识库"}`)
	if err != nil || started.State != core.GoalActive || started.MaxRuns != 10 || started.Runs != 0 {
		t.Fatalf("start = %+v, %v", started, err)
	}
	if _, err := call(`{"action":"start","objective":"另一个"}`); err == nil || !strings.Contains(err.Error(), "already has goal #1 (把这本书下完并整理进知识库)") {
		t.Fatalf("second start err = %v", err)
	}
	active, err := db.ActiveGoal(context.Background(), "chat")
	if err != nil || active.SourceRunID != "run_goal" || !active.CreatedAt.Equal(followUpNow) {
		t.Fatalf("active = %+v, %v", active, err)
	}
	if _, err := call(`{"action":"blocked"}`); err == nil || !strings.Contains(err.Error(), "result is required") {
		t.Fatalf("blocked without a result err = %v", err)
	}
	ended, err := call(`{"action":"blocked","result":"需要你登录图书馆账号"}`)
	if err != nil || ended.State != core.GoalBlocked {
		t.Fatalf("blocked = %+v, %v", ended, err)
	}
	pending, err := db.UnannouncedGoals(context.Background())
	if err != nil || len(pending) != 1 || pending[0].Result != "需要你登录图书馆账号" {
		t.Fatalf("unannounced = %+v, %v", pending, err)
	}
}
