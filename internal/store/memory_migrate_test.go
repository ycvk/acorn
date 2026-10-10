package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

const legacyMemorySchema = `CREATE TABLE memory_items(id INTEGER PRIMARY KEY,kind TEXT NOT NULL,content TEXT NOT NULL,status TEXT NOT NULL,session_id TEXT NOT NULL,source_run_id TEXT NOT NULL,wake_at TEXT NOT NULL,recurrence TEXT NOT NULL,expires_at TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL); DELETE FROM schema_migrations WHERE version='v6_personal_memory';`

func TestMemoryMigrationPreservesCommitmentsAndReleasedHistory(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if _, err := s.db.Exec(legacyMemorySchema); err != nil {
		t.Fatal(err)
	}
	stamp := formatTimestamp(memoryTestNow)
	for _, i := range []struct {
		id                               int
		kind, state, content, recurrence string
	}{{1, "commitment", "active", "早晨提醒", "0 9 * * *"}, {2, "commitment", "woken", "已唤醒", "0 9 * * *"}, {3, "commitment", "settled", "已完成", ""}, {4, "commitment", "released", "已取消", ""}, {5, "said", "released", "我不吃香菜", ""}, {6, "tendency", "active", "喜欢清淡", ""}, {7, "said", "active", "每周六游泳", ""}} {
		if _, err := s.db.Exec(`INSERT INTO memory_items VALUES(?,?,?,?,?,?,?,?,?,?,?)`, i.id, i.kind, i.content, i.state, "thread", "old_run", stamp, i.recurrence, "", stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CreateRun(ctx, core.RunCreateParams{RunID: "old_run", Input: "wake"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendEvent(ctx, "old_run", core.EventWakeFired, map[string]any{"memory_id": 2}); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRun(ctx, "old_run", core.RunStatusSucceeded, "完成", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.migratePersonalMemory(); err != nil {
		t.Fatal(err)
	}
	for id, state := range map[int64]string{1: "scheduled", 2: "due", 3: "completed", 4: "cancelled"} {
		c, err := s.LoadCommitment(ctx, id)
		if err != nil || c.State != state || c.ID != id {
			t.Fatalf("commitment %d=%+v %v", id, c, err)
		}
	}
	due, err := s.DueCommitments(ctx, memoryTestNow.Add(time.Hour))
	if err != nil || len(due) != 1 || due[0].ID != 1 {
		t.Fatalf("due=%+v %v", due, err)
	}
	occurrences, err := s.ListDueOccurrences(ctx)
	if err != nil || len(occurrences) != 1 || occurrences[0].RunID != "old_run" {
		t.Fatalf("occurrences=%+v %v", occurrences, err)
	}
	history, err := s.ReadMemory(ctx, "import_5")
	if err != nil || history.Record.State != "retracted" {
		t.Fatalf("released=%+v %v", history, err)
	}
	var jobs int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM memory_jobs WHERE operation='extract' AND object_id LIKE 'import:%'`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("import extraction would reactivate history: %d %v", jobs, err)
	}
	imported, err := s.ReadMemory(ctx, "import_7")
	if err != nil || imported.Record.Basis != "unverified" {
		t.Fatalf("unverified import=%+v %v", imported, err)
	}
	var remaining int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='memory_items'`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("old table=%d %v", remaining, err)
	}
	if err := s.migratePersonalMemory(); err != nil {
		t.Fatalf("reopen migration: %v", err)
	}
}

func TestMemoryMigrationRequiresDrainedRuns(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if _, err := s.db.Exec(legacyMemorySchema); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRun(ctx, core.RunCreateParams{RunID: "active", Input: "still running"}); err != nil {
		t.Fatal(err)
	}
	if err := s.migratePersonalMemory(); !errors.Is(err, core.ErrUnsupportedStorageSchema) {
		t.Fatalf("active migration: %v", err)
	}
	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='memory_items'`).Scan(&exists); err != nil || exists != 1 {
		t.Fatalf("migration failed to preserve table: %d %v", exists, err)
	}
}

func TestMemoryPreflightIsReadOnlyAndReportsDraining(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(fmt.Sprintf("active=%t", active), func(t *testing.T) {
			dir := t.TempDir()
			db, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.db.Exec(legacyMemorySchema); err != nil {
				t.Fatal(err)
			}
			stamp := formatTimestamp(memoryTestNow)
			if _, err := db.db.Exec(`INSERT INTO memory_items VALUES(1,'commitment','提醒','active','thread','',?,'','',?,?)`, stamp, stamp, stamp); err != nil {
				t.Fatal(err)
			}
			if active {
				if err := db.CreateRun(context.Background(), core.RunCreateParams{RunID: "still-running", Input: "work"}); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "acorn.db")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			report, err := PreflightMemoryMigration(context.Background(), dir)
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("read-only preflight changed database bytes")
			}
			if report.Ready == active || report.Schema != "working-memory" || report.LegacyEntries != 1 || report.LegacyCommitments["active"] != 1 || report.Integrity != "ok" {
				t.Fatalf("preflight %+v", report)
			}
		})
	}
}
