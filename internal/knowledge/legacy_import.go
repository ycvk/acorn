package knowledge

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ycvk/acorn/internal/core"
)

// COMPAT: the git-backed knowledge directory written before notes moved into
// SQLite — remove after the deployed installation has started once on this
// release.

// LegacyImporter stores notes from the former knowledge directory.
type LegacyImporter interface {
	ImportLegacyKnowledge(ctx context.Context, notes []core.KnowledgeNote) error
}

// ImportLegacyDir moves {storage}/knowledge into SQLite when it is still a git
// repository: attachments move to {storage}/attachments, notes become their
// first revision, and the directory is removed.
func ImportLegacyDir(ctx context.Context, storageDir string, importer LegacyImporter, loc *time.Location) error {
	dir := filepath.Join(storageDir, "knowledge")
	if _, err := os.Stat(filepath.Join(dir, ".git")); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect legacy knowledge %s: %w", dir, err)
	}
	var notes []core.KnowledgeNote
	err := filepath.WalkDir(dir, func(abs string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(dir, abs)
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
		info, err := d.Info()
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(abs)
		if err != nil {
			return err
		}
		note, err := parseLegacyNote(rel, raw, info.ModTime(), loc)
		if err != nil {
			return err
		}
		notes = append(notes, note)
		return nil
	})
	if err != nil {
		return fmt.Errorf("read legacy knowledge %s: %w", dir, err)
	}
	legacyAttachments := filepath.Join(dir, attachmentsDir)
	if _, err := os.Stat(legacyAttachments); err == nil {
		if err := os.Rename(legacyAttachments, filepath.Join(storageDir, attachmentsDir)); err != nil {
			return fmt.Errorf("move legacy attachments: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect legacy attachments: %w", err)
	}
	if err := importer.ImportLegacyKnowledge(ctx, notes); err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove legacy knowledge %s: %w", dir, err)
	}
	return nil
}

var legacyTimeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05",
	"2006-01-02T15:04",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}

func parseLegacyNote(notePath string, raw []byte, mtime time.Time, loc *time.Location) (core.KnowledgeNote, error) {
	if _, err := CleanNotePath(notePath); err != nil {
		return core.KnowledgeNote{}, err
	}
	note := core.KnowledgeNote{Path: notePath, Revision: 1}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	body := text
	if rest, ok := strings.CutPrefix(text, "---\n"); ok {
		if header, after, found := strings.Cut(rest, "\n---\n"); found {
			body = after
			var fm struct {
				Title   string `yaml:"title"`
				Tags    any    `yaml:"tags"`
				Source  string `yaml:"source"`
				Created string `yaml:"created"`
				Updated string `yaml:"updated"`
			}
			if err := yaml.Unmarshal([]byte(header), &fm); err != nil {
				return core.KnowledgeNote{}, fmt.Errorf("parse frontmatter of %s: %w", notePath, err)
			}
			note.Title, note.Source = strings.TrimSpace(fm.Title), strings.TrimSpace(fm.Source)
			switch tags := fm.Tags.(type) {
			case []any:
				for _, tag := range tags {
					note.Tags = append(note.Tags, fmt.Sprint(tag))
				}
			case string:
				note.Tags = strings.FieldsFunc(tags, func(r rune) bool { return r == ',' || r == ' ' })
			}
			note.Tags = normalizeTags(note.Tags)
			var err error
			if note.CreatedAt, err = parseLegacyTime(fm.Created, loc); err != nil {
				return core.KnowledgeNote{}, fmt.Errorf("%s created: %w", notePath, err)
			}
			if note.UpdatedAt, err = parseLegacyTime(fm.Updated, loc); err != nil {
				return core.KnowledgeNote{}, fmt.Errorf("%s updated: %w", notePath, err)
			}
		}
	}
	note.Body = strings.TrimSpace(body)
	if note.Title == "" {
		for _, line := range strings.Split(note.Body, "\n") {
			if title, ok := strings.CutPrefix(strings.TrimSpace(line), "# "); ok {
				note.Title = strings.TrimSpace(title)
				break
			}
		}
	}
	if note.Title == "" {
		note.Title = strings.TrimSuffix(path.Base(notePath), ".md")
	}
	if note.UpdatedAt.IsZero() {
		note.UpdatedAt = mtime
	}
	if note.CreatedAt.IsZero() {
		note.CreatedAt = note.UpdatedAt
	}
	return note, nil
}

func parseLegacyTime(value string, loc *time.Location) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	for _, layout := range legacyTimeLayouts {
		if t, err := time.ParseInLocation(layout, value, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized time %q", value)
}
