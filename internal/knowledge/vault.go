// Package knowledge owns the knowledge base: a directory of markdown notes
// that is also a git repository. Files are the truth; the SQLite index is
// rebuilt from them on demand.
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
	"sync"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

var (
	ErrInvalidPath  = errors.New("invalid knowledge path")
	ErrNoteNotFound = errors.New("knowledge note not found")
)

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
type VaultConfig struct {
	Dir      string
	Git      Git
	Index    core.KnowledgeStore
	Clock    func() time.Time
	Location *time.Location
}

// Vault is the only writer of the knowledge directory. Every write is one
// commit and updates the index; reads that list or search sync the index with
// the files first.
type Vault struct {
	dir   string
	git   Git
	index core.KnowledgeStore
	clock func() time.Time
	loc   *time.Location
	mu    sync.Mutex
}

// Open creates the directory and repository when missing and syncs the index.
func Open(ctx context.Context, cfg VaultConfig) (*Vault, error) {
	switch {
	case strings.TrimSpace(cfg.Dir) == "":
		return nil, errors.New("knowledge vault: dir is required")
	case cfg.Git == nil:
		return nil, errors.New("knowledge vault: git is required")
	case cfg.Index == nil:
		return nil, errors.New("knowledge vault: index is required")
	case cfg.Clock == nil:
		return nil, errors.New("knowledge vault: clock is required")
	case cfg.Location == nil:
		return nil, errors.New("knowledge vault: location is required")
	}
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("create knowledge dir %s: %w", cfg.Dir, err)
	}
	if err := cfg.Git.Init(ctx, cfg.Dir); err != nil {
		return nil, fmt.Errorf("init knowledge repository %s: %w", cfg.Dir, err)
	}
	v := &Vault{dir: cfg.Dir, git: cfg.Git, index: cfg.Index, clock: cfg.Clock, loc: cfg.Location}
	if err := v.Sync(ctx); err != nil {
		return nil, err
	}
	return v, nil
}

// Dir is the knowledge directory.
func (v *Vault) Dir() string { return v.dir }

// WriteNote is a whole-note write. RunID, when set, goes into the commit.
type WriteNote struct {
	Path   string
	Title  string
	Tags   []string
	Source string
	Body   string
	RunID  string
}

// Write creates or replaces a note. A replaced note keeps its created time
// and the frontmatter keys Acorn does not manage.
func (v *Vault) Write(ctx context.Context, w WriteNote) (Note, error) {
	notePath, err := CleanNotePath(w.Path)
	if err != nil {
		return Note{}, err
	}
	if strings.TrimSpace(w.Title) == "" {
		return Note{}, errors.New("knowledge write: title is required")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	now := v.clock()
	fm := Frontmatter{Created: now}
	if existing, err := v.readLocked(notePath); err == nil {
		fm = existing.Frontmatter
		if fm.Created.IsZero() {
			fm.Created = now
		}
	} else if !errors.Is(err, ErrNoteNotFound) {
		return Note{}, err
	}
	fm.Title = strings.TrimSpace(w.Title)
	fm.Tags = normalizeTags(w.Tags)
	fm.Source = strings.TrimSpace(w.Source)
	fm.Updated = now
	return v.commitNote(ctx, notePath, fm, w.Body, "write", w.RunID)
}

// Edit replaces the single occurrence of old in a note's body with new.
func (v *Vault) Edit(ctx context.Context, notePath, old, replacement, runID string) (Note, error) {
	notePath, err := CleanNotePath(notePath)
	if err != nil {
		return Note{}, err
	}
	if old == "" {
		return Note{}, errors.New("knowledge edit: old text is required")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	note, err := v.readLocked(notePath)
	if err != nil {
		return Note{}, err
	}
	switch count := strings.Count(note.Body, old); count {
	case 0:
		return Note{}, fmt.Errorf("knowledge edit: the old text does not occur in %s", notePath)
	case 1:
	default:
		return Note{}, fmt.Errorf("knowledge edit: the old text occurs %d times in %s; include more context so it is unique", count, notePath)
	}
	fm := note.Frontmatter
	if fm.Created.IsZero() {
		fm.Created = fm.Updated
	}
	fm.Updated = v.clock()
	return v.commitNote(ctx, notePath, fm, strings.Replace(note.Body, old, replacement, 1), "edit", runID)
}

// Read returns one note; a missing file is ErrNoteNotFound.
func (v *Vault) Read(_ context.Context, notePath string) (Note, error) {
	notePath, err := CleanNotePath(notePath)
	if err != nil {
		return Note{}, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.readLocked(notePath)
}

// Search syncs the index and searches it.
func (v *Vault) Search(ctx context.Context, query string, limit int) ([]core.KnowledgeHit, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.syncLocked(ctx); err != nil {
		return nil, err
	}
	return v.index.SearchKnowledge(ctx, query, limit)
}

// Recent syncs the index and lists notes under prefix, newest first.
func (v *Vault) Recent(ctx context.Context, prefix string, limit int) ([]core.KnowledgeHit, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.syncLocked(ctx); err != nil {
		return nil, err
	}
	return v.index.RecentKnowledge(ctx, prefix, limit)
}

// SaveAttachment stores an image under attachments/YYYY/MM/ and commits it
// with message. The extension follows mime; other types are rejected.
func (v *Vault) SaveAttachment(ctx context.Context, mime string, data []byte, message string) (string, error) {
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
	v.mu.Lock()
	defer v.mu.Unlock()
	now := v.clock().In(v.loc)
	rel := path.Join(attachmentsDir, now.Format("2006"), now.Format("01"), hex.EncodeToString(id)+"."+ext)
	if err := writeFileAtomic(v.abs(rel), data); err != nil {
		return "", err
	}
	if _, err := v.git.Commit(ctx, v.dir, []string{rel}, message); err != nil {
		return "", fmt.Errorf("commit attachment %s: %w", rel, err)
	}
	return rel, nil
}

func (v *Vault) commitNote(ctx context.Context, notePath string, fm Frontmatter, body, verb, runID string) (Note, error) {
	body = strings.TrimSpace(body)
	if len(body) > MaxNoteBodyBytes {
		return Note{}, fmt.Errorf("knowledge %s: body is %d bytes, over the %d byte limit; split the note", verb, len(body), MaxNoteBodyBytes)
	}
	raw, err := RenderNote(fm, body, v.loc)
	if err != nil {
		return Note{}, err
	}
	abs := v.abs(notePath)
	if err := writeFileAtomic(abs, raw); err != nil {
		return Note{}, err
	}
	message := fmt.Sprintf("knowledge: %s %s", verb, notePath)
	if runID != "" {
		message += "\n\nAcorn-Run: " + runID
	}
	sha, err := v.git.Commit(ctx, v.dir, []string{notePath}, message)
	if err != nil {
		return Note{}, fmt.Errorf("commit %s: %w", notePath, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Note{}, fmt.Errorf("stat %s: %w", notePath, err)
	}
	if err := v.index.UpsertKnowledgeNote(ctx, indexEntry(notePath, fm, body, info)); err != nil {
		return Note{}, err
	}
	return Note{Path: notePath, Frontmatter: fm, Body: body, Commit: sha}, nil
}

func (v *Vault) readLocked(notePath string) (Note, error) {
	abs := v.abs(notePath)
	info, err := os.Stat(abs)
	if errors.Is(err, os.ErrNotExist) {
		return Note{}, fmt.Errorf("%w: %s", ErrNoteNotFound, notePath)
	}
	if err != nil {
		return Note{}, fmt.Errorf("stat %s: %w", notePath, err)
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return Note{}, fmt.Errorf("read %s: %w", notePath, err)
	}
	fm, body, err := ParseNote(notePath, raw, info.ModTime(), v.loc)
	if err != nil {
		return Note{}, err
	}
	return Note{Path: notePath, Frontmatter: fm, Body: body}, nil
}

func (v *Vault) abs(rel string) string {
	return filepath.Join(v.dir, filepath.FromSlash(rel))
}

func indexEntry(notePath string, fm Frontmatter, body string, info os.FileInfo) core.KnowledgeNote {
	return core.KnowledgeNote{
		Path:      notePath,
		Title:     fm.Title,
		Tags:      fm.Tags,
		Body:      body,
		MTimeNS:   info.ModTime().UnixNano(),
		Size:      info.Size(),
		UpdatedAt: fm.Updated,
	}
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
