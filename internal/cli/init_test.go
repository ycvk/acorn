package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/config"
)

// TestInitTemplateIsValidAndExecutionReady guards the most important property of
// `acorn init`: the embedded starter must load and be execution-ready with only
// OPENAI_API_KEY set, and semantic recall must be OFF by default (optional).
func TestInitTemplateIsValidAndExecutionReady(t *testing.T) {
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
		t.Fatalf("init template must be execution-ready with OPENAI_API_KEY set: %v", err)
	}
	if cfg.MemorySemanticConfigured() {
		t.Fatal("init template must have semantic OFF by default (model/base_url commented)")
	}
}

func TestInitWritesConfigAndRefusesClobber(t *testing.T) {
	dir := t.TempDir()
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
