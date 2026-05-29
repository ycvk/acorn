package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	storecore "github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/stream"

	"github.com/ycvk/acorn/internal/clientevents"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/runtime"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	storesqlite "github.com/ycvk/acorn/internal/store/sqlite"
)

func TestProjectThread(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	service := BuildClientService(nil, nil, nil, "/repo")

	thread, err := service.projectThread(events.SessionRecord{
		SessionID: "session_1",
		Title:     "Inspect repo",
		CreatedAt: now,
		UpdatedAt: now,
	}, &events.RunRecord{
		RunID:     "run_1",
		Status:    events.RunStatusSucceeded,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("projectThread: %v", err)
	}
	if thread.ID != "session_1" || thread.WorkspaceRoot != "/repo" || thread.LatestRunID != "run_1" || thread.State != "completed" {
		t.Fatalf("thread = %#v", thread)
	}
}

func TestClientCreateMessageBackfillsEmptyThreadTitle(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	service := BuildClientService(store, nil, nil, "/repo")
	service.newThreadID = func() string { return "thread_title" }

	thread, err := service.CreateThread(ctx, "")
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	if _, err := service.CreateMessage(ctx, thread.ID, "  Investigate the mobile thread list\nand fix titles  "); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}

	updated, err := service.GetThread(ctx, thread.ID)
	if err != nil {
		t.Fatalf("GetThread: %v", err)
	}
	if updated.Title != "Investigate the mobile thread list and fix titles" {
		t.Fatalf("title = %q", updated.Title)
	}
}

func TestClientListThreadsProjectsTitleFromRecentUserMessage(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	session, err := store.CreateSession(ctx, "legacy_thread", "")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := store.AppendSessionMessage(session.SessionID, 1, "user", "How do I configure pairing on the VPS?", ""); err != nil {
		t.Fatalf("AppendSessionMessage: %v", err)
	}
	service := BuildClientService(store, nil, nil, "/repo")

	threads, err := service.ListThreads(ctx, 10)
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("threads = %#v", threads)
	}
	if threads[0].Title != "How do I configure pairing on the VPS?" {
		t.Fatalf("title = %q", threads[0].Title)
	}
}

func TestGeneratedThreadTitleTruncatesLongText(t *testing.T) {
	title := generatedThreadTitle(strings.Repeat("a", generatedThreadTitleMaxRunes+1))
	if got := len([]rune(title)); got != generatedThreadTitleMaxRunes+3 {
		t.Fatalf("title rune len = %d, want %d", got, generatedThreadTitleMaxRunes+3)
	}
	if !strings.HasSuffix(title, "...") {
		t.Fatalf("title = %q, want ellipsis suffix", title)
	}
}

func TestProjectMessage(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	message, err := projectMessage(events.SessionMessageRecord{
		ID:        42,
		SessionID: "session_1",
		Role:      "user",
		Content:   "hello",
		RunID:     "run_1",
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("projectMessage: %v", err)
	}
	if message.ID != "42" || message.ThreadID != "session_1" || message.Content.Type != "text" || message.Content.Text != "hello" {
		t.Fatalf("message = %#v", message)
	}
}

func TestProjectMessageParts(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	message, err := projectMessage(events.SessionMessageRecord{
		ID:        42,
		SessionID: "session_1",
		Role:      "assistant",
		Content:   "done",
		ContentParts: json.RawMessage(`[
			{"kind":"text","text":"done"},
			{"kind":"result","title":"Task completed","changed":[],"verified":[],"risks":[]},
			{"kind":"technical_detail_link","run_id":"run_1","label":"View technical details"}
		]`),
		RunID:     "run_1",
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("projectMessage: %v", err)
	}
	if len(message.Content.Parts) != 3 {
		t.Fatalf("parts length = %d, want 3: %#v", len(message.Content.Parts), message.Content.Parts)
	}
	if message.Content.Parts[1].Kind != "result" || message.Content.Parts[2].RunID != "run_1" {
		t.Fatalf("message parts = %#v", message.Content.Parts)
	}
}

func TestProjectMessagePartsAcceptsDisclosure(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	message, err := projectMessage(events.SessionMessageRecord{
		ID:        44,
		SessionID: "session_1",
		Role:      "assistant",
		Content:   "done",
		ContentParts: json.RawMessage(`[
			{"kind":"text","text":"done"},
			{"kind":"disclosure","items":[
				{"kind":"memory","label":"Used 2 learned context notes","detail":"Known pattern","tone":"memory"},
				{"kind":"skill","label":"Used skill","detail":"Repository patch shipping","tone":"skill","skill_id":"skill.ship.patch"},
				{"kind":"skill","label":"Used procedure","detail":"SQLite Rows Error Handling SOP","tone":"procedure","skill_id":"sop.fix-sqlite-rows"}
			]},
			{"kind":"result","title":"Task completed","changed":[],"verified":[],"risks":[]}
		]`),
		RunID:     "run_1",
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("projectMessage: %v", err)
	}
	if len(message.Content.Parts) != 3 {
		t.Fatalf("parts length = %d, want 3: %#v", len(message.Content.Parts), message.Content.Parts)
	}
	disclosure := message.Content.Parts[1]
	if disclosure.Kind != "disclosure" || len(disclosure.Items) != 3 {
		t.Fatalf("disclosure part = %#v", disclosure)
	}
	if disclosure.Items[0].Kind != "memory" || disclosure.Items[1].Kind != "skill" || disclosure.Items[2].Tone != "procedure" {
		t.Fatalf("disclosure order = %#v", disclosure.Items)
	}
	if disclosure.Items[1].SkillID != "skill.ship.patch" || disclosure.Items[2].SkillID != "sop.fix-sqlite-rows" {
		t.Fatalf("disclosure skill ids = %#v", disclosure.Items)
	}
}

func TestProjectMessagePartsRejectsInvalidDisclosure(t *testing.T) {
	_, err := projectMessage(events.SessionMessageRecord{
		ID:           45,
		SessionID:    "session_1",
		Role:         "assistant",
		Content:      "done",
		ContentParts: json.RawMessage(`[{"kind":"disclosure","items":[]}]`),
		CreatedAt:    time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, ErrClientProjectionFailed) {
		t.Fatalf("error = %v, want ErrClientProjectionFailed", err)
	}
}

func TestProjectMessagePartsAcceptsWorkStatusAction(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	message, err := projectMessage(events.SessionMessageRecord{
		ID:        43,
		SessionID: "session_1",
		Role:      "assistant",
		Content:   "Acorn paused before continuing.",
		ContentParts: json.RawMessage(`[
			{"kind":"work_status","status":"interrupted","title":"Paused before continuing","summary":"Acorn paused at a real interrupt. Resume this run only when you want to continue the same execution.","detail_run_id":"run_1","action":{"kind":"resume_run","run_id":"run_1","label":"Resume"}},
			{"kind":"technical_detail_link","run_id":"run_1","label":"View technical details"}
		]`),
		RunID:     "run_1",
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("projectMessage: %v", err)
	}
	if len(message.Content.Parts) != 2 {
		t.Fatalf("parts length = %d, want 2: %#v", len(message.Content.Parts), message.Content.Parts)
	}
	if message.Content.Parts[0].Action == nil || message.Content.Parts[0].Action.Kind != "resume_run" {
		t.Fatalf("work status action = %#v", message.Content.Parts[0].Action)
	}
}

func TestProjectMessagePartsRejectUnknownKind(t *testing.T) {
	_, err := projectMessage(events.SessionMessageRecord{
		ID:           42,
		SessionID:    "session_1",
		Role:         "assistant",
		Content:      "done",
		ContentParts: json.RawMessage(`[{"kind":"debug","text":"hidden"}]`),
		CreatedAt:    time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, ErrClientProjectionFailed) {
		t.Fatalf("error = %v, want ErrClientProjectionFailed", err)
	}
}

func TestProjectRunMapsStatusAndMode(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	run, err := projectRun(events.RunRecord{
		RunID:             "run_1",
		SessionID:         "session_1",
		Status:            events.RunStatusSucceeded,
		OrchestrationMode: events.ModeSingleAgent,
		CreatedAt:         now,
		UpdatedAt:         now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("projectRun: %v", err)
	}
	if run.ID != "run_1" || run.ThreadID != "session_1" || run.Status != "completed" || run.Mode != "agent" || run.CompletedAt.IsZero() {
		t.Fatalf("run = %#v", run)
	}
}

func TestProjectRunRejectsUnknownStatusAndMode(t *testing.T) {
	_, err := projectRun(events.RunRecord{
		RunID:             "run_bad_status",
		Status:            events.RunStatus(""),
		OrchestrationMode: events.ModeSingleAgent,
	})
	if !errors.Is(err, ErrClientProjectionFailed) {
		t.Fatalf("status error = %v, want ErrClientProjectionFailed", err)
	}

	_, err = projectRun(events.RunRecord{
		RunID:             "run_bad_mode",
		Status:            events.RunStatusRunning,
		OrchestrationMode: events.OrchestrationMode("unknown"),
	})
	if !errors.Is(err, ErrClientProjectionFailed) {
		t.Fatalf("mode error = %v, want ErrClientProjectionFailed", err)
	}
}

func TestProjectRunEvent(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	event, err := clientevents.ProjectRunEvent(events.EventRecord{
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
	event, err := clientevents.ProjectRunEvent(events.EventRecord{
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

func TestProjectRunEventAcceptsDecisionEvents(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		kind string
		data any
	}{
		{
			name: "selected",
			kind: "decision_selected",
			data: clientevents.DecisionSelectedData{
				Action:          "execute_with_skill",
				SelectedSkillID: "skill.ship.patch",
			},
		},
		{
			name: "blocked",
			kind: "decision_blocked",
			data: clientevents.DecisionBlockedData{
				Action:         "ask_user",
				DecisionReason: "operator confirmation required",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event, err := clientevents.ProjectRunEvent(events.EventRecord{
				Sequence:  9,
				RunID:     "run_1",
				Kind:      tc.kind,
				CreatedAt: now,
				Payload: map[string]any{
					"action":            "execute_with_skill",
					"intent":            "implement",
					"selected_skill_id": "skill.ship.patch",
					"decision_reason":   "route matched",
					"explicit_skill_id": "skill.ship.patch",
				},
			})
			if err != nil {
				t.Fatalf("clientevents.ProjectRunEvent: %v", err)
			}
			if event.Type != tc.kind || event.Data == nil {
				t.Fatalf("event = %#v", event)
			}
			if fmt.Sprintf("%T", event.Data) != fmt.Sprintf("%T", tc.data) {
				t.Fatalf("data type = %T, want %T", event.Data, tc.data)
			}
		})
	}
}

func TestProjectRunEventAcceptsElicitationEvents(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	for _, kind := range []string{"elicitation.pending", "elicitation.decided"} {
		t.Run(kind, func(t *testing.T) {
			event, err := clientevents.ProjectRunEvent(events.EventRecord{
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

func TestProjectRunEventAcceptsSkillEvents(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	for _, kind := range []string{"skill.discovered", "skill.selected", "skill.loaded", "skill.failed"} {
		t.Run(kind, func(t *testing.T) {
			event, err := clientevents.ProjectRunEvent(events.EventRecord{
				Sequence:  10,
				RunID:     "run_1",
				Kind:      kind,
				CreatedAt: now,
				Payload: map[string]any{
					"skill": map[string]any{
						"selected_id": "skill.ship.patch",
						"name":        "skill.ship.patch",
					},
				},
			})
			if err != nil {
				t.Fatalf("clientevents.ProjectRunEvent: %v", err)
			}
			data, ok := event.Data.(clientevents.SkillData)
			if !ok || data.Skill["selected_id"] != "skill.ship.patch" {
				t.Fatalf("event data = %#v", event.Data)
			}
		})
	}
}

func TestProjectRunEventAcceptsSkillLifecycle(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	event, err := clientevents.ProjectRunEvent(events.EventRecord{
		Sequence:  11,
		RunID:     "run_1",
		Kind:      "skill.lifecycle",
		CreatedAt: now,
		Payload: map[string]any{
			"skill_lifecycle": map[string]any{
				"skill_id":         "skill.generated",
				"action":           "assessed",
				"status":           "verified",
				"verdict":          "verified",
				"reason":           "durable evidence-backed promotion",
				"assessment_id":    "skill_assessment_1",
				"changes_required": []any{"none"},
				"applied":          true,
				"assessment": map[string]any{
					"assessment_id": "skill_assessment_1",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("clientevents.ProjectRunEvent: %v", err)
	}
	data, ok := event.Data.(clientevents.SkillLifecycleData)
	if !ok {
		t.Fatalf("event data = %T, want clientevents.SkillLifecycleData", event.Data)
	}
	if data.SkillLifecycle["skill_id"] != "skill.generated" || data.SkillLifecycle["action"] != "assessed" {
		t.Fatalf("event data = %#v", event.Data)
	}
}

func TestProjectRunEventAcceptsProcedureActivation(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	event, err := clientevents.ProjectRunEvent(events.EventRecord{
		Sequence:  12,
		RunID:     "run_1",
		Kind:      "procedure.activation",
		CreatedAt: now,
		Payload: map[string]any{
			"procedure_activation": map[string]any{
				"procedure_ref": "skills/learned/sqlite.md#sqlite",
				"phase":         "injected",
				"reason":        "injected_into_memory_context",
			},
		},
	})
	if err != nil {
		t.Fatalf("clientevents.ProjectRunEvent: %v", err)
	}
	data, ok := event.Data.(clientevents.ProcedureActivationData)
	if !ok {
		t.Fatalf("event data = %T, want clientevents.ProcedureActivationData", event.Data)
	}
	if data.ProcedureActivation["phase"] != "injected" {
		t.Fatalf("event data = %#v", event.Data)
	}
}

func TestProjectRunEventAcceptsContextEvents(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	pressure, err := clientevents.ProjectRunEvent(events.EventRecord{
		Sequence:  13,
		RunID:     "run_1",
		Kind:      "context.pressure",
		CreatedAt: now,
		Payload: map[string]any{
			"context_pressure": map[string]any{
				"state":                         "warning",
				"estimated_input_tokens":        float64(12000),
				"effective_window_tokens":       float64(16000),
				"warning_threshold_tokens":      float64(11000),
				"auto_compact_threshold_tokens": float64(13000),
				"blocking_threshold_tokens":     float64(15000),
				"percent_used":                  float64(75),
			},
		},
	})
	if err != nil {
		t.Fatalf("clientevents.ProjectRunEvent context.pressure: %v", err)
	}
	pressureData, ok := pressure.Data.(clientevents.ContextPressureData)
	if !ok {
		t.Fatalf("pressure data = %T, want clientevents.ContextPressureData", pressure.Data)
	}
	if pressureData.ContextPressure["state"] != "warning" {
		t.Fatalf("pressure data = %#v", pressure.Data)
	}

	compressed, err := clientevents.ProjectRunEvent(events.EventRecord{
		Sequence:  14,
		RunID:     "run_1",
		Kind:      "context.compressed",
		CreatedAt: now,
		Payload: map[string]any{
			"context_compressed": map[string]any{
				"boundary_id":     "ctxb_run_1_0001",
				"first_index":     float64(2),
				"last_index":      float64(8),
				"tokens_before":   float64(12000),
				"tokens_after":    float64(4000),
				"summary_snippet": "User asked about the repo.",
			},
		},
	})
	if err != nil {
		t.Fatalf("clientevents.ProjectRunEvent context.compressed: %v", err)
	}
	compressedData, ok := compressed.Data.(clientevents.ContextCompressedData)
	if !ok {
		t.Fatalf("compressed data = %T, want clientevents.ContextCompressedData", compressed.Data)
	}
	if compressedData.ContextCompressed["boundary_id"] != "ctxb_run_1_0001" {
		t.Fatalf("compressed data = %#v", compressed.Data)
	}
}

func TestProjectRunEventAcceptsPlanEvents(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	cases := []string{"plan.created", "plan.updated", "plan.cleared", "step.started", "step.completed", "step.failed"}
	for _, kind := range cases {
		t.Run(kind, func(t *testing.T) {
			event, err := clientevents.ProjectRunEvent(events.EventRecord{
				Sequence:  11,
				RunID:     "run_1",
				Kind:      kind,
				CreatedAt: now,
				Payload: map[string]any{
					"plan_id":    "plan_1",
					"session_id": "thread_1",
					"plan":       map[string]any{"plan_id": "plan_1"},
					"step":       map[string]any{"id": "s1", "title": "Do it"},
					"updated_at": "2026-05-02T10:00:00Z",
					"error":      "boom",
				},
			})
			if err != nil {
				t.Fatalf("clientevents.ProjectRunEvent: %v", err)
			}
			if event.Type != kind || event.Data == nil {
				t.Fatalf("event = %#v", event)
			}
		})
	}
}

func TestProjectRunEventAcceptsSubagentEvents(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	for _, kind := range []string{"subagent.started", "subagent.completed", "subagent.failed"} {
		t.Run(kind, func(t *testing.T) {
			event, err := clientevents.ProjectRunEvent(events.EventRecord{
				Sequence:  12,
				RunID:     "run_1",
				Kind:      kind,
				CreatedAt: now,
				Payload: map[string]any{
					"sub_run_id":         "sub_1",
					"parent_id":          "run_1",
					"session_id":         "thread_1",
					"depth":              float64(1),
					"task":               "inspect",
					"child_run_mode":     "fork",
					"workspace_mode":     "worktree",
					"worktree_path":      "/tmp/acorn-child",
					"context_messages":   float64(3),
					"summary":            "done",
					"final_status":       "completed",
					"acceptance_status":  "accepted",
					"acceptance_reasons": []any{"ok"},
					"evidence_refs":      []any{"run:sub_1", "evidence:e1"},
					"orchestration_mode": "single_agent",
					"parent_step_id":     "step_1",
					"error":              "boom",
				},
			})
			if err != nil {
				t.Fatalf("clientevents.ProjectRunEvent: %v", err)
			}
			data, ok := event.Data.(clientevents.SubagentData)
			if !ok || data.SubRunID != "sub_1" || data.Depth != 1 {
				t.Fatalf("event data = %#v", event.Data)
			}
			if data.ChildRunMode != "fork" ||
				data.WorkspaceMode != "worktree" ||
				data.WorktreePath != "/tmp/acorn-child" ||
				data.ContextMessages != 3 ||
				data.OrchestrationMode != "single_agent" ||
				data.ParentStepID != "step_1" {
				t.Fatalf("event data = %#v", event.Data)
			}
			if !reflect.DeepEqual(data.EvidenceRefs, []string{"run:sub_1", "evidence:e1"}) {
				t.Fatalf("evidence refs = %#v", data.EvidenceRefs)
			}
		})
	}
}

func TestProjectRunEventAcceptsOperatorQuestionEvents(t *testing.T) {
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	event, err := clientevents.ProjectRunEvent(events.EventRecord{
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

func TestProjectRunEventAcceptsOperationalEvents(t *testing.T) {
	now := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)

	provider, err := clientevents.ProjectRunEvent(events.EventRecord{
		Sequence:  13,
		RunID:     "run_1",
		Kind:      "provider.degraded",
		CreatedAt: now,
		Payload: map[string]any{
			"affected_providers": []any{
				map[string]any{"name": "mcp.remote", "transport": "streamable_http", "error": "dial refused"},
			},
		},
	})
	if err != nil {
		t.Fatalf("provider.degraded projection: %v", err)
	}
	providerData, ok := provider.Data.(clientevents.ProviderDegradedData)
	if !ok || len(providerData.AffectedProviders) != 1 || providerData.AffectedProviders[0].Name != "mcp.remote" {
		t.Fatalf("provider.degraded data = %#v", provider.Data)
	}

	mcpKinds := []string{
		"mcp.tool_catalog_refreshed",
		"mcp.tool_catalog_refresh_failed",
		"mcp.provider_added",
		"mcp.provider_removed",
		"mcp.provider_restarted",
		"mcp.resource_catalog_refreshed",
		"mcp.resource_catalog_refresh_failed",
		"mcp.prompt_catalog_refreshed",
		"mcp.prompt_catalog_refresh_failed",
		"mcp.auth_status_changed",
	}
	for _, kind := range mcpKinds {
		t.Run(kind, func(t *testing.T) {
			event, err := clientevents.ProjectRunEvent(events.EventRecord{
				Sequence:  14,
				RunID:     "run_1",
				Kind:      kind,
				CreatedAt: now,
				Payload: map[string]any{
					"provider_name": "github",
					"transport":     "sse",
					"error":         "refresh failed",
					"auth_status":   "reauth_required",
				},
			})
			if err != nil {
				t.Fatalf("%s projection: %v", kind, err)
			}
			data, ok := event.Data.(clientevents.MCPProviderLifecycleData)
			if !ok || data.ProviderName != "github" || data.AuthStatus != "reauth_required" {
				t.Fatalf("%s data = %#v", kind, event.Data)
			}
		})
	}

	for _, kind := range []string{"sampling.started", "sampling.completed", "sampling.failed"} {
		t.Run(kind, func(t *testing.T) {
			event, err := clientevents.ProjectRunEvent(events.EventRecord{
				Sequence:  15,
				RunID:     "run_1",
				Kind:      kind,
				CreatedAt: now,
				Payload: map[string]any{
					"run_id": "subagent_pending",
					"depth":  float64(1),
					"model":  "acorn-default",
				},
			})
			if err != nil {
				t.Fatalf("%s projection: %v", kind, err)
			}
			data, ok := event.Data.(clientevents.SamplingData)
			if !ok || data.RunID != "subagent_pending" || data.Depth != 1 {
				t.Fatalf("%s data = %#v", kind, event.Data)
			}
		})
	}
}

func TestProjectRunEventRejectsNonObjectPayload(t *testing.T) {
	_, err := clientevents.ProjectRunEvent(events.EventRecord{
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
	_, err := clientevents.ProjectRunEvent(events.EventRecord{
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

func TestProjectUnsupportedRunEventPreservesRawPayload(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	event := clientevents.ProjectUnsupportedRunEvent(events.EventRecord{
		Sequence:  7,
		RunID:     "run_1",
		Kind:      "future.kind",
		CreatedAt: now,
		Payload:   map[string]any{"value": "debug"},
	})
	if event.EventID != "run_1:7" || event.Type != "future.kind" || event.Raw["value"] != "debug" || event.Reason == "" {
		t.Fatalf("unsupported event = %#v", event)
	}
}

func TestLoadRunEventsForDetailSeparatesUnsupportedEvents(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateRun(context.Background(), "run_detail", "input", "thread_detail"); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if _, err := store.AppendEventContext(context.Background(), "run_detail", "run.started", map[string]any{"input": "hello"}); err != nil {
		t.Fatalf("append run.started: %v", err)
	}
	if _, err := store.AppendEventContext(context.Background(), "run_detail", "future.kind", map[string]any{"value": "debug"}); err != nil {
		t.Fatalf("append future.kind: %v", err)
	}
	service := BuildClientService(store, nil, nil, "/repo")
	detail, err := service.LoadRunEventsForDetail(ctx, "run_detail")
	if err != nil {
		t.Fatalf("LoadRunEventsForDetail: %v", err)
	}
	if len(detail.Events) != 1 || detail.Events[0].Type != "run.started" {
		t.Fatalf("events = %#v", detail.Events)
	}
	if len(detail.Unsupported) != 1 || detail.Unsupported[0].Type != "future.kind" || detail.Unsupported[0].Raw["value"] != "debug" {
		t.Fatalf("unsupported = %#v", detail.Unsupported)
	}
	if detail.Trace == nil || detail.Trace.ItemCount == 0 {
		t.Fatalf("trace summary = %#v, want non-empty", detail.Trace)
	}
}

func TestClientCreateRunUsesRealExecutorPath(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	modelServer := newClientOpenAITestServer(t, "client runtime answer")
	cfg := clientRuntimeTestConfig(t, modelServer.URL+"/v1")
	runnerFactory, err := runtime.NewRunnerFactory(cfg, store, runtime.RunnerFactoryOptions{
		MemoryModule:              newClientRuntimeMemoryModule(t, cfg),
		ChildAgentExecutorFactory: runtime.NewSubagentExecutorFactory(cfg, store, nil),
	})
	if err != nil {
		t.Fatalf("NewRunnerFactory: %v", err)
	}
	executor, err := runtime.NewExecutorWithRunRuntimeAndController(cfg, store, runnerFactory, nil)
	if err != nil {
		t.Fatalf("NewExecutorWithRunRuntimeAndController: %v", err)
	}

	service := BuildClientService(store, func(context.Context) (executorHandle, error) {
		return executor, nil
	}, nil, cfg.WorkspaceRoot())
	service.newThreadID = func() string { return "thread_runtime" }
	service.newRunID = func() string { return "run_runtime" }

	thread, err := service.CreateThread(ctx, "runtime")
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	message, err := service.CreateMessage(ctx, thread.ID, "hello")
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}

	run, err := service.CreateRun(ctx, thread.ID, "", "")
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run.ID != "run_runtime" || run.ThreadID != thread.ID || run.Status != "running" || run.Mode != "direct" {
		t.Fatalf("created run = %#v", run)
	}

	waitForRunStatus(t, store, "run_runtime", events.RunStatusSucceeded)
	records, err := store.LoadEvents(ctx, "run_runtime")
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(records) < 3 {
		t.Fatalf("events = %#v, want lifecycle events from executor", records)
	}
	var sawStarted, sawMessage, sawCompleted bool
	for _, record := range records {
		switch record.Kind {
		case "run.started":
			sawStarted = true
		case "agent.message":
			sawMessage = strings.Contains(strings.TrimSpace(toClientTestJSON(record.Payload)), "client runtime answer")
		case "run.completed":
			sawCompleted = true
		}
	}
	if !sawStarted || !sawMessage || !sawCompleted {
		t.Fatalf("missing expected runtime events: started=%v message=%v completed=%v records=%#v", sawStarted, sawMessage, sawCompleted, records)
	}
	items, err := store.ListSessionMessages(ctx, thread.ID, 10)
	if err != nil {
		t.Fatalf("ListSessionMessages: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("session messages = %#v, want user + assistant", items)
	}
	if items[0].ID != mustParseMessageID(t, message.ID) || items[0].RunID != "run_runtime" {
		t.Fatalf("user message was not bound to run: %#v", items[0])
	}
	if items[1].Role != "assistant" || items[1].RunID != "run_runtime" || !strings.Contains(items[1].Content, "client runtime answer") {
		t.Fatalf("assistant message was not synced from run: %#v", items[1])
	}
}

func TestClientCreateRunReturnsExecutionNotReady(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	service := BuildClientService(store, func(context.Context) (executorHandle, error) {
		return nil, runtimeapi.ErrExecutionNotReady
	}, nil, "/repo")
	service.newThreadID = func() string { return "thread_not_ready" }
	service.newRunID = func() string { return "run_not_ready" }

	thread, err := service.CreateThread(ctx, "not ready")
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	if _, err := service.CreateMessage(ctx, thread.ID, "hello"); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	_, err = service.CreateRun(ctx, thread.ID, "", "")
	if !errors.Is(err, runtimeapi.ErrExecutionNotReady) {
		t.Fatalf("CreateRun error = %v, want ErrExecutionNotReady", err)
	}
	if _, loadErr := store.LoadRun(ctx, "run_not_ready"); !errors.Is(loadErr, storecore.ErrRunNotFound) {
		t.Fatalf("LoadRun after execution-not-ready = %v, want ErrRunNotFound", loadErr)
	}
}

func TestClientCreateRunReportsPostStartPersistenceFailure(t *testing.T) {
	ctx := context.Background()
	db, err := storesqlite.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = db.Close()
		}
	})

	exec := &postStartFailingExecutor{
		store:   db,
		release: make(chan struct{}),
	}
	service := BuildClientService(db, func(context.Context) (executorHandle, error) {
		return exec, nil
	}, nil, "/repo")
	service.newThreadID = func() string { return "thread_post_start_failure" }
	service.newRunID = func() string { return "run_post_start_failure" }
	reported := make(chan error, 1)
	service.reportError = func(_ context.Context, runID string, err error) {
		if runID != "run_post_start_failure" {
			t.Errorf("reported run id = %q", runID)
		}
		reported <- err
	}

	thread, err := service.CreateThread(ctx, "post-start failure")
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	if _, err := service.CreateMessage(ctx, thread.ID, "hello"); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	run, err := service.CreateRun(ctx, thread.ID, "", "")
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run.ID != "run_post_start_failure" || run.Status != "running" {
		t.Fatalf("created run = %#v", run)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	closed = true
	close(exec.release)

	select {
	case err := <-reported:
		if err == nil || !strings.Contains(err.Error(), "record started client run failure") {
			t.Fatalf("reported error = %v, want persistence failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for background failure report")
	}
}

func TestClientCreateRunRejectsInvalidModes(t *testing.T) {
	tests := []struct {
		name string
		mode string
	}{
		{name: "unknown mode", mode: "workflow"},
		{name: "single agent is internal only", mode: "single_agent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := storesqlite.Open(filepath.Join(t.TempDir(), "state"))
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = store.Close() })

			service := BuildClientService(store, func(context.Context) (executorHandle, error) {
				t.Fatal("executor factory should not be called for invalid mode")
				return nil, nil
			}, nil, "/repo")
			service.newThreadID = func() string { return "thread_invalid_mode" }
			service.newRunID = func() string { return "run_invalid_mode" }

			thread, err := service.CreateThread(ctx, "invalid mode")
			if err != nil {
				t.Fatalf("CreateThread: %v", err)
			}
			if _, err := service.CreateMessage(ctx, thread.ID, "hello"); err != nil {
				t.Fatalf("CreateMessage: %v", err)
			}
			_, err = service.CreateRun(ctx, thread.ID, "", tt.mode)
			if !errors.Is(err, ErrClientInvalidRunMode) {
				t.Fatalf("CreateRun error = %v, want ErrClientInvalidRunMode", err)
			}
			if _, loadErr := store.LoadRun(ctx, "run_invalid_mode"); !errors.Is(loadErr, storecore.ErrRunNotFound) {
				t.Fatalf("LoadRun after invalid mode = %v, want ErrRunNotFound", loadErr)
			}
		})
	}
}

type postStartFailingExecutor struct {
	store   *storesqlite.Store
	release chan struct{}
}

func (e *postStartFailingExecutor) Run(context.Context, string, string, stream.StreamSink) (*runtime.Result, error) {
	return nil, errors.New("unexpected Run call")
}

func (e *postStartFailingExecutor) ExecuteMessages(ctx context.Context, req runtimeapi.ExecuteRequest, sink stream.StreamSink) (*runtime.Result, error) {
	mode := req.OrchestrationMode
	if strings.TrimSpace(string(mode)) == "" {
		mode = events.ModeDirectResponse
	}
	if err := e.store.CreateBoundRunWithParams(ctx, storecore.RunCreateParams{
		RunID:             req.RunID,
		SessionID:         req.SessionID,
		TurnIndex:         req.TurnIndex,
		Input:             req.Input,
		OrchestrationMode: mode,
	}); err != nil {
		return nil, err
	}
	if _, err := e.store.AppendEventContext(ctx, req.RunID, "run.started", map[string]any{"input": req.Input}); err != nil {
		return nil, err
	}
	if err := sink(stream.StreamItem{
		RunID:     req.RunID,
		Kind:      stream.StreamKindRunStarted,
		CreatedAt: time.Now().UTC(),
		Payload:   map[string]any{"input": req.Input},
	}); err != nil {
		return nil, err
	}
	<-e.release
	return nil, errors.New("executor failed after start")
}

func (e *postStartFailingExecutor) ResumeWithTargets(context.Context, string, map[string]any, stream.StreamSink) (*runtime.Result, error) {
	return nil, errors.New("unexpected ResumeWithTargets call")
}

type openaiTestRequest struct {
	Stream bool   `json:"stream"`
	Model  string `json:"model"`
}

type openaiTestResponse struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []openaiTestChoice `json:"choices"`
	Usage   openaiTestUsage    `json:"usage"`
}

type openaiTestChoice struct {
	Index        int               `json:"index"`
	Message      openaiTestMessage `json:"message"`
	FinishReason string            `json:"finish_reason"`
}

type openaiTestMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiTestStreamResponse struct {
	ID      string                   `json:"id"`
	Object  string                   `json:"object"`
	Created int64                    `json:"created"`
	Model   string                   `json:"model"`
	Choices []openaiTestStreamChoice `json:"choices"`
}

type openaiTestStreamChoice struct {
	Index        int                    `json:"index"`
	Delta        openaiTestMessageDelta `json:"delta"`
	FinishReason string                 `json:"finish_reason"`
}

type openaiTestMessageDelta struct {
	Content string `json:"content"`
}

type openaiTestUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func newClientOpenAITestServer(t *testing.T, answer string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		var req openaiTestRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			chunk := openaiTestStreamResponse{
				ID:      "chatcmpl_client_v1",
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   req.Model,
				Choices: []openaiTestStreamChoice{{
					Index: 0,
					Delta: openaiTestMessageDelta{
						Content: answer,
					},
					FinishReason: "stop",
				}},
			}
			body, err := json.Marshal(chunk)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if _, err := fmt.Fprintf(w, "event: message\ndata: %s\n\n", body); err != nil {
				t.Errorf("write stream chunk: %v", err)
				return
			}
			if _, err := fmt.Fprint(w, "event: done\ndata: [DONE]\n\n"); err != nil {
				t.Errorf("write stream done: %v", err)
				return
			}
			return
		}
		response := openaiTestResponse{
			ID:      "chatcmpl_client_v1",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   req.Model,
			Choices: []openaiTestChoice{{
				Index: 0,
				Message: openaiTestMessage{
					Role:    "assistant",
					Content: answer,
				},
				FinishReason: "stop",
			}},
			Usage: openaiTestUsage{
				PromptTokens:     1,
				CompletionTokens: 1,
				TotalTokens:      2,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("encode chat completion response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func clientRuntimeTestConfig(t *testing.T, baseURL string) *config.Config {
	t.Helper()
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Providers[0].Model = "gpt-test"
	cfg.Providers[0].BaseURL = baseURL
	cfg.Providers[0].APIKey = "test-key"
	cfg.Providers[0].MaxCompletionTokens = 32
	cfg.Providers[0].TimeoutSeconds = 5
	cfg.Runtime.StorageDir = filepath.Join(root, "state")
	cfg.Runtime.RunTimeoutSeconds = 5
	cfg.Tools.Workspace.RootDir = root
	cfg.Tools.Mutation.RootDir = root
	cfg.Tools.Mutation.Disabled = true
	cfg.Tools.RunCommand.WorkDir = root
	cfg.Tools.RunCommand.Disabled = true
	cfg.Agent.MaxIterations = 2
	return cfg
}

func newClientRuntimeMemoryModule(t *testing.T, cfg *config.Config) memorymodule.Service {
	t.Helper()
	service, err := memorymodule.NewLocalService(memorymodule.Config{Root: filepath.Join(cfg.Runtime.StorageDir, "memory")})
	if err != nil {
		t.Fatalf("NewLocalService: %v", err)
	}
	if err := service.EnsureLayout(t.Context()); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}
	if err := service.BuildIndex(t.Context()); err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	if err := service.SetSemanticRuntime(memorymodule.SemanticRuntimeOptions{
		Index:      &clientRuntimeSemanticIndex{},
		Embedder:   clientRuntimeEmbedder{dimensions: cfg.Memory.Semantic.Embedding.Dimensions, model: cfg.Memory.Semantic.Embedding.Model},
		Model:      cfg.Memory.Semantic.Embedding.Model,
		Dimensions: cfg.Memory.Semantic.Embedding.Dimensions,
		BatchSize:  cfg.Memory.Semantic.Embedding.BatchSize,
		Schema:     memorymodule.SemanticSchemaMemoryRecordsV1,
		IndexName:  cfg.Memory.Semantic.Bleve.IndexName,
		Mode:       "hybrid",
	}); err != nil {
		t.Fatalf("SetSemanticRuntime: %v", err)
	}
	return service
}

type clientRuntimeSemanticIndex struct{}

func (i *clientRuntimeSemanticIndex) Rebuild(context.Context, memorymodule.SemanticRebuildRequest) (*memorymodule.SemanticRebuildResult, error) {
	return nil, errors.New("client runtime semantic rebuild is not implemented")
}

func (i *clientRuntimeSemanticIndex) Search(context.Context, memorymodule.SemanticSearchRequest) (*memorymodule.SemanticSearchResult, error) {
	return &memorymodule.SemanticSearchResult{}, nil
}

func (i *clientRuntimeSemanticIndex) Close() error { return nil }

type clientRuntimeEmbedder struct {
	dimensions int
	model      string
}

func (e clientRuntimeEmbedder) Embed(_ context.Context, req memorymodule.EmbedRequest) (*memorymodule.EmbedResult, error) {
	vectors := make([]memorymodule.EmbeddingVector, 0, len(req.Inputs))
	for _, input := range req.Inputs {
		vectors = append(vectors, memorymodule.EmbeddingVector{
			Ref:    input.Ref,
			Values: make([]float32, e.dimensions),
		})
	}
	return &memorymodule.EmbedResult{Model: e.model, Dimensions: e.dimensions, Vectors: vectors}, nil
}

func waitForRunStatus(t *testing.T, store *storesqlite.Store, runID string, want events.RunStatus) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := store.LoadRun(context.Background(), runID)
		if err == nil && run.Status == want {
			return
		}
		if err != nil && !errors.Is(err, storecore.ErrRunNotFound) {
			t.Fatalf("LoadRun: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	run, err := store.LoadRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("LoadRun after wait: %v", err)
	}
	t.Fatalf("run status = %q, want %q, error=%q, output=%q", run.Status, want, run.Error, run.Output)
}

func toClientTestJSON(value any) string {
	body, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(body)
}

func mustParseMessageID(t *testing.T, value string) int64 {
	t.Helper()
	var id int64
	if _, err := fmt.Sscanf(value, "%d", &id); err != nil {
		t.Fatalf("parse message id %q: %v", value, err)
	}
	return id
}
