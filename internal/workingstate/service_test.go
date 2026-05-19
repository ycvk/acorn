package workingstate

import (
	"context"
	"strings"
	"testing"
)

type mockStore struct {
	checkpoints map[string]Checkpoint
}

func (m *mockStore) GetWorkingCheckpoint(_ context.Context, threadID string) (*Checkpoint, error) {
	if cp, ok := m.checkpoints[threadID]; ok {
		return &cp, nil
	}
	return nil, nil
}

func (m *mockStore) UpsertWorkingCheckpoint(_ context.Context, checkpoint Checkpoint) error {
	if m.checkpoints == nil {
		m.checkpoints = make(map[string]Checkpoint)
	}
	m.checkpoints[checkpoint.ThreadID] = checkpoint
	return nil
}

func (m *mockStore) DeleteWorkingCheckpoint(_ context.Context, threadID string) error {
	delete(m.checkpoints, threadID)
	return nil
}

func TestNewService(t *testing.T) {
	s := NewService(nil, 0)
	if s.maxChars != 4000 {
		t.Fatalf("expected default maxChars 4000, got %d", s.maxChars)
	}
}

func TestServiceGet(t *testing.T) {
	store := &mockStore{checkpoints: map[string]Checkpoint{
		"thread-1": {ThreadID: "thread-1", Content: "content-1"},
	}}
	s := NewService(store, 100)
	ctx := context.Background()

	cp, err := s.Get(ctx, "thread-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if cp == nil || cp.Content != "content-1" {
		t.Fatalf("unexpected checkpoint: %+v", cp)
	}
}

func TestServiceGetNilStore(t *testing.T) {
	s := NewService(nil, 100)
	ctx := context.Background()

	_, err := s.Get(ctx, "thread-1")
	if err == nil {
		t.Fatalf("expected error for nil store")
	}
}

func TestServiceGetEmptyThreadID(t *testing.T) {
	store := &mockStore{}
	s := NewService(store, 100)
	ctx := context.Background()

	_, err := s.Get(ctx, "")
	if err == nil {
		t.Fatalf("expected error for empty thread id")
	}
}

func TestServiceUpdate(t *testing.T) {
	store := &mockStore{}
	s := NewService(store, 100)
	ctx := context.Background()

	cp, err := s.Update(ctx, "thread-1", "content-1", "skill-1")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if cp.ThreadID != "thread-1" || cp.Content != "content-1" || cp.RelatedSkillID != "skill-1" {
		t.Fatalf("unexpected checkpoint: %+v", cp)
	}
}

func TestServiceUpdateTrimsSpace(t *testing.T) {
	store := &mockStore{}
	s := NewService(store, 100)
	ctx := context.Background()

	cp, err := s.Update(ctx, " thread-1 ", " content-1 ", " skill-1 ")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if cp.ThreadID != "thread-1" || cp.Content != "content-1" || cp.RelatedSkillID != "skill-1" {
		t.Fatalf("unexpected checkpoint after trim: %+v", cp)
	}
}

func TestServiceUpdateMaxChars(t *testing.T) {
	store := &mockStore{}
	s := NewService(store, 5)
	ctx := context.Background()

	cp, err := s.Update(ctx, "thread-1", "hello world", "")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if cp.Content != "hello" {
		t.Fatalf("expected content truncated to 5 chars, got %q", cp.Content)
	}
}

func TestServiceUpdateEmptyContent(t *testing.T) {
	store := &mockStore{}
	s := NewService(store, 100)
	ctx := context.Background()

	_, err := s.Update(ctx, "thread-1", "", "")
	if err == nil {
		t.Fatalf("expected error for empty content")
	}
}

func TestServiceClear(t *testing.T) {
	store := &mockStore{checkpoints: map[string]Checkpoint{
		"thread-1": {ThreadID: "thread-1", Content: "content-1"},
	}}
	s := NewService(store, 100)
	ctx := context.Background()

	err := s.Clear(ctx, "thread-1")
	if err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if len(store.checkpoints) != 0 {
		t.Fatalf("expected checkpoint deleted")
	}
}

func TestFormatForPrompt(t *testing.T) {
	cp := &Checkpoint{ThreadID: "t1", Content: "focus", RelatedSkillID: "skill-1"}
	got := FormatForPrompt(cp)
	if got == "" {
		t.Fatalf("expected non-empty formatted prompt")
	}
	if !contains(got, "focus") || !contains(got, "skill-1") {
		t.Fatalf("formatted prompt missing content: %q", got)
	}
}

func TestFormatForPromptNil(t *testing.T) {
	got := FormatForPrompt(nil)
	if got != "" {
		t.Fatalf("expected empty string for nil checkpoint, got %q", got)
	}
}

func TestFormatForPromptEmpty(t *testing.T) {
	got := FormatForPrompt(&Checkpoint{Content: "  "})
	if got != "" {
		t.Fatalf("expected empty string for empty content, got %q", got)
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
