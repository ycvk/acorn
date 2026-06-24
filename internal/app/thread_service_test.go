package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/store"
)

func TestProjectThread(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	service := NewThreadService(nil, "/repo")

	thread, err := service.projectThread(domain.SessionRecord{
		SessionID: "session_1",
		Title:     "Inspect repo",
		CreatedAt: now,
		UpdatedAt: now,
	}, &domain.RunRecord{
		RunID:      "run_1",
		Status:     domain.RunStatusSucceeded,
		CreatedAt:  now,
		FinishedAt: now,
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
	store, err := store.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	service := NewThreadService(store, "/repo")
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
	store, err := store.Open(filepath.Join(t.TempDir(), "state"))
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
	service := NewThreadService(store, "/repo")

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
	message, err := projectMessage(domain.SessionMessageRecord{
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
	message, err := projectMessage(domain.SessionMessageRecord{
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
	message, err := projectMessage(domain.SessionMessageRecord{
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
	_, err := projectMessage(domain.SessionMessageRecord{
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
	message, err := projectMessage(domain.SessionMessageRecord{
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
	_, err := projectMessage(domain.SessionMessageRecord{
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
