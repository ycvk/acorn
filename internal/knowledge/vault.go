// Package knowledge owns the knowledge base: notes stored as revisions in
// SQLite.
package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/textedit"
)

// ErrInvalidPath reports a note path CleanNotePath rejects.
var ErrInvalidPath = errors.New("invalid knowledge path")

// MaxNoteBodyBytes bounds one note's body.
const MaxNoteBodyBytes = 256 << 10

// VaultConfig carries the dependencies of a Vault; all are required.
type VaultConfig struct {
	Store core.KnowledgeStore
	Clock func() time.Time
}

// Vault is the only writer of the knowledge base.
type Vault struct {
	store core.KnowledgeStore
	clock func() time.Time
}

func NewVault(cfg VaultConfig) (*Vault, error) {
	switch {
	case cfg.Store == nil:
		return nil, errors.New("knowledge vault: store is required")
	case cfg.Clock == nil:
		return nil, errors.New("knowledge vault: clock is required")
	}
	return &Vault{store: cfg.Store, clock: cfg.Clock}, nil
}

// WriteNote is a whole-note write. RunID attributes it to an agent run.
type WriteNote struct {
	Path   string
	Title  string
	Tags   []string
	Source string
	Body   string
	RunID  string
}

// Write creates or replaces a note as a new revision; a replaced note keeps
// its created time.
func (v *Vault) Write(ctx context.Context, w WriteNote) (core.KnowledgeNote, error) {
	notePath, err := CleanNotePath(w.Path)
	if err != nil {
		return core.KnowledgeNote{}, err
	}
	if strings.TrimSpace(w.Title) == "" {
		return core.KnowledgeNote{}, errors.New("knowledge write: title is required")
	}
	return v.write(ctx, core.KnowledgeWrite{Path: notePath, Title: strings.TrimSpace(w.Title), Tags: normalizeTags(w.Tags), Source: strings.TrimSpace(w.Source), Body: w.Body, RunID: w.RunID}, "write")
}

// Edit makes edits to a note's body, each replacing the one place its old
// text occurs. The write is based on the revision it read, so a concurrent
// change fails it.
func (v *Vault) Edit(ctx context.Context, notePath string, edits []textedit.Edit, runID string) (core.KnowledgeNote, error) {
	notePath, err := CleanNotePath(notePath)
	if err != nil {
		return core.KnowledgeNote{}, err
	}
	note, err := v.store.KnowledgeNote(ctx, notePath)
	if err != nil {
		return core.KnowledgeNote{}, err
	}
	body, _, err := textedit.Apply(note.Body, edits)
	if err != nil {
		return core.KnowledgeNote{}, fmt.Errorf("knowledge edit %s: %w", notePath, err)
	}
	return v.write(ctx, core.KnowledgeWrite{Path: notePath, Title: note.Title, Tags: note.Tags, Source: note.Source, Body: body, RunID: runID, BaseRevision: note.Revision}, "edit")
}

func (v *Vault) write(ctx context.Context, w core.KnowledgeWrite, verb string) (core.KnowledgeNote, error) {
	w.Body = strings.TrimSpace(w.Body)
	if len(w.Body) > MaxNoteBodyBytes {
		return core.KnowledgeNote{}, fmt.Errorf("knowledge %s: body is %d bytes, over the %d byte limit; split the note", verb, len(w.Body), MaxNoteBodyBytes)
	}
	w.At = v.clock()
	return v.store.WriteKnowledgeNote(ctx, w)
}

// Delete removes a note the owner deleted, at the revision the caller read
// and forgot; a concurrent change fails it.
func (v *Vault) Delete(ctx context.Context, notePath string, revision int64) error {
	notePath, err := CleanNotePath(notePath)
	if err != nil {
		return err
	}
	return v.store.DeleteKnowledgeNote(ctx, notePath, revision)
}

// Read returns the current revision of one note.
func (v *Vault) Read(ctx context.Context, notePath string) (core.KnowledgeNote, error) {
	notePath, err := CleanNotePath(notePath)
	if err != nil {
		return core.KnowledgeNote{}, err
	}
	return v.store.KnowledgeNote(ctx, notePath)
}

func (v *Vault) Search(ctx context.Context, query string, limit int) ([]core.KnowledgeHit, error) {
	return v.store.SearchKnowledge(ctx, query, limit)
}

// Recent lists notes under prefix, newest first.
func (v *Vault) Recent(ctx context.Context, prefix string, limit int) ([]core.KnowledgeHit, error) {
	return v.store.RecentKnowledge(ctx, prefix, limit)
}

// Status describes the knowledge base for diagnostics.
type Status struct {
	Notes int
}

func (v *Vault) Status(ctx context.Context) (Status, error) {
	n, err := v.store.CountKnowledgeNotes(ctx)
	if err != nil {
		return Status{}, err
	}
	return Status{Notes: n}, nil
}
