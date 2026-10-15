package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestPersonaWritesAreRevisionsBasedOnTheCurrentOne(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	day := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	if _, err := s.Persona(ctx); !errors.Is(err, core.ErrPersonaNotFound) {
		t.Fatalf("persona before any write: err = %v", err)
	}
	first, err := s.WritePersona(ctx, core.PersonaWrite{Content: "You are Acorn.", Author: core.PersonaAuthorRelease, At: day})
	if err != nil || first.Revision != 1 || first.SHA256 == "" {
		t.Fatalf("first = %+v, %v", first, err)
	}
	second, err := s.WritePersona(ctx, core.PersonaWrite{Content: "你是 Acorn。", Author: core.PersonaAuthorOwner, DeviceID: "device_1", At: day.Add(time.Hour), BaseRevision: 1})
	if err != nil || second.Revision != 2 {
		t.Fatalf("second = %+v, %v", second, err)
	}
	if _, err := s.WritePersona(ctx, core.PersonaWrite{Content: "stale", Author: core.PersonaAuthorOwner, At: day, BaseRevision: 1}); !errors.Is(err, core.ErrPersonaConflict) {
		t.Fatalf("stale write err = %v", err)
	}
	if _, err := s.WritePersona(ctx, core.PersonaWrite{Content: "x", Author: "agent", At: day, BaseRevision: 2}); err == nil {
		t.Fatal("an unknown author must be rejected")
	}
	if _, err := s.WritePersona(ctx, core.PersonaWrite{Content: " \n", Author: core.PersonaAuthorOwner, At: day, BaseRevision: 2}); err == nil {
		t.Fatal("an empty persona must be rejected")
	}

	current, err := s.Persona(ctx)
	if err != nil || current.Revision != 2 || current.Content != "你是 Acorn。" || current.Author != core.PersonaAuthorOwner || current.DeviceID != "device_1" || !current.At.Equal(day.Add(time.Hour)) {
		t.Fatalf("current = %+v, %v", current, err)
	}
	old, err := s.PersonaRevision(ctx, 1)
	if err != nil || old.Content != "You are Acorn." || old.Author != core.PersonaAuthorRelease || old.SHA256 != first.SHA256 {
		t.Fatalf("revision 1 = %+v, %v", old, err)
	}
	if _, err := s.PersonaRevision(ctx, 3); !errors.Is(err, core.ErrPersonaNotFound) {
		t.Fatalf("missing revision err = %v", err)
	}
	list, err := s.PersonaRevisions(ctx)
	if err != nil || len(list) != 2 || list[0].Revision != 2 || list[1].Revision != 1 || list[0].Content != "" || list[0].SHA256 != second.SHA256 {
		t.Fatalf("revisions = %+v, %v", list, err)
	}
}
