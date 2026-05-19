package runtimehistory

import (
	"context"
	"testing"
)

type mockSummaryStore struct {
	summaries map[string]SessionSummary
}

func (m *mockSummaryStore) GetSessionSummary(_ context.Context, sessionID string) (*SessionSummary, error) {
	if s, ok := m.summaries[sessionID]; ok {
		return &s, nil
	}
	return nil, nil
}

func (m *mockSummaryStore) UpsertSessionSummary(_ context.Context, summary SessionSummary) error {
	if m.summaries == nil {
		m.summaries = make(map[string]SessionSummary)
	}
	m.summaries[summary.SessionID] = summary
	return nil
}

func TestNewSessionSummaryService(t *testing.T) {
	s := NewSessionSummaryService(nil, 0)
	if s.maxChars != 2000 {
		t.Fatalf("expected default maxChars 2000, got %d", s.maxChars)
	}
}

func TestSessionSummaryServiceGet(t *testing.T) {
	store := &mockSummaryStore{summaries: map[string]SessionSummary{
		"session-1": {SessionID: "session-1", Summary: "summary-1"},
	}}
	s := NewSessionSummaryService(store, 100)
	ctx := context.Background()

	sum, err := s.Get(ctx, "session-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if sum == nil || sum.Summary != "summary-1" {
		t.Fatalf("unexpected summary: %+v", sum)
	}
}

func TestSessionSummaryServiceGetNilStore(t *testing.T) {
	s := NewSessionSummaryService(nil, 100)
	ctx := context.Background()

	_, err := s.Get(ctx, "session-1")
	if err == nil {
		t.Fatalf("expected error for nil store")
	}
}

func TestSessionSummaryServiceGetEmptyID(t *testing.T) {
	store := &mockSummaryStore{}
	s := NewSessionSummaryService(store, 100)
	ctx := context.Background()

	_, err := s.Get(ctx, "")
	if err == nil {
		t.Fatalf("expected error for empty session id")
	}
}

func TestSessionSummaryServiceUpdate(t *testing.T) {
	store := &mockSummaryStore{}
	s := NewSessionSummaryService(store, 100)
	ctx := context.Background()

	sum, err := s.Update(ctx, "session-1", "run-1", "completed", "summary-1")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if sum.SessionID != "session-1" || sum.Summary != "summary-1" || sum.SourceRunID != "run-1" || sum.RunStatus != "completed" {
		t.Fatalf("unexpected summary: %+v", sum)
	}
}

func TestSessionSummaryServiceUpdateMaxChars(t *testing.T) {
	store := &mockSummaryStore{}
	s := NewSessionSummaryService(store, 5)
	ctx := context.Background()

	sum, err := s.Update(ctx, "session-1", "", "", "hello world")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if sum.Summary != "hello" {
		t.Fatalf("expected summary truncated to 5 chars, got %q", sum.Summary)
	}
}

func TestSessionSummaryServiceUpdateEmptySummary(t *testing.T) {
	store := &mockSummaryStore{}
	s := NewSessionSummaryService(store, 100)
	ctx := context.Background()

	_, err := s.Update(ctx, "session-1", "", "", "")
	if err == nil {
		t.Fatalf("expected error for empty summary")
	}
}

func TestFormatSessionSummaryForPrompt(t *testing.T) {
	sum := &SessionSummary{SessionID: "s1", Summary: "focus", RunStatus: "done", SourceRunID: "run-1"}
	got := FormatSessionSummaryForPrompt(sum)
	if got == "" {
		t.Fatalf("expected non-empty formatted prompt")
	}
	if !contains(got, "focus") || !contains(got, "done") || !contains(got, "run-1") {
		t.Fatalf("formatted prompt missing content: %q", got)
	}
}

func TestFormatSessionSummaryForPromptNil(t *testing.T) {
	got := FormatSessionSummaryForPrompt(nil)
	if got != "" {
		t.Fatalf("expected empty string for nil summary, got %q", got)
	}
}

func TestFormatSessionSummaryForPromptEmpty(t *testing.T) {
	got := FormatSessionSummaryForPrompt(&SessionSummary{Summary: "  "})
	if got != "" {
		t.Fatalf("expected empty string for empty summary, got %q", got)
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
