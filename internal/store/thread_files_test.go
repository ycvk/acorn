package store

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

func saveFile(t *testing.T, s *Store, in core.ThreadFileWrite) core.ThreadFile {
	t.Helper()
	file, err := s.SaveThreadFile(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func TestThreadFilesAreSentOnceAndLeaveWithTheirThread(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range []string{"books", "other"} {
		if _, err := s.CreateSession(ctx, id, id); err != nil {
			t.Fatal(err)
		}
	}
	owner := core.ThreadFileWrite{SessionID: "books", Origin: core.ThreadFileFromOwner, Name: "notes.txt", Content: strings.NewReader("第一回"), MaxBytes: 64}
	notes := saveFile(t, s, owner)
	if notes.MIME != "text/plain" || notes.Size != int64(len("第一回")) || notes.MessageID != 0 {
		t.Fatalf("saved = %+v", notes)
	}
	elsewhere := saveFile(t, s, core.ThreadFileWrite{SessionID: "other", Origin: core.ThreadFileFromOwner, Name: "a.txt", Content: strings.NewReader("a"), MaxBytes: 64})
	sent := saveFile(t, s, core.ThreadFileWrite{SessionID: "books", RunID: "run_1", Origin: core.ThreadFileFromAgent, Name: "book.epub", Content: strings.NewReader("PK\x03\x04"), MaxBytes: 64})

	for _, ids := range [][]string{{elsewhere.ID}, {sent.ID}, {notes.ID, notes.ID}} {
		if err := s.AttachThreadFiles(ctx, "books", 7, ids); !errors.Is(err, core.ErrInvalidThreadFile) {
			t.Fatalf("attach %v err = %v, want ErrInvalidThreadFile", ids, err)
		}
	}
	if err := s.AttachThreadFiles(ctx, "books", 7, []string{notes.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachThreadFiles(ctx, "books", 8, []string{notes.ID}); !errors.Is(err, core.ErrInvalidThreadFile) {
		t.Fatalf("second attach err = %v, want ErrInvalidThreadFile", err)
	}
	if _, err := s.SaveThreadFile(ctx, core.ThreadFileWrite{SessionID: "books", Origin: core.ThreadFileFromOwner, Name: "big.bin", Content: strings.NewReader(strings.Repeat("x", 65)), MaxBytes: 64}); !errors.Is(err, core.ErrThreadFileTooLarge) {
		t.Fatalf("oversized err = %v", err)
	}
	if _, err := s.SaveThreadFile(ctx, core.ThreadFileWrite{SessionID: "missing", Origin: core.ThreadFileFromOwner, Name: "a.txt", Content: strings.NewReader("a"), MaxBytes: 64}); !errors.Is(err, core.ErrSessionNotFound) {
		t.Fatalf("missing thread err = %v", err)
	}

	files, err := s.ThreadFiles(ctx, "books")
	if err != nil || len(files) != 2 || files[0].MessageID != 7 || files[1].RunID != "run_1" {
		t.Fatalf("thread files = %+v, %v", files, err)
	}
	_, content, err := s.OpenThreadFile(ctx, "books", notes.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(content)
	_ = content.Close()
	if string(data) != "第一回" {
		t.Fatalf("content = %q", data)
	}
	if _, _, err := s.OpenThreadFile(ctx, "other", notes.ID); !errors.Is(err, core.ErrThreadFileNotFound) {
		t.Fatalf("open from another thread err = %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "files")); len(entries) != 3 {
		t.Fatalf("files on disk = %d, want 3 with no leftovers", len(entries))
	}

	if err := s.DeleteThread(ctx, "books"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{notes.ID, sent.ID} {
		if _, err := os.Stat(filepath.Join(dir, "files", id)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("file %s after delete: %v", id, err)
		}
	}
	if ids, err := s.OwnerThreadFileIDs(ctx); err != nil || len(ids) != 1 || ids[0] != elsewhere.ID {
		t.Fatalf("owner files = %v, %v", ids, err)
	}
}
