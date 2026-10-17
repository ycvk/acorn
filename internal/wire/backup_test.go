package wire

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/store"
)

func backupTestConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Runtime.StorageDir = t.TempDir()
	return cfg
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBackupWhileRunningAndRestoreIntoEmptyStorage(t *testing.T) {
	ctx := context.Background()
	source := backupTestConfig(t)
	running, err := store.Open(source.Runtime.StorageDir)
	if err != nil {
		t.Fatal(err)
	}
	defer running.Close()
	if _, err := running.CreateSession(ctx, "thread_1", "周末计划"); err != nil {
		t.Fatal(err)
	}
	if _, err := running.WritePersona(ctx, core.PersonaWrite{Content: "你是 Acorn。", Author: core.PersonaAuthorOwner, At: time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if _, err := running.WriteSkill(ctx, core.SkillWrite{ID: "github", Markdown: "md", Author: core.SkillAuthorOwner, At: time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if err := running.SetSkillSecret(ctx, "github", "GITHUB_TOKEN", "ghp_backup", time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	attachment := filepath.Join("attachments", "2026", "10", "0123456789abcdef.png")
	writeTestFile(t, filepath.Join(source.Runtime.StorageDir, attachment), "png bytes")
	writeTestFile(t, filepath.Join(source.Runtime.StorageDir, "artifacts", "run_1", "report.md"), "report")

	dir := t.TempDir()
	source.Backup.Dir, source.Backup.Keep = dir, 1
	if latest, err := LatestSnapshot(dir); err != nil || !latest.IsZero() {
		t.Fatalf("latest snapshot before any backup = %v, %v", latest, err)
	}
	first, err := Backup(ctx, source, time.Date(2026, 10, 11, 4, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if first.Snapshot != filepath.Join(dir, "snapshots", "20261011T043000Z") || first.FilesCopied != 2 || first.DatabaseBytes == 0 {
		t.Fatalf("first backup = %+v", first)
	}
	if _, err := running.CreateSession(ctx, "thread_2", "之后的对话"); err != nil {
		t.Fatal(err)
	}
	second, err := Backup(ctx, source, time.Date(2026, 10, 12, 4, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if second.FilesCopied != 0 || len(second.Removed) != 1 || second.Removed[0] != first.Snapshot {
		t.Fatalf("second backup = %+v", second)
	}
	if latest, err := LatestSnapshot(dir); err != nil || !latest.Equal(time.Date(2026, 10, 12, 4, 30, 0, 0, time.UTC)) {
		t.Fatalf("latest snapshot = %v, %v", latest, err)
	}

	target := backupTestConfig(t)
	restored, err := Restore(ctx, target, second.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if restored.FilesCopied != 2 {
		t.Fatalf("restore = %+v", restored)
	}
	db, err := store.Open(target.Runtime.StorageDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, id := range []string{"thread_1", "thread_2"} {
		if _, err := db.LoadSession(ctx, id); err != nil {
			t.Fatalf("restored %s: %v", id, err)
		}
	}
	if persona, err := db.Persona(ctx); err != nil || persona.Content != "你是 Acorn。" {
		t.Fatalf("restored persona %+v: %v", persona, err)
	}
	if secrets, err := db.SkillSecrets(ctx, "github"); err != nil || secrets["GITHUB_TOKEN"] != "ghp_backup" {
		t.Fatalf("restored secrets %v: %v", secrets, err)
	}
	if data, err := os.ReadFile(filepath.Join(target.Runtime.StorageDir, attachment)); err != nil || string(data) != "png bytes" {
		t.Fatalf("restored attachment %q: %v", data, err)
	}
}

func TestRestoreRefusesARunningAcornAndForeignDirectories(t *testing.T) {
	ctx := context.Background()
	cfg := backupTestConfig(t)
	db, err := store.Open(cfg.Runtime.StorageDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg.Backup.Dir = t.TempDir()
	report, err := Backup(ctx, cfg, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(ctx, cfg, report.Snapshot); err == nil || !strings.Contains(err.Error(), "restore needs Acorn stopped") {
		t.Fatalf("restore over an open store = %v", err)
	}
	if _, err := Restore(ctx, cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "not a snapshot directory") {
		t.Fatalf("restore from a foreign directory = %v", err)
	}
}
