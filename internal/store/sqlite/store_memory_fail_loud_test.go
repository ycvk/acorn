package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGetRunArchiveFailsOnInvalidTouchedPathsJSON(t *testing.T) {
	store := openStoreMemoryFailLoudTestStore(t)
	if err := store.CreateRun(context.Background(), "run_bad_touched_paths", "inspect repo", ""); err != nil {
		t.Fatalf("create run: %v", err)
	}
	_, err := store.db.ExecContext(
		context.Background(),
		`INSERT INTO run_archives(run_id, session_id, input_excerpt, output_excerpt, touched_paths_json, tool_names_json, run_status, created_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		"run_bad_touched_paths",
		"",
		"inspect repo",
		"done",
		"{bad json",
		`["read_file"]`,
		"succeeded",
		formatTimestamp(time.Now().UTC()),
	)
	if err != nil {
		t.Fatalf("insert run archive: %v", err)
	}

	_, err = store.GetRunArchive(context.Background(), "run_bad_touched_paths")
	if err == nil || !strings.Contains(err.Error(), "unmarshal run archive touched paths") {
		t.Fatalf("GetRunArchive error = %v, want touched-path decode failure", err)
	}
}

func TestGetRunArchiveFailsOnInvalidToolNamesJSON(t *testing.T) {
	store := openStoreMemoryFailLoudTestStore(t)
	if err := store.CreateRun(context.Background(), "run_bad_tool_names", "inspect repo", ""); err != nil {
		t.Fatalf("create run: %v", err)
	}
	_, err := store.db.ExecContext(
		context.Background(),
		`INSERT INTO run_archives(run_id, session_id, input_excerpt, output_excerpt, touched_paths_json, tool_names_json, run_status, created_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		"run_bad_tool_names",
		"",
		"inspect repo",
		"done",
		`["README.md"]`,
		"{bad json",
		"succeeded",
		formatTimestamp(time.Now().UTC()),
	)
	if err != nil {
		t.Fatalf("insert run archive: %v", err)
	}

	_, err = store.GetRunArchive(context.Background(), "run_bad_tool_names")
	if err == nil || !strings.Contains(err.Error(), "unmarshal run archive tool names") {
		t.Fatalf("GetRunArchive error = %v, want tool-name decode failure", err)
	}
}

func openStoreMemoryFailLoudTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
