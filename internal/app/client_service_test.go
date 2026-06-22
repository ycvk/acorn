package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	storecore "github.com/ycvk/acorn/internal/store"

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
	if _, err := store.AppendSessionMessage(ctx, session.SessionID, 1, "user", "How do I configure pairing on the VPS?", ""); err != nil {
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
		RunID:     "run_1",
		SessionID: "session_1",
		Status:    events.RunStatusSucceeded,
		CreatedAt: now,
		UpdatedAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("projectRun: %v", err)
	}
	if run.ID != "run_1" || run.ThreadID != "session_1" || run.Status != "completed" || run.Mode != "direct" || run.CompletedAt.IsZero() {
		t.Fatalf("run = %#v", run)
	}
}

func TestProjectRunRejectsUnknownStatus(t *testing.T) {
	_, err := projectRun(events.RunRecord{
		RunID:  "run_bad_status",
		Status: events.RunStatus(""),
	})
	if !errors.Is(err, ErrClientProjectionFailed) {
		t.Fatalf("status error = %v, want ErrClientProjectionFailed", err)
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

func TestProjectRunEventAcceptsDecisionBlocked(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	event, err := clientevents.ProjectRunEvent(events.EventRecord{
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
			_, err := clientevents.ProjectRunEvent(events.EventRecord{
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

func TestLoadRunEventsAfterFiltersDiagnosticsAndAdvancesCursor(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateRun(ctx, "run_live", "input"); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if _, err := store.AppendEventContext(ctx, "run_live", "run.started", map[string]any{"input": "hello"}); err != nil {
		t.Fatalf("append run.started: %v", err)
	}
	if _, err := store.AppendEventContext(ctx, "run_live", "tool.call.progress", map[string]any{"delta": "hidden"}); err != nil {
		t.Fatalf("append tool.call.progress: %v", err)
	}
	if _, err := store.AppendEventContext(ctx, "run_live", "memory.prepared", map[string]any{"memory_prepared": map[string]any{"entry_count": float64(2)}}); err != nil {
		t.Fatalf("append memory.prepared: %v", err)
	}
	if _, err := store.AppendEventContext(ctx, "run_live", "assistant.delta", map[string]any{"assistant_delta": map[string]any{"delta": "hi"}}); err != nil {
		t.Fatalf("append assistant.delta: %v", err)
	}
	if _, err := store.AppendEventContext(ctx, "run_live", "skill.selected", map[string]any{"skill": map[string]any{"selected_id": "skill.hidden"}}); err != nil {
		t.Fatalf("append skill.selected: %v", err)
	}

	service := BuildClientService(store, nil, nil, "/repo")
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
	store, err := storesqlite.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateRun(context.Background(), "run_detail", "input"); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if _, err := store.AppendEventContext(context.Background(), "run_detail", "run.started", map[string]any{"input": "hello"}); err != nil {
		t.Fatalf("append run.started: %v", err)
	}
	if _, err := store.AppendEventContext(context.Background(), "run_detail", "future.kind", map[string]any{"value": "debug"}); err != nil {
		t.Fatalf("append future.kind: %v", err)
	}
	if _, err := store.AppendEventContext(context.Background(), "run_detail", "skill.selected", map[string]any{"skill": map[string]any{"selected_id": "skill.ship.patch"}}); err != nil {
		t.Fatalf("append skill.selected: %v", err)
	}
	service := BuildClientService(store, nil, nil, "/repo")
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
	store, err := storesqlite.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateRun(ctx, "run_artifacts", "input"); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	_, err = store.SaveArtifact(ctx, storecore.ArtifactRecord{
		ArtifactID:          "artifact_report",
		RunID:               "run_artifacts",
		SessionID:           "thread_artifacts",
		SourceToolResultRef: "tool_result:run_artifacts:call_1",
		Kind:                storecore.ArtifactKindMarkdown,
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

	service := BuildClientService(store, nil, nil, "/repo")
	artifacts, err := service.ListRunArtifacts(ctx, "run_artifacts")
	if err != nil {
		t.Fatalf("ListRunArtifacts: %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].ArtifactID != "artifact_report" || artifacts[0].SourceToolResultRef != "tool_result:run_artifacts:call_1" {
		t.Fatalf("artifacts = %#v", artifacts)
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
		MemoryModule: newClientRuntimeMemoryModule(t, cfg),
	})
	if err != nil {
		t.Fatalf("NewRunnerFactory: %v", err)
	}
	executor, err := runtime.NewExecutorWithRunRuntimeAndController(cfg, store, runnerFactory, nil)
	if err != nil {
		t.Fatalf("NewExecutorWithRunRuntimeAndController: %v", err)
	}

	service := BuildClientService(store, func(context.Context) (executorHandle, error) {
		return runtimeExecutorHandle{exec: executor}, nil
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

type postStartFailingExecutor struct {
	store   *storesqlite.Store
	release chan struct{}
}

func (e *postStartFailingExecutor) ExecuteMessages(ctx context.Context, req runtimeapi.ExecuteRequest, observer runStartObserver) error {
	if err := e.store.CreateBoundRunWithParams(ctx, storecore.RunCreateParams{
		RunID:     req.RunID,
		SessionID: req.SessionID,
		TurnIndex: req.TurnIndex,
		Input:     req.Input,
	}); err != nil {
		return err
	}
	if _, err := e.store.AppendEventContext(ctx, req.RunID, "run.started", map[string]any{"input": req.Input}); err != nil {
		return err
	}
	if observer != nil {
		observer.RunStarted()
	}
	<-e.release
	return errors.New("executor failed after start")
}

func (e *postStartFailingExecutor) ResumeWithTargets(context.Context, string, map[string]any) (*executorRunResult, error) {
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
	// Semantic is OFF by default now (no embedding model/base_url defaults); this
	// test wires a semantic runtime, so set them explicitly.
	cfg.Memory.Semantic.Embedding.Model = "text-embedding-test"
	cfg.Memory.Semantic.Embedding.BaseURL = baseURL
	cfg.Memory.Semantic.Embedding.APIKey = "test-key"
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
		VectorStore: &clientRuntimeSemanticIndex{},
		Embedder:    clientRuntimeEmbedder{dimensions: cfg.Memory.Semantic.Embedding.Dimensions, model: cfg.Memory.Semantic.Embedding.Model},
		Model:       cfg.Memory.Semantic.Embedding.Model,
		Dimensions:  cfg.Memory.Semantic.Embedding.Dimensions,
		BatchSize:   cfg.Memory.Semantic.Embedding.BatchSize,
	}); err != nil {
		t.Fatalf("SetSemanticRuntime: %v", err)
	}
	return service
}

type clientRuntimeSemanticIndex struct{}

func (i *clientRuntimeSemanticIndex) Store(_ context.Context, _ string, _ memorymodule.Kind, _ string, _ []float32, _ string, _ int) error {
	return nil
}

func (i *clientRuntimeSemanticIndex) Search(_ context.Context, _ []float32, limit int) ([]memorymodule.VectorSearchResult, error) {
	return make([]memorymodule.VectorSearchResult, 0, limit), nil
}

func (i *clientRuntimeSemanticIndex) Delete(_ context.Context, _ string) error {
	return nil
}

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
