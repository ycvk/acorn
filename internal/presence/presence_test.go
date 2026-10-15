package presence

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ycvk/acorn/internal/core"
)

var t0 = time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC) // 09:00 Monday in Shanghai

func shanghai(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	return loc
}

func runeCount(s string) (int, error) { return utf8.RuneCountInString(s), nil }

func mustRender(t *testing.T, in RenderInput) string {
	t.Helper()
	out, err := Render(in)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return out
}

func TestRenderOrdersSectionsInOwnerTime(t *testing.T) {
	in := RenderInput{Now: t0, Location: shanghai(t), Wake: "commitment #3", Count: runeCount, MaxTokens: 10000,
		Commitments: []core.Commitment{{ID: 2, State: "scheduled", Content: "later", WakeAt: t0.Add(48 * time.Hour), Recurrence: "0 9 * * *"}, {ID: 3, State: "due", Content: "提醒 owner 看 X", WakeAt: t0}, {ID: 5, State: "scheduled", Content: "sooner", WakeAt: t0.Add(time.Hour)}},
		Occurrences: []core.CommitmentOccurrence{{ID: 30, CommitmentID: 3, State: "due", DueAt: t0}},
		Thoughts:    []core.MemoryRecord{{ID: "thought", Kind: "thought", State: "open", Content: "owner 在读论文", UpdatedAt: t0}},
		Concerns:    []core.MemoryConcern{{ID: "concern", State: "active", Title: "准备面试", Revision: 2, UpdatedAt: t0}}}
	out := mustRender(t, in)
	for _, want := range []string{"Now: 2026-10-05 Mon 09:00 (Asia/Shanghai)", "#3 occurrence #30", `repeats "0 9 * * *"`, "owner 在读论文", "concern rev 2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
	last := -1
	for _, marker := range []string{"Due commitments", "#5 [", "#2 [", "Concerns", "Open thoughts"} {
		i := strings.Index(out, marker)
		if i <= last {
			t.Fatalf("section order: %s", out)
		}
		last = i
	}
}

func TestRenderPreservesDueOccurrenceAndEnforcesBudget(t *testing.T) {
	in := RenderInput{Now: t0, Location: time.UTC, Wake: "owner message", Count: runeCount,
		Commitments: []core.Commitment{{ID: 1, State: "due", Content: "must stay", WakeAt: t0}}, Occurrences: []core.CommitmentOccurrence{{ID: 10, CommitmentID: 1, State: "due", DueAt: t0}},
		Thoughts: []core.MemoryRecord{{ID: "old", Kind: "thought", State: "open", Content: strings.Repeat("x", 200), UpdatedAt: t0.Add(-time.Hour)}, {ID: "new", Kind: "thought", State: "open", Content: "recent thought", UpdatedAt: t0}}}
	full := mustRender(t, in)
	in.MaxTokens = utf8.RuneCountInString(full) - 120
	out := mustRender(t, in)
	if len([]rune(out)) > in.MaxTokens || strings.Contains(out, "old rev") || !strings.Contains(out, "new rev") || !strings.Contains(out, "must stay") {
		t.Fatalf("budgeted=%s", out)
	}
	in.MaxTokens = 1
	if _, err := Render(in); err == nil {
		t.Fatal("mandatory context overflow accepted")
	}
}

type memoryPersonaStore struct{ revisions []core.PersonaRevision }

func (m *memoryPersonaStore) WritePersona(_ context.Context, w core.PersonaWrite) (core.PersonaRevision, error) {
	if int64(len(m.revisions)) != w.BaseRevision {
		return core.PersonaRevision{}, core.ErrPersonaConflict
	}
	r := core.PersonaRevision{Revision: w.BaseRevision + 1, Content: w.Content, Author: w.Author, DeviceID: w.DeviceID, At: w.At}
	m.revisions = append(m.revisions, r)
	return r, nil
}

func (m *memoryPersonaStore) Persona(context.Context) (core.PersonaRevision, error) {
	if len(m.revisions) == 0 {
		return core.PersonaRevision{}, core.ErrPersonaNotFound
	}
	return m.revisions[len(m.revisions)-1], nil
}

func (m *memoryPersonaStore) PersonaRevisions(context.Context) ([]core.PersonaRevision, error) {
	return m.revisions, nil
}

func (m *memoryPersonaStore) PersonaRevision(_ context.Context, revision int64) (core.PersonaRevision, error) {
	if revision < 1 || revision > int64(len(m.revisions)) {
		return core.PersonaRevision{}, core.ErrPersonaNotFound
	}
	return m.revisions[revision-1], nil
}

func TestSeedPersonaWritesTheDefaultOnceAndKeepsTheOwners(t *testing.T) {
	store := &memoryPersonaStore{}
	if err := SeedPersona(t.Context(), store, t0); err != nil {
		t.Fatal(err)
	}
	if len(store.revisions) != 1 || store.revisions[0].Author != core.PersonaAuthorRelease || store.revisions[0].Content != strings.TrimSpace(DefaultPersona) {
		t.Fatalf("seeded = %+v", store.revisions)
	}
	if _, err := SavePersona(t.Context(), store, core.PersonaWrite{Content: "你是 Acorn。", Author: core.PersonaAuthorOwner, At: t0, BaseRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if err := SeedPersona(t.Context(), store, t0); err != nil {
		t.Fatal(err)
	}
	if len(store.revisions) != 2 || store.revisions[1].Content != "你是 Acorn。" {
		t.Fatalf("seed over the owner's persona: %+v", store.revisions)
	}
}

func TestSavePersonaTrimsAndBoundsTheContent(t *testing.T) {
	store := &memoryPersonaStore{}
	saved, err := SavePersona(t.Context(), store, core.PersonaWrite{Content: "\n  Be brief.  \n", Author: core.PersonaAuthorOwner, At: t0})
	if err != nil || saved.Content != "Be brief." {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
	for _, content := range []string{" \n\t", strings.Repeat("x", MaxPersonaBytes+1)} {
		if _, err := SavePersona(t.Context(), store, core.PersonaWrite{Content: content, Author: core.PersonaAuthorOwner, At: t0, BaseRevision: 1}); !errors.Is(err, core.ErrPersonaInvalid) {
			t.Fatalf("persona of %d bytes: err = %v", len(content), err)
		}
	}
	if len(store.revisions) != 1 {
		t.Fatalf("rejected personas were written: %+v", store.revisions)
	}
}
