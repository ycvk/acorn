// Package knowledge owns the knowledge base: notes stored as revisions in
// SQLite, and image attachments kept as files.
package knowledge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

var ErrInvalidPath = errors.New("invalid knowledge path")

const (
	attachmentsDir = "attachments"
	// MaxNoteBodyBytes bounds one note's body.
	MaxNoteBodyBytes = 256 << 10
)

var attachmentExtensions = map[string]string{
	"image/jpeg": "jpg",
	"image/png":  "png",
	"image/webp": "webp",
	"image/gif":  "gif",
}

// SupportsAttachment reports whether SaveAttachment accepts mime.
func SupportsAttachment(mime string) bool {
	_, ok := attachmentExtensions[mime]
	return ok
}

// VaultConfig carries the dependencies of a Vault; all are required.
// StorageDir holds attachments/.
type VaultConfig struct {
	Store      core.KnowledgeStore
	StorageDir string
	Clock      func() time.Time
	Location   *time.Location
}

// Vault is the only writer of the knowledge base.
type Vault struct {
	store      core.KnowledgeStore
	storageDir string
	clock      func() time.Time
	loc        *time.Location
}

func NewVault(cfg VaultConfig) (*Vault, error) {
	switch {
	case cfg.Store == nil:
		return nil, errors.New("knowledge vault: store is required")
	case strings.TrimSpace(cfg.StorageDir) == "":
		return nil, errors.New("knowledge vault: storage dir is required")
	case cfg.Clock == nil:
		return nil, errors.New("knowledge vault: clock is required")
	case cfg.Location == nil:
		return nil, errors.New("knowledge vault: location is required")
	}
	return &Vault{store: cfg.Store, storageDir: cfg.StorageDir, clock: cfg.Clock, loc: cfg.Location}, nil
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

// Edit replaces the single occurrence of old in a note's body. The write is
// based on the revision it read, so a concurrent change fails it.
func (v *Vault) Edit(ctx context.Context, notePath, old, replacement, runID string) (core.KnowledgeNote, error) {
	notePath, err := CleanNotePath(notePath)
	if err != nil {
		return core.KnowledgeNote{}, err
	}
	if old == "" {
		return core.KnowledgeNote{}, errors.New("knowledge edit: old text is required")
	}
	note, err := v.store.KnowledgeNote(ctx, notePath)
	if err != nil {
		return core.KnowledgeNote{}, err
	}
	switch count := strings.Count(note.Body, old); count {
	case 0:
		return core.KnowledgeNote{}, fmt.Errorf("knowledge edit: the old text does not occur in %s", notePath)
	case 1:
	default:
		return core.KnowledgeNote{}, fmt.Errorf("knowledge edit: the old text occurs %d times in %s; include more context so it is unique", count, notePath)
	}
	return v.write(ctx, core.KnowledgeWrite{Path: notePath, Title: note.Title, Tags: note.Tags, Source: note.Source, Body: strings.Replace(note.Body, old, replacement, 1), RunID: runID, BaseRevision: note.Revision}, "edit")
}

func (v *Vault) write(ctx context.Context, w core.KnowledgeWrite, verb string) (core.KnowledgeNote, error) {
	w.Body = strings.TrimSpace(w.Body)
	if len(w.Body) > MaxNoteBodyBytes {
		return core.KnowledgeNote{}, fmt.Errorf("knowledge %s: body is %d bytes, over the %d byte limit; split the note", verb, len(w.Body), MaxNoteBodyBytes)
	}
	w.At = v.clock()
	return v.store.WriteKnowledgeNote(ctx, w)
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

// SaveAttachment stores an image under attachments/YYYY/MM/ in the storage
// dir and returns its path relative to the storage dir. The extension
// follows mime; other types are rejected.
func (v *Vault) SaveAttachment(_ context.Context, mime string, data []byte) (string, error) {
	ext, ok := attachmentExtensions[mime]
	if !ok {
		return "", fmt.Errorf("unsupported attachment type %q", mime)
	}
	if len(data) == 0 {
		return "", errors.New("attachment is empty")
	}
	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return "", fmt.Errorf("attachment id: %w", err)
	}
	now := v.clock().In(v.loc)
	rel := path.Join(attachmentsDir, now.Format("2006"), now.Format("01"), hex.EncodeToString(id)+"."+ext)
	if err := writeFileAtomic(filepath.Join(v.storageDir, filepath.FromSlash(rel)), data); err != nil {
		return "", err
	}
	return rel, nil
}

// Status describes the knowledge base for diagnostics.
type Status struct {
	Notes       int
	Attachments string
}

func (v *Vault) Status(ctx context.Context) (Status, error) {
	n, err := v.store.CountKnowledgeNotes(ctx)
	if err != nil {
		return Status{}, err
	}
	return Status{Notes: n, Attachments: filepath.Join(v.storageDir, attachmentsDir)}, nil
}

func writeFileAtomic(abs string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(abs), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".acorn-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", abs, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", abs, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", abs, err)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", abs, err)
	}
	if err := os.Rename(tmp.Name(), abs); err != nil {
		return fmt.Errorf("write %s: %w", abs, err)
	}
	return nil
}
