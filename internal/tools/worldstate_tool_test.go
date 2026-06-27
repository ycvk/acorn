package tools

import (
	"context"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
)

// stubWorldStateUpdater records ApplyDelta calls and serves Load queries.
type stubWorldStateUpdater struct {
	deltas []worldStateDeltaCall
	state  map[string]string
}

type worldStateDeltaCall struct {
	Upserts map[string]string
	Deletes []string
}

func newStubWorldStateUpdater() *stubWorldStateUpdater {
	return &stubWorldStateUpdater{state: make(map[string]string)}
}

func (s *stubWorldStateUpdater) ApplyDelta(_ context.Context, delta WorldStateDelta) error {
	s.deltas = append(s.deltas, worldStateDeltaCall(delta))
	for k, v := range delta.Upserts {
		s.state[k] = v
	}
	for _, k := range delta.Deletes {
		delete(s.state, k)
	}
	return nil
}

func (s *stubWorldStateUpdater) Load(_ context.Context) (map[string]string, error) {
	out := make(map[string]string, len(s.state))
	for k, v := range s.state {
		out[k] = v
	}
	return out, nil
}

func mustInvokable(t *testing.T, tool einotool.BaseTool) einotool.InvokableTool {
	t.Helper()
	invokable, ok := tool.(einotool.InvokableTool)
	if !ok {
		t.Fatal("tool does not implement InvokableTool")
	}
	return invokable
}

func TestWorldStateUpdateToolUpsertsAndDeletes(t *testing.T) {
	updater := newStubWorldStateUpdater()
	tool, err := buildWorldStateUpdateTool(updater)
	if err != nil {
		t.Fatalf("buildWorldStateUpdateTool: %v", err)
	}
	invokable := mustInvokable(t, tool)

	result, err := invokable.InvokableRun(context.Background(), `{"upserts":{"unread_emails":"5","last_deploy":"success"},"deletes":["old_key"]}`)
	if err != nil {
		t.Fatalf("invoke upsert: %v", err)
	}
	if !strings.Contains(result, `"updated":3`) {
		t.Fatalf("result = %q, want updated count 3", result)
	}

	if len(updater.deltas) != 1 {
		t.Fatalf("deltas = %d, want 1", len(updater.deltas))
	}
	d := updater.deltas[0]
	if d.Upserts["unread_emails"] != "5" {
		t.Fatalf("upserts = %v, want unread_emails=5", d.Upserts)
	}
	if len(d.Deletes) != 1 || d.Deletes[0] != "old_key" {
		t.Fatalf("deletes = %v, want [old_key]", d.Deletes)
	}
}

func TestWorldStateUpdateToolRejectsEmptyDelta(t *testing.T) {
	updater := newStubWorldStateUpdater()
	tool, err := buildWorldStateUpdateTool(updater)
	if err != nil {
		t.Fatalf("buildWorldStateUpdateTool: %v", err)
	}
	invokable := mustInvokable(t, tool)

	_, err = invokable.InvokableRun(context.Background(), `{"upserts":{},"deletes":[]}`)
	if err == nil {
		t.Fatal("expected error for empty delta, got nil")
	}
}

func TestWorldStateUpdateToolRequiresNonNilUpdater(t *testing.T) {
	_, err := buildWorldStateUpdateTool(nil)
	if err == nil {
		t.Fatal("expected error for nil updater, got nil")
	}
}

func TestWorldStateLoadToolReturnsCurrentState(t *testing.T) {
	updater := newStubWorldStateUpdater()
	updater.state["unread_emails"] = "3"
	updater.state["last_deploy"] = "success"

	tool, err := buildWorldStateLoadTool(updater)
	if err != nil {
		t.Fatalf("buildWorldStateLoadTool: %v", err)
	}
	invokable := mustInvokable(t, tool)

	result, err := invokable.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("invoke load: %v", err)
	}
	if !strings.Contains(result, "unread_emails") {
		t.Fatalf("result = %q, want contains 'unread_emails'", result)
	}
	if !strings.Contains(result, "3") {
		t.Fatalf("result = %q, want contains '3'", result)
	}
}
