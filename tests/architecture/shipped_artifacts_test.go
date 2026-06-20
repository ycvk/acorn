package architecture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/config"
)

// T-001 RED: shipped config / doc artifacts must reflect current truth, not
// stale scaffold. These fail today and are made green by T-002.

func TestExampleConfigHasNoPhase0Scaffold(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "configs", "acorn.example.yaml"))
	if err != nil {
		t.Fatalf("read example config: %v", err)
	}
	if strings.Contains(string(data), "Phase 0") {
		t.Fatalf("configs/acorn.example.yaml must not carry stale 'Phase 0' scaffold prompt")
	}
}

func TestMinimalConfigExists(t *testing.T) {
	info, err := os.Stat(filepath.Join("..", "..", "configs", "acorn.minimal.yaml"))
	if err != nil {
		t.Fatalf("configs/acorn.minimal.yaml must exist (provider+runtime+context+agent.name only): %v", err)
	}
	if info.IsDir() {
		t.Fatalf("configs/acorn.minimal.yaml must be a file, not a directory")
	}
}

func TestNoDuplicateAppDecisionMd(t *testing.T) {
	if _, err := os.Stat(filepath.Join("..", "..", "internal", "app", "decision.md")); err == nil {
		t.Fatalf("internal/app/decision.md must not exist — it duplicates the root decision.md and is dead config")
	}
}

func TestMinimalConfigLoads(t *testing.T) {
	cfg, err := config.Load(filepath.Join("..", "..", "configs", "acorn.minimal.yaml"))
	if err != nil {
		t.Fatalf("configs/acorn.minimal.yaml must load via config.Load (defaults merge): %v", err)
	}
	if strings.TrimSpace(cfg.Agent.Name) == "" {
		t.Fatalf("minimal config must set agent.name")
	}
	if len(cfg.Providers) == 0 || !cfg.Providers[0].Enabled {
		t.Fatalf("minimal config must enable at least one provider")
	}
}
