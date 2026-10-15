package presence

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// MaxPersonaBytes bounds the persona, which enters every run's instruction.
const MaxPersonaBytes = 8 << 10

// DefaultPersona is the release's first persona revision. The owner edits it
// freely; operating rules for tools live in the runtime, not here.
const DefaultPersona = `You are Acorn, the owner's personal agent. You run on the owner's own server and
work only for them.

- You have a continuous life: you remember what the owner told you, keep your
  own thoughts, and keep the appointments you make.
- Be direct and brief. Say what you did, what you found and what you need.
- Reply in the language the owner writes in.
- When something matters and the owner is not around, reach out instead of
  waiting to be asked.
`

// SavePersona saves the trimmed content as a new revision on top of
// write.BaseRevision.
func SavePersona(ctx context.Context, store core.PersonaStore, write core.PersonaWrite) (core.PersonaRevision, error) {
	content := strings.TrimSpace(write.Content)
	if content == "" {
		return core.PersonaRevision{}, fmt.Errorf("%w: the persona is empty", core.ErrPersonaInvalid)
	}
	if len(content) > MaxPersonaBytes {
		return core.PersonaRevision{}, fmt.Errorf("%w: the persona is %d bytes, over the %d byte limit", core.ErrPersonaInvalid, len(content), MaxPersonaBytes)
	}
	write.Content = content
	return store.WritePersona(ctx, write)
}

// SeedPersona saves the default persona as the release's first revision when
// no persona exists yet. An existing persona is the owner's and stays.
func SeedPersona(ctx context.Context, store core.PersonaStore, at time.Time) error {
	_, err := store.Persona(ctx)
	if !errors.Is(err, core.ErrPersonaNotFound) {
		return err
	}
	_, err = SavePersona(ctx, store, core.PersonaWrite{Content: DefaultPersona, Author: core.PersonaAuthorRelease, At: at})
	if errors.Is(err, core.ErrPersonaConflict) {
		// Another Acorn process starting at the same time seeded it first.
		return nil
	}
	return err
}
