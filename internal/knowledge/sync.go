package knowledge

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ycvk/acorn/internal/core"
)

// Status describes the knowledge base for diagnostics.
type Status struct {
	Dir   string
	Git   string
	Notes int
}

// Status syncs the index and reports the directory, git version and number
// of indexed notes.
func (v *Vault) Status(ctx context.Context) (Status, error) {
	version, err := v.git.Version(ctx)
	if err != nil {
		return Status{}, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.syncLocked(ctx); err != nil {
		return Status{}, err
	}
	stats, err := v.index.ListKnowledgeFileStats(ctx)
	if err != nil {
		return Status{}, err
	}
	return Status{Dir: v.dir, Git: version, Notes: len(stats)}, nil
}

// Sync brings the index in line with the files: new and changed notes are
// parsed and indexed, removed ones dropped. Hidden entries and attachments/
// are skipped. A note that cannot be read or parsed fails the sync.
func (v *Vault) Sync(ctx context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.syncLocked(ctx)
}

func (v *Vault) syncLocked(ctx context.Context) error {
	indexed, err := v.index.ListKnowledgeFileStats(ctx)
	if err != nil {
		return err
	}
	known := make(map[string]core.KnowledgeFileStat, len(indexed))
	for _, st := range indexed {
		known[st.Path] = st
	}
	seen := map[string]bool{}
	err = filepath.WalkDir(v.dir, func(abs string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(v.dir, abs)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || (d.IsDir() && rel == attachmentsDir) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !strings.HasSuffix(rel, ".md") {
			return nil
		}
		seen[rel] = true
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", rel, err)
		}
		if st, ok := known[rel]; ok && st.MTimeNS == info.ModTime().UnixNano() && st.Size == info.Size() {
			return nil
		}
		raw, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Errorf("read %s: %w", rel, err)
		}
		fm, body, err := ParseNote(rel, raw, info.ModTime(), v.loc)
		if err != nil {
			return err
		}
		return v.index.UpsertKnowledgeNote(ctx, indexEntry(rel, fm, body, info))
	})
	if err != nil {
		return fmt.Errorf("sync knowledge index: %w", err)
	}
	for notePath := range known {
		if seen[notePath] {
			continue
		}
		if err := v.index.DeleteKnowledgeNote(ctx, notePath); err != nil {
			return err
		}
	}
	return nil
}
