package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"


	"github.com/ycvk/acorn/internal/clientevents"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/store"
	storecore "github.com/ycvk/acorn/internal/store"
)

func TestProjectRunEvent(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	event, err := clientevents.ProjectRunEvent(domain.EventRecord{
		Sequence:  7,
		RunID:     "run_1",
		Kind:      "assistant.delta",
		CreatedAt: now,
		Payload: map[string]any{
			"assistant_delta": map[string]any{
				"delta":      "he",
				"sequence":   float64(1),
				"message_id": "msg_1",
			},
		},
	})
	if err != nil {
		t.Fatalf("clientevents.ProjectRunEvent: %v", err)
	}
	if event.EventID != "run_1:7" || event.Type != "assistant.delta" {
		t.Fatalf("event = %#v", event)
	}
	data, ok := event.Data.(clientevents.AssistantDeltaData)
	if !ok || data.AssistantDelta["delta"] != "he" {
		t.Fatalf("event data = %#v", event.Data)
	}
}

func TestProjectRunEventAcceptsResumeRequested(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	event, err := clientevents.ProjectRunEvent(domain.EventRecord{
		Sequence:  8,
		RunID:     "run_1",
		Kind:      "run.resume_requested",
		CreatedAt: now,
		Payload: map[string]any{
			"targets": map[string]any{
				"agent:coordinator": map[string]any{},
			},
		},
	})
	if err != nil {
		t.Fatalf("clientevents.ProjectRunEvent: %v", err)
	}
	if event.EventID != "run_1:8" || event.Type != "run.resume_requested" {
		t.Fatalf("event = %#v", event)
	}
	data, ok := event.Data.(clientevents.RunResumeRequestedData)
	if !ok {
		t.Fatalf("event data = %T, want clientevents.RunResumeRequestedData", event.Data)
	}
	if _, ok := data.Targets["agent:coordinator"]; !ok {
		t.Fatalf("targets = %#v", data.Targets)
	}
}

func TestProjectRunEventAcceptsDecisionBlocked(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	event, err := clientevents.ProjectRunEvent(domain.EventRecord{
		Sequence:  9,
		RunID:     "run_1",
		Kind:      "decision_blocked",
		CreatedAt: now,
		Payload: map[string]any{
			"action":            "block",
			"decision_reason":   "missing_required_capability",
			"explicit_skill_id": "skill.missing",
		},
	})
	if err != nil {
		t.Fatalf("clientevents.ProjectRunEvent: %v", err)
	}
	if event.Type != "decision_blocked" {
		t.Fatalf("event = %#v", event)
	}
	data, ok := event.Data.(clientevents.DecisionBlockedData)
	if !ok {
		t.Fatalf("event data = %T, want clientevents.DecisionBlockedData", event.Data)
	}
	if data.Action != "block" || data.DecisionReason != "missing_required_capability" || data.ExplicitSkillID != "skill.missing" {
		t.Fatalf("event data = %#v", data)
	}
}

func TestProjectRunEventAcceptsElicitationEvents(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	for _, kind := range []string{"elicitation.pending", "elicitation.decided"} {
		t.Run(kind, func(t *testing.T) {
			event, err := clientevents.ProjectRunEvent(domain.EventRecord{
				Sequence:  11,
				RunID:     "run_1",
				Kind:      kind,
				CreatedAt: now,
				Payload: map[string]any{
					"action_id": "action_1",
					"message":   "Allow Acorn to continue?",
				},
			})
			if err != nil {
				t.Fatalf("clientevents.ProjectRunEvent: %v", err)
			}
			if event.Type != kind {
				t.Fatalf("event type = %q, want %q", event.Type, kind)
			}
			data, ok := event.Data.(clientevents.ElicitationPendingData)
			if !ok {
				t.Fatalf("event data = %T, want clientevents.ElicitationPendingData", event.Data)
			}
			if data.ActionID != "action_1" || data.Message != "Allow Acorn to continue?" {
				t.Fatalf("event data = %#v", data)
			}
		})
	}
}

func TestProjectRunEventRejectsDiagnosticOnlyKinds(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	for _, kind := range []string{
		"tool.call.started",
		"tool.call.progress",
		"tool.call.succeeded",
		"tool.call.failed",
		"decision_selected",
		"skill.selected",
		"skill.lifecycle",
		"procedure.activation",
		"memory.prepared",
		"subagent.failed",
	} {
		t.Run(kind, func(t *testing.T) {
			_, err := clientevents.ProjectRunEvent(domain.EventRecord{
				Sequence:  12,
				RunID:     "run_1",
				Kind:      kind,
				CreatedAt: now,
				Payload:   map[string]any{"value": "diagnostic"},
			})
			if !errors.Is(err, clientevents.ErrProjectionFailed) {
				t.Fatalf("error = %v, want ErrProjectionFailed", err)
			}
		})
	}
}

func TestProjectRunEventAcceptsOperatorQuestionEvents(t *testing.T) {
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	event, err := clientevents.ProjectRunEvent(domain.EventRecord{
		Sequence:  12,
		RunID:     "run_1",
		Kind:      "operator_question.decided",
		CreatedAt: now,
		Payload: map[string]any{
			"action_id":          "action_1",
			"question":           "Which path?",
			"decision":           "answer",
			"selected_option_id": "fast",
			"answer":             "Ship it",
			"allow_freeform":     true,
			"options": []any{
				map[string]any{"id": "fast", "label": "Fast path"},
			},
		},
	})
	if err != nil {
		t.Fatalf("clientevents.ProjectRunEvent: %v", err)
	}
	data, ok := event.Data.(clientevents.OperatorQuestionData)
	if !ok {
		t.Fatalf("event data = %T, want clientevents.OperatorQuestionData", event.Data)
	}
	if data.ActionID != "action_1" || data.Decision != "answer" || data.SelectedOptionID != "fast" || data.Answer != "Ship it" || len(data.Options) != 1 {
		t.Fatalf("event data = %#v", data)
	}
}

func TestProjectRunEventRejectsNonObjectPayload(t *testing.T) {
	_, err := clientevents.ProjectRunEvent(domain.EventRecord{
		Sequence: 1,
		RunID:    "run_1",
		Kind:     "assistant.delta",
		Payload:  []any{"not", "object"},
	})
	if !errors.Is(err, clientevents.ErrProjectionFailed) {
		t.Fatalf("error = %v, want clientevents.ErrProjectionFailed", err)
	}
}

func TestProjectRunEventRejectsUnsupportedLiveKind(t *testing.T) {
	_, err := clientevents.ProjectRunEvent(domain.EventRecord{
		Sequence:  1,
		RunID:     "run_1",
		Kind:      "future.kind",
		CreatedAt: time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
		Payload:   map[string]any{"value": "debug"},
	})
	if !errors.Is(err, clientevents.ErrProjectionFailed) {
		t.Fatalf("error = %v, want clientevents.ErrProjectionFailed", err)
	}
}

func TestLoadRunEventsAfterFiltersDiagnosticsAndAdvancesCursor(t *testing.T) {
	ctx := context.Background()
	store, err := store.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateRun(ctx, domain.RunCreateParams{RunID: "run_live", Input: "input"}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if _, err := store.AppendEvent(ctx, "run_live", "run.started", map[string]any{"input": "hello"}); err != nil {
		t.Fatalf("append run.started: %v", err)
	}
	if _, err := store.AppendEvent(ctx, "run_live", "tool.call.progress", map[string]any{"delta": "hidden"}); err != nil {
		t.Fatalf("append tool.call.progress: %v", err)
	}
	if _, err := store.AppendEvent(ctx, "run_live", "memory.prepared", map[string]any{"memory_prepared": map[string]any{"entry_count": float64(2)}}); err != nil {
		t.Fatalf("append memory.prepared: %v", err)
	}
	if _, err := store.AppendEvent(ctx, "run_live", "assistant.delta", map[string]any{"assistant_delta": map[string]any{"delta": "hi"}}); err != nil {
		t.Fatalf("append assistant.delta: %v", err)
	}
	if _, err := store.AppendEvent(ctx, "run_live", "skill.selected", map[string]any{"skill": map[string]any{"selected_id": "skill.hidden"}}); err != nil {
		t.Fatalf("append skill.selected: %v", err)
	}

	service := NewEventService(store)
	batch, err := service.LoadRunEventsAfter(ctx, "run_live", 1)
	if err != nil {
		t.Fatalf("LoadRunEventsAfter: %v", err)
	}
	if batch.CursorSeq != 5 {
		t.Fatalf("cursor = %d, want 5", batch.CursorSeq)
	}
	if len(batch.Events) != 1 || batch.Events[0].Type != "assistant.delta" {
		t.Fatalf("events = %#v", batch.Events)
	}

	batch, err = service.LoadRunEventsAfter(ctx, "run_live", 2)
	if err != nil {
		t.Fatalf("LoadRunEventsAfter after diagnostic: %v", err)
	}
	if batch.CursorSeq != 5 || len(batch.Events) != 1 || batch.Events[0].Seq != 4 {
		t.Fatalf("batch after diagnostic = %#v", batch)
	}

	batch, err = service.LoadRunEventsAfter(ctx, "run_live", 4)
	if err != nil {
		t.Fatalf("LoadRunEventsAfter after live event: %v", err)
	}
	if batch.CursorSeq != 5 || len(batch.Events) != 0 {
		t.Fatalf("diagnostic-only batch = %#v", batch)
	}
}

func TestLoadRunEventsForDetailFiltersDiagnostics(t *testing.T) {
	ctx := context.Background()
	store, err := store.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateRun(context.Background(), domain.RunCreateParams{RunID: "run_detail", Input: "input"}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if _, err := store.AppendEvent(context.Background(), "run_detail", "run.started", map[string]any{"input": "hello"}); err != nil {
		t.Fatalf("append run.started: %v", err)
	}
	if _, err := store.AppendEvent(context.Background(), "run_detail", "future.kind", map[string]any{"value": "debug"}); err != nil {
		t.Fatalf("append future.kind: %v", err)
	}
	if _, err := store.AppendEvent(context.Background(), "run_detail", "skill.selected", map[string]any{"skill": map[string]any{"selected_id": "skill.ship.patch"}}); err != nil {
		t.Fatalf("append skill.selected: %v", err)
	}
	service := NewEventService(store)
	detail, err := service.LoadRunEventsForDetail(ctx, "run_detail")
	if err != nil {
		t.Fatalf("LoadRunEventsForDetail: %v", err)
	}
	if len(detail.Events) != 1 || detail.Events[0].Type != "run.started" {
		t.Fatalf("events = %#v", detail.Events)
	}
}

func TestClientServiceListRunArtifactsUsesRunScopedStorePort(t *testing.T) {
	ctx := context.Background()
	store, err := store.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateRun(ctx, domain.RunCreateParams{RunID: "run_artifacts", Input: "input"}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	_, err = store.SaveArtifact(ctx, domain.ArtifactRecord{
		ArtifactID:          "artifact_report",
		RunID:               "run_artifacts",
		SessionID:           "thread_artifacts",
		SourceToolResultRef: "tool_result:run_artifacts:call_1",
		Kind:                string(storecore.ArtifactKindMarkdown),
		Title:               "Report",
		MIMEType:            "text/markdown",
		SizeBytes:           42,
		SHA256:              "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RelativePath:        "run_artifacts/artifact_report",
		CreatedAt:           time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("SaveArtifact: %v", err)
	}

	service := NewEventService(store)
	artifacts, err := service.ListRunArtifacts(ctx, "run_artifacts")
	if err != nil {
		t.Fatalf("ListRunArtifacts: %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].ArtifactID != "artifact_report" || artifacts[0].SourceToolResultRef != "tool_result:run_artifacts:call_1" {
		t.Fatalf("artifacts = %#v", artifacts)
	}
}

// toClientTestJSON marshals a value to JSON for substring assertions in tests.
func toClientTestJSON(value any) string {
	body, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(body)
}

// mustParseMessageID parses a numeric message id string, failing the test on error.
func mustParseMessageID(t *testing.T, value string) int64 {
	t.Helper()
	var id int64
	if _, err := fmt.Sscanf(value, "%d", &id); err != nil {
		t.Fatalf("parse message id %q: %v", value, err)
	}
	return id
}
