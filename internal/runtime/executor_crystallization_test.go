package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/ycvk/acorn/internal/crystallization"
	"github.com/ycvk/acorn/internal/events"
	storerepo "github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/stream"
)

type stubCrystallizer struct {
	crystallize func(context.Context, crystallization.CrystallizationRequest) (*crystallization.CrystallizationResult, error)
}

func (s stubCrystallizer) Crystallize(ctx context.Context, req crystallization.CrystallizationRequest) (*crystallization.CrystallizationResult, error) {
	return s.crystallize(ctx, req)
}

func (s stubCrystallizer) BuildIndexEntry(ctx context.Context, skillID string) (*crystallization.IndexEntry, error) {
	return nil, nil
}

func (s stubCrystallizer) QueryIndex(ctx context.Context, input string, limit int) ([]crystallization.IndexEntry, error) {
	return nil, nil
}

func TestCrystallizationCalledOnSuccess(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	exec := newFinalizationTestExecutor(t, store, cfg)

	var lastReq crystallization.CrystallizationRequest
	svc := stubCrystallizer{crystallize: func(ctx context.Context, req crystallization.CrystallizationRequest) (*crystallization.CrystallizationResult, error) {
		lastReq = req
		return &crystallization.CrystallizationResult{Verdict: crystallization.VerdictCrystallized, SkillID: "skill-mock"}, nil
	}}
	exec.SetCrystallizer(svc)

	sessionID := "session-crystal"
	runID := createFinalizationRun(t, ctx, store, sessionID, "deploy app")
	appendSuccessfulToolEvent(t, ctx, store, runID, "deploy_tool", `{"target":"prod"}`)
	evidenceRef := appendSuccessfulToolResult(t, ctx, store, runID, sessionID, "call_deploy", "deploy_tool")

	result, err := exec.finishCollectedRun(ctx, runID, "deploy app", RunState{lastOutput: "deployed"}, nil, nil)
	if err != nil {
		t.Fatalf("finishCollectedRun: %v", err)
	}
	if result.TraceSummary == nil || !result.TraceSummary.Completed {
		t.Fatal("expected successful completion")
	}
	if lastReq.RunID != runID {
		t.Fatalf("expected runID %q, got %q", runID, lastReq.RunID)
	}
	if len(lastReq.ToolNames) != 1 || lastReq.ToolNames[0] != "deploy_tool" {
		t.Fatalf("expected tool names from finalized archive, got %v", lastReq.ToolNames)
	}
	if len(lastReq.EvidenceRefs) != 1 || lastReq.EvidenceRefs[0] != evidenceRef {
		t.Fatalf("expected evidence refs %q, got %v", evidenceRef, lastReq.EvidenceRefs)
	}

	run, err := store.LoadRun(ctx, runID)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if run.Status != events.RunStatusSucceeded {
		t.Fatalf("run status = %q, want succeeded", run.Status)
	}
}

func TestCrystallizationErrorDoesNotBlockSuccess(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	exec := newFinalizationTestExecutor(t, store, cfg)
	svc := stubCrystallizer{crystallize: func(ctx context.Context, req crystallization.CrystallizationRequest) (*crystallization.CrystallizationResult, error) {
		return nil, errors.New("crystallizer failure")
	}}
	exec.SetCrystallizer(svc)

	sessionID := "session-crystal-err"
	runID := createFinalizationRun(t, ctx, store, sessionID, "update config")
	appendSuccessfulToolEvent(t, ctx, store, runID, "file_edit", `{"path":"config.yaml"}`)
	appendSuccessfulToolResult(t, ctx, store, runID, sessionID, "call_edit", "file_edit")

	result, err := exec.finishCollectedRun(ctx, runID, "update config", RunState{lastOutput: "done"}, nil, nil)
	if err != nil {
		t.Fatalf("finishCollectedRun: %v", err)
	}
	if result.TraceSummary == nil || !result.TraceSummary.Completed {
		t.Fatal("expected successful completion despite crystallization error")
	}

	run, err := store.LoadRun(ctx, runID)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if run.Status != events.RunStatusSucceeded {
		t.Fatalf("run status = %q, want succeeded despite crystallization failure", run.Status)
	}
}

func TestCrystallizationSkippedWhenNil(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	exec := newFinalizationTestExecutor(t, store, cfg)
	runID := createFinalizationRun(t, ctx, store, "session-no-crystal", "list files")
	appendSuccessfulToolEvent(t, ctx, store, runID, "file_read", `{"path":"README.md"}`)

	result, err := exec.finishCollectedRun(ctx, runID, "list files", RunState{lastOutput: "files listed"}, nil, nil)
	if err != nil {
		t.Fatalf("finishCollectedRun: %v", err)
	}
	if result.TraceSummary == nil || !result.TraceSummary.Completed {
		t.Fatal("expected successful completion without crystallizer")
	}

	run, err := store.LoadRun(ctx, runID)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if run.Status != events.RunStatusSucceeded {
		t.Fatalf("run status = %q, want succeeded", run.Status)
	}
}

func TestCrystallizationVerdictEventEmitted(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	exec := newFinalizationTestExecutor(t, store, cfg)
	svc := stubCrystallizer{crystallize: func(ctx context.Context, req crystallization.CrystallizationRequest) (*crystallization.CrystallizationResult, error) {
		return &crystallization.CrystallizationResult{Verdict: crystallization.VerdictInsufficientValue, Reason: "no meaningful tool sequence"}, nil
	}}
	exec.SetCrystallizer(svc)

	sessionID := "session-verdict"
	runID := createFinalizationRun(t, ctx, store, sessionID, "hello")
	appendSuccessfulToolEvent(t, ctx, store, runID, "file_edit", `{"path":"README.md"}`)
	appendSuccessfulToolResult(t, ctx, store, runID, sessionID, "call_edit", "file_edit")

	_, err := exec.finishCollectedRun(ctx, runID, "hello", RunState{lastOutput: "hi"}, nil, nil)
	if err != nil {
		t.Fatalf("finishCollectedRun: %v", err)
	}

	raw, err := store.LoadEvents(ctx, runID)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	trace := BuildTrace(&events.RunRecord{RunID: runID}, raw)
	found := false
	for _, item := range trace.Items {
		if item.Kind == stream.StreamKindCrystallizationVerdict {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected crystallization verdict event in trace")
	}
}

func TestCrystallizationEventSinkFailureReturnsError(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	exec := newFinalizationTestExecutor(t, store, cfg)
	svc := stubCrystallizer{crystallize: func(ctx context.Context, req crystallization.CrystallizationRequest) (*crystallization.CrystallizationResult, error) {
		return &crystallization.CrystallizationResult{Verdict: crystallization.VerdictCrystallized, SkillID: "skill-mock", Reason: "test verdict"}, nil
	}}
	exec.SetCrystallizer(svc)

	sessionID := "session-crystal-sink-failure"
	runID := createFinalizationRun(t, ctx, store, sessionID, "edit config")
	appendSuccessfulToolEvent(t, ctx, store, runID, "file_edit", `{"path":"config.yaml"}`)
	appendSuccessfulToolResult(t, ctx, store, runID, sessionID, "call_edit", "file_edit")

	_, err := exec.finishCollectedRun(ctx, runID, "edit config", RunState{lastOutput: "done"}, nil, func(stream.StreamItem) error {
		return errors.New("sink unavailable")
	})
	if err == nil {
		t.Fatal("expected finishCollectedRun to return sink failure")
	}
}

func TestCrystallizationWithArchiveData(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	exec := newFinalizationTestExecutor(t, store, cfg)

	var lastReq crystallization.CrystallizationRequest
	svc := stubCrystallizer{crystallize: func(ctx context.Context, req crystallization.CrystallizationRequest) (*crystallization.CrystallizationResult, error) {
		lastReq = req
		return &crystallization.CrystallizationResult{Verdict: crystallization.VerdictCrystallized, SkillID: "skill-mock"}, nil
	}}
	exec.SetCrystallizer(svc)

	sessionID := "session-archive"
	runID := createFinalizationRun(t, ctx, store, sessionID, "migrate db")
	appendSuccessfulToolEvent(t, ctx, store, runID, "db_migrate", `{"version":"2"}`)
	appendSuccessfulToolEvent(t, ctx, store, runID, "db_verify", `{"path":"migrations/002.sql"}`)
	firstEvidenceRef := appendSuccessfulToolResult(t, ctx, store, runID, sessionID, "call_migrate", "db_migrate")
	secondEvidenceRef := appendSuccessfulToolResult(t, ctx, store, runID, sessionID, "call_verify", "db_verify")

	_, err := exec.finishCollectedRun(ctx, runID, "migrate db", RunState{lastOutput: "migration complete"}, nil, nil)
	if err != nil {
		t.Fatalf("finishCollectedRun: %v", err)
	}
	if len(lastReq.ToolNames) != 2 || lastReq.ToolNames[0] != "db_migrate" {
		t.Fatalf("expected tool names from archive, got %v", lastReq.ToolNames)
	}
	if len(lastReq.TouchedPaths) != 1 || lastReq.TouchedPaths[0] != "migrations/002.sql" {
		t.Fatalf("expected touched paths from archive, got %v", lastReq.TouchedPaths)
	}
	if len(lastReq.EvidenceRefs) != 2 || lastReq.EvidenceRefs[0] != firstEvidenceRef || lastReq.EvidenceRefs[1] != secondEvidenceRef {
		t.Fatalf("expected evidence refs [%q %q], got %v", firstEvidenceRef, secondEvidenceRef, lastReq.EvidenceRefs)
	}
}

func TestCrystallizationFeatureGateDisabled(t *testing.T) {
	ctx := context.Background()
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{})
	if factory.Crystallizer() != nil {
		t.Fatal("expected crystallizer to be nil when feature gate is off")
	}
	exec := newFinalizationTestExecutor(t, store, cfg)

	var called bool
	svc := stubCrystallizer{crystallize: func(ctx context.Context, req crystallization.CrystallizationRequest) (*crystallization.CrystallizationResult, error) {
		called = true
		return &crystallization.CrystallizationResult{Verdict: crystallization.VerdictCrystallized, SkillID: "skill-mock"}, nil
	}}
	exec.SetCrystallizer(svc)

	sessionID := "session-gate"
	runID := createFinalizationRun(t, ctx, store, sessionID, "test gate")
	appendSuccessfulToolEvent(t, ctx, store, runID, "test_tool", `{}`)
	appendSuccessfulToolResult(t, ctx, store, runID, sessionID, "call_test", "test_tool")

	_, err := exec.finishCollectedRun(ctx, runID, "test gate", RunState{lastOutput: "ok"}, nil, nil)
	if err != nil {
		t.Fatalf("finishCollectedRun: %v", err)
	}
	if !called {
		t.Fatal("expected crystallizer to be called when explicitly set on executor")
	}
}

type toolResultAppender interface {
	Append(context.Context, storerepo.ToolResultAppendRequest) (storerepo.ToolResultRecord, error)
}

func appendSuccessfulToolResult(t *testing.T, ctx context.Context, store toolResultAppender, runID, sessionID, callID, toolName string) string {
	t.Helper()
	record, err := store.Append(ctx, storerepo.ToolResultAppendRequest{
		RunID:         runID,
		SessionID:     sessionID,
		CallID:        callID,
		ToolName:      toolName,
		ArgumentsJSON: `{}`,
		Status:        storerepo.ToolResultStatusSucceeded,
		FullText:      "ok",
	})
	if err != nil {
		t.Fatalf("append tool result: %v", err)
	}
	return record.ResultRef
}
