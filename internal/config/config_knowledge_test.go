package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKnowledgeDir(t *testing.T) {
	cfg := defaultConfig()
	cfg.Runtime.StorageDir = "/srv/acorn"
	if got := cfg.KnowledgeDir(); got != "/srv/acorn/knowledge" {
		t.Fatalf("default KnowledgeDir = %q", got)
	}

	dir := t.TempDir()
	configPath := filepath.Join(dir, "acorn.yaml")
	if err := os.WriteFile(configPath, []byte("knowledge:\n  dir: vault\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := loaded.KnowledgeDir(); got != filepath.Join(dir, "vault") {
		t.Fatalf("relative knowledge.dir = %q", got)
	}
}

func TestValidateBaseRejectsKnowledgeDirThatIsAFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig()
	cfg.Knowledge.Dir = file
	if err := cfg.ValidateBase(); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("ValidateBase() = %v", err)
	}
}
