package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/runtimehistory"
)

func TestSaveAndLoadContextBoundary(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Date(2026, 5, 8, 12, 30, 0, 0, time.UTC)
	boundary := testContextBoundary("ctxb_1", "session_1", "run_1", 1, now)
	if err := store.SaveContextBoundary(context.Background(), boundary); err != nil {
		t.Fatalf("save context boundary: %v", err)
	}

	got, err := store.LoadContextBoundary(context.Background(), "ctxb_1")
	if err != nil {
		t.Fatalf("load context boundary: %v", err)
	}
	if got == nil {
		t.Fatal("expected boundary")
	}
	if got.BoundaryID != boundary.BoundaryID || got.SessionID != boundary.SessionID || got.RunID != boundary.RunID {
		t.Fatalf("unexpected identity: %+v", got)
	}
	if got.Sequence != boundary.Sequence || got.FirstIndex != boundary.FirstIndex || got.LastIndex != boundary.LastIndex {
		t.Fatalf("unexpected range: %+v", got)
	}
	if got.TurnIndex != boundary.TurnIndex || got.Mode != boundary.Mode || got.Trigger != boundary.Trigger {
		t.Fatalf("unexpected protocol metadata: %+v", got)
	}
	if got.CoveredFirstMessageID != boundary.CoveredFirstMessageID || got.CoveredLastMessageID != boundary.CoveredLastMessageID {
		t.Fatalf("unexpected covered message ids: %+v", got)
	}
	if got.SummaryMessageID != boundary.SummaryMessageID || got.TranscriptRef != boundary.TranscriptRef {
		t.Fatalf("unexpected summary/transcript refs: %+v", got)
	}
	if got.PreservedFromIndex != boundary.PreservedFromIndex || got.PreservedToIndex != boundary.PreservedToIndex {
		t.Fatalf("unexpected preserved range: %+v", got)
	}
	if got.PreservedHeadMessageID != boundary.PreservedHeadMessageID || got.PreservedAnchorMessageID != boundary.PreservedAnchorMessageID || got.PreservedTailMessageID != boundary.PreservedTailMessageID {
		t.Fatalf("unexpected preserved message ids: %+v", got)
	}
	if got.TokensBefore != boundary.TokensBefore || got.TokensAfter != boundary.TokensAfter {
		t.Fatalf("unexpected token metrics: %+v", got)
	}
	if got.EffectiveWindowTokens != boundary.EffectiveWindowTokens {
		t.Fatalf("effective window tokens = %d, want %d", got.EffectiveWindowTokens, boundary.EffectiveWindowTokens)
	}
	if got.Summary != boundary.Summary || got.SummarySnippet != boundary.SummarySnippet {
		t.Fatalf("unexpected summary fields: %+v", got)
	}
	if !got.CreatedAt.Equal(now) {
		t.Fatalf("created_at = %s, want %s", got.CreatedAt, now)
	}
}

func TestLoadLatestAndListContextBoundaries(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	base := time.Date(2026, 5, 8, 12, 30, 0, 0, time.UTC)
	for _, boundary := range []runtimehistory.ContextBoundary{
		testContextBoundary("ctxb_1", "session_1", "run_1", 1, base),
		testContextBoundary("ctxb_2", "session_1", "run_2", 2, base.Add(time.Minute)),
		testContextBoundary("ctxb_other", "session_2", "run_3", 3, base.Add(2*time.Minute)),
	} {
		if err := store.SaveContextBoundary(ctx, boundary); err != nil {
			t.Fatalf("save context boundary %s: %v", boundary.BoundaryID, err)
		}
	}

	latest, err := store.LoadLatestContextBoundary(ctx, "session_1")
	if err != nil {
		t.Fatalf("load latest context boundary: %v", err)
	}
	if latest == nil || latest.BoundaryID != "ctxb_2" {
		t.Fatalf("latest boundary = %+v, want ctxb_2", latest)
	}

	items, err := store.ListContextBoundaries(ctx, "session_1")
	if err != nil {
		t.Fatalf("list context boundaries: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("boundary count = %d, want 2", len(items))
	}
	if items[0].BoundaryID != "ctxb_1" || items[1].BoundaryID != "ctxb_2" {
		t.Fatalf("boundaries out of order: %+v", items)
	}
}

func TestSaveContextBoundaryRejectsInvalidInput(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	valid := testContextBoundary("ctxb_1", "session_1", "run_1", 1, time.Date(2026, 5, 8, 12, 30, 0, 0, time.UTC))
	cases := []struct {
		name    string
		mutate  func(*runtimehistory.ContextBoundary)
		wantErr string
	}{
		{name: "missing boundary id", mutate: func(b *runtimehistory.ContextBoundary) { b.BoundaryID = "" }, wantErr: "id is required"},
		{name: "missing session id", mutate: func(b *runtimehistory.ContextBoundary) { b.SessionID = "" }, wantErr: "session id is required"},
		{name: "missing run id", mutate: func(b *runtimehistory.ContextBoundary) { b.RunID = "" }, wantErr: "run id is required"},
		{name: "invalid sequence", mutate: func(b *runtimehistory.ContextBoundary) { b.Sequence = 0 }, wantErr: "sequence must be positive"},
		{name: "invalid turn index", mutate: func(b *runtimehistory.ContextBoundary) { b.TurnIndex = -1 }, wantErr: "turn index"},
		{name: "missing mode", mutate: func(b *runtimehistory.ContextBoundary) { b.Mode = "" }, wantErr: "mode is required"},
		{name: "missing trigger", mutate: func(b *runtimehistory.ContextBoundary) { b.Trigger = "" }, wantErr: "trigger is required"},
		{name: "invalid covered range", mutate: func(b *runtimehistory.ContextBoundary) { b.LastIndex = b.FirstIndex - 1 }, wantErr: "last index"},
		{name: "missing covered first message", mutate: func(b *runtimehistory.ContextBoundary) { b.CoveredFirstMessageID = "" }, wantErr: "covered first message id"},
		{name: "missing covered last message", mutate: func(b *runtimehistory.ContextBoundary) { b.CoveredLastMessageID = "" }, wantErr: "covered last message id"},
		{name: "missing summary message", mutate: func(b *runtimehistory.ContextBoundary) { b.SummaryMessageID = "" }, wantErr: "summary message id"},
		{name: "missing transcript ref", mutate: func(b *runtimehistory.ContextBoundary) { b.TranscriptRef = "" }, wantErr: "transcript ref"},
		{name: "invalid preserved range", mutate: func(b *runtimehistory.ContextBoundary) { b.PreservedToIndex = b.PreservedFromIndex - 1 }, wantErr: "preserved to index"},
		{name: "missing preserved head", mutate: func(b *runtimehistory.ContextBoundary) { b.PreservedHeadMessageID = "" }, wantErr: "preserved head message id"},
		{name: "missing preserved anchor", mutate: func(b *runtimehistory.ContextBoundary) { b.PreservedAnchorMessageID = "" }, wantErr: "preserved anchor message id"},
		{name: "missing preserved tail", mutate: func(b *runtimehistory.ContextBoundary) { b.PreservedTailMessageID = "" }, wantErr: "preserved tail message id"},
		{name: "invalid tokens before", mutate: func(b *runtimehistory.ContextBoundary) { b.TokensBefore = 0 }, wantErr: "tokens before"},
		{name: "invalid tokens after", mutate: func(b *runtimehistory.ContextBoundary) { b.TokensAfter = 0 }, wantErr: "tokens after"},
		{name: "token growth", mutate: func(b *runtimehistory.ContextBoundary) { b.TokensAfter = b.TokensBefore }, wantErr: "less than tokens before"},
		{name: "invalid effective window", mutate: func(b *runtimehistory.ContextBoundary) { b.EffectiveWindowTokens = 0 }, wantErr: "effective window tokens"},
		{name: "tokens exceed effective window", mutate: func(b *runtimehistory.ContextBoundary) { b.EffectiveWindowTokens = b.TokensAfter - 1 }, wantErr: "must not exceed effective window"},
		{name: "missing summary", mutate: func(b *runtimehistory.ContextBoundary) { b.Summary = "  " }, wantErr: "summary is required"},
		{name: "missing created_at", mutate: func(b *runtimehistory.ContextBoundary) { b.CreatedAt = time.Time{} }, wantErr: "created_at is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			boundary := valid
			tc.mutate(&boundary)
			err := store.SaveContextBoundary(context.Background(), boundary)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadContextBoundaryRejectsCorruptTimestamp(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	boundary := testContextBoundary("ctxb_1", "session_1", "run_1", 1, time.Date(2026, 5, 8, 12, 30, 0, 0, time.UTC))
	if err := store.SaveContextBoundary(ctx, boundary); err != nil {
		t.Fatalf("save context boundary: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE context_boundaries SET created_at = 'not-a-timestamp' WHERE boundary_id = ?", boundary.BoundaryID); err != nil {
		t.Fatalf("corrupt timestamp: %v", err)
	}

	_, err = store.LoadContextBoundary(ctx, boundary.BoundaryID)
	if err == nil || !strings.Contains(err.Error(), "parse context_boundary.created_at timestamp") {
		t.Fatalf("error = %v, want parse timestamp error", err)
	}
}

func testContextBoundary(boundaryID, sessionID, runID string, sequence int, createdAt time.Time) runtimehistory.ContextBoundary {
	return runtimehistory.ContextBoundary{
		BoundaryID:               boundaryID,
		SessionID:                sessionID,
		RunID:                    runID,
		Sequence:                 sequence,
		TurnIndex:                4,
		Mode:                     "direct_response",
		Trigger:                  "auto",
		FirstIndex:               2,
		LastIndex:                12,
		CoveredFirstMessageID:    "msg_2",
		CoveredLastMessageID:     "msg_12",
		PreviousBoundaryID:       "",
		SummaryMessageID:         "msg_summary_1",
		TranscriptRef:            "run_1:events:10-20",
		PreservedFromIndex:       13,
		PreservedToIndex:         16,
		PreservedHeadMessageID:   "msg_13",
		PreservedAnchorMessageID: "msg_14",
		PreservedTailMessageID:   "msg_16",
		TokensBefore:             12000,
		TokensAfter:              4200,
		EffectiveWindowTokens:    16000,
		Summary:                  "Continuation summary for the compacted conversation.",
		SummarySnippet:           "Continuation summary",
		CreatedAt:                createdAt,
	}
}
