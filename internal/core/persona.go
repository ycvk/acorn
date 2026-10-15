package core

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrPersonaNotFound means no persona revision exists, or not the one
	// asked for.
	ErrPersonaNotFound = errors.New("persona not found")
	// ErrPersonaConflict means the persona is not at the revision a write was
	// based on.
	ErrPersonaConflict = errors.New("persona changed concurrently")
	// ErrPersonaInvalid means a persona's content is empty or too long.
	ErrPersonaInvalid = errors.New("invalid persona")
)

// PersonaAuthor says who wrote a persona revision.
type PersonaAuthor string

const (
	PersonaAuthorOwner   PersonaAuthor = "owner"
	PersonaAuthorRelease PersonaAuthor = "release"
)

// PersonaRevision is one saved persona. DeviceID is the paired device it was
// saved from, empty for the CLI and the release. Listings leave Content empty.
type PersonaRevision struct {
	Revision int64
	Content  string
	SHA256   string
	Author   PersonaAuthor
	DeviceID string
	At       time.Time
}

// PersonaWrite saves a new persona revision. BaseRevision must equal the
// current revision, zero when no persona exists yet.
type PersonaWrite struct {
	Content      string
	Author       PersonaAuthor
	DeviceID     string
	At           time.Time
	BaseRevision int64
}

// PersonaStore holds the persona. Every save is a new revision and the
// newest one is the current persona.
type PersonaStore interface {
	WritePersona(ctx context.Context, write PersonaWrite) (PersonaRevision, error)
	// Persona reads the current revision.
	Persona(ctx context.Context) (PersonaRevision, error)
	// PersonaRevisions lists every revision without content, newest first.
	PersonaRevisions(ctx context.Context) ([]PersonaRevision, error)
	PersonaRevision(ctx context.Context, revision int64) (PersonaRevision, error)
}
