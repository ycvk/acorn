package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var unsafeNameChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func (s *LocalService) AppendHistory(ctx context.Context, event HistoryEvent) error {
	if s == nil {
		return fmt.Errorf("memory service is nil")
	}
	if err := s.EnsureLayout(ctx); err != nil {
		return err
	}
	sessionID := sanitizeName(firstNonEmpty(event.SessionID, "standalone"))
	if sessionID == "" {
		return fmt.Errorf("history session id is required")
	}
	ts := event.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	line := formatHistoryLine(ts, event)
	path := s.path("history", sessionID+".md")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open history file %q: %w", path, err)
	}
	if _, err := file.WriteString(line); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		return fmt.Errorf("append history file %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close history file %q: %w", path, err)
	}
	if err := s.BuildIndex(ctx); err != nil {
		return fmt.Errorf("rebuild index after history append: %w", err)
	}
	// Re-embed the updated history file. Best-effort: the record is already
	// on disk and keyword-indexed; a failed embedding just means semantic
	// search won't find this history until the next append retries.
	if data, rErr := os.ReadFile(path); rErr == nil {
		if record, hErr := historyRecordFromFile(s.root, path, data, time.Now()); hErr == nil {
			s.indexEmbedding(ctx, *record)
		}
	}
	return nil
}

func (s *LocalService) ListHistory(ctx context.Context, selection RecordSelection) ([]Record, error) {
	if s == nil {
		return nil, fmt.Errorf("memory service is nil")
	}
	if err := s.EnsureLayout(ctx); err != nil {
		return nil, err
	}
	records, err := s.listHistory(ctx)
	if err != nil {
		return nil, err
	}
	return s.selectRecords(ctx, records, selection)
}

func (s *LocalService) listHistory(ctx context.Context) ([]Record, error) {
	root := s.path("history")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read history directory: %w", err)
	}
	records := make([]Record, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("stat history file %q: %w", entry.Name(), err)
		}
		path := filepath.Join(root, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read history file %q: %w", path, err)
		}
		record, err := historyRecordFromFile(s.root, path, data, info.ModTime())
		if err != nil {
			return nil, err
		}
		records = append(records, *record)
	}
	return records, nil
}

func formatHistoryLine(ts time.Time, event HistoryEvent) string {
	parts := []string{
		ts.UTC().Format(time.RFC3339),
		strings.TrimSpace(event.RunID),
		strings.TrimSpace(event.Status),
		compactWhitespace(event.Summary),
	}
	line := "- " + strings.Join(nonEmpty(parts), " ") + "."
	if len(event.FilesChanged) > 0 {
		line += " files changed: " + strings.Join(nonEmpty(event.FilesChanged), ", ") + "."
	}
	return line + "\n"
}

func sanitizeName(value string) string {
	trimmed := strings.Trim(unsafeNameChars.ReplaceAllString(strings.TrimSpace(value), "-"), "-")
	return trimmed
}

func historyRecordFromFile(root string, path string, data []byte, modTime time.Time) (*Record, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil, fmt.Errorf("resolve history relative path: %w", err)
	}
	rel = filepath.ToSlash(rel)
	body := strings.TrimSpace(string(data))
	date := historyRecordDate(body, modTime)
	title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return &Record{
		Ref:      rel,
		Kind:     KindHistory,
		RootPath: root,
		RelPath:  rel,
		Title:    title,
		Status:   StatusVerified,
		Scope:    "user",
		Tags:     []string{"history"},
		Body:     body,
		Created:  date,
		Updated:  date,
	}, nil
}

func historyRecordDate(body string, modTime time.Time) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
		if trimmed == "" {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			continue
		}
		ts, err := time.Parse(time.RFC3339, fields[0])
		if err == nil {
			return ts.UTC().Format("2006-01-02")
		}
		break
	}
	if modTime.IsZero() {
		return time.Now().UTC().Format("2006-01-02")
	}
	return modTime.UTC().Format("2006-01-02")
}
