package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/config"
)

// TestInitTemplateIsValidAndExecutionReady guards the most important property of
// `acorn init`: the embedded starter loads and becomes execution-ready after the model and Voyage keys are supplied.
func TestInitTemplateIsValidAndExecutionReady(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "voyage-test")
	t.Setenv("OPENAI_API_KEY", "sk-test-init")
	dir := t.TempDir()
	path := filepath.Join(dir, "acorn.yaml")
	if err := os.WriteFile(path, []byte(initConfigTemplate), 0o600); err != nil {
		t.Fatalf("write template: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("init template must load: %v", err)
	}
	if err := cfg.ValidateExecutionReady(); err != nil {
		t.Fatalf("init template must be execution-ready with model and Voyage keys: %v", err)
	}
}

// The release installer writes its config through `acorn init --storage-dir`.
func TestInitStorageDirPlacesInstallerLayout(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "voyage-test")
	t.Setenv("OPENAI_API_KEY", "sk-test-init")
	t.Setenv("ACORN_WORKSTATION_TOKEN", strings.Repeat("w", 32))
	storage := filepath.Join(t.TempDir(), "srv", "acorn workspace")
	backups := filepath.Join(t.TempDir(), "srv", "acorn backups")
	path := filepath.Join(t.TempDir(), "acorn.yaml")
	if err := runInit(t.Context(), []string{"-c", path, "--storage-dir", storage, "--backup-dir", backups, "--workstation-url", "http://127.0.0.1:7810"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("installer config must load: %v", err)
	}
	if err := cfg.ValidateExecutionReady(); err != nil {
		t.Fatalf("installer config must execute with model and Voyage keys: %v", err)
	}
	if cfg.Runtime.StorageDir != storage || cfg.Backup.Dir != backups || cfg.Backup.Keep != 14 {
		t.Fatalf("storage=%q backup=%+v, want storage %q and backups %q", cfg.Runtime.StorageDir, cfg.Backup, storage, backups)
	}
	if cfg.Workstation.URL != "http://127.0.0.1:7810" || cfg.Workstation.Token != strings.Repeat("w", 32) {
		t.Fatalf("workstation = %+v", cfg.Workstation)
	}
	if err := runInit(t.Context(), []string{"--print", "--storage-dir", "relative"}); err == nil {
		t.Fatal("relative storage dir accepted")
	}
	if err := runInit(t.Context(), []string{"--print", "--backup-dir", "relative"}); err == nil {
		t.Fatal("relative backup dir accepted")
	}
}

func TestInitWritesConfigAndRefusesClobber(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	path := filepath.Join(dir, "nested", "acorn.yaml")

	if err := runInit(t.Context(), []string{"-c", path}); err != nil {
		t.Fatalf("init: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written config: %v", err)
	}
	if !strings.Contains(string(body), "Acorn self-hosted starter config") {
		t.Fatalf("written config missing starter header:\n%s", string(body))
	}

	// Second init without --force must refuse rather than clobber.
	err = runInit(t.Context(), []string{"-c", path})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second init should refuse clobber, got: %v", err)
	}

	// --force overwrites.
	if err := runInit(t.Context(), []string{"-c", path, "--force"}); err != nil {
		t.Fatalf("init --force: %v", err)
	}
}

func TestInitPrintDoesNotWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "acorn.yaml")
	if err := runInit(t.Context(), []string{"-c", path, "--print"}); err != nil {
		t.Fatalf("init --print: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("--print must not write the config file, stat err = %v", err)
	}
}
