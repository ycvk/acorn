package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/skills"
)

type testSkillFixture struct {
	id          string
	name        string
	summary     string
	instruction string
}

func newTestSkillService(t *testing.T, fixtures ...testSkillFixture) *SkillService {
	t.Helper()

	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Skills.Dir = root
	cfg.Runtime.StorageDir = t.TempDir()

	for _, fixture := range fixtures {
		id := strings.TrimSpace(fixture.id)
		if id == "" {
			t.Fatal("test skill id is required")
		}
		name := strings.TrimSpace(fixture.name)
		if name == "" {
			t.Fatalf("test skill %s name is required", id)
		}
		summary := strings.TrimSpace(fixture.summary)
		instruction := strings.TrimSpace(fixture.instruction)
		if instruction == "" {
			instruction = "Use repo inspection."
		}

		dir := filepath.Join(root, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir test skill dir: %v", err)
		}

		body := fmt.Sprintf(`---
id: %s
name: %s
summary: %s
---

# %s

%s
`, id, name, summary, name, instruction)
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatalf("write test skill markdown: %v", err)
		}
	}

	return NewSkillService(cfg, skills.NewLoader(cfg.Skills.Dir))
}
