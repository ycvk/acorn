package api

import (
	"context"
	"fmt"
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

type staticSkillScanner struct{ scan skills.ScanResult }

func (s staticSkillScanner) Scan(context.Context) (*skills.ScanResult, error) {
	scan := s.scan
	return &scan, nil
}

func newTestSkillService(t *testing.T, fixtures ...testSkillFixture) *SkillService {
	t.Helper()

	cfg := config.DefaultConfig()
	cfg.Runtime.StorageDir = t.TempDir()

	var scan skills.ScanResult
	for i, fixture := range fixtures {
		id := strings.TrimSpace(fixture.id)
		if id == "" {
			t.Fatal("test skill id is required")
		}
		name := strings.TrimSpace(fixture.name)
		if name == "" {
			t.Fatalf("test skill %s name is required", id)
		}
		instruction := strings.TrimSpace(fixture.instruction)
		if instruction == "" {
			instruction = "Use repo inspection."
		}
		markdown := fmt.Sprintf("---\nid: %s\nname: %s\nsummary: %s\n---\n\n# %s\n\n%s\n", id, name, strings.TrimSpace(fixture.summary), name, instruction)
		spec, err := skills.Parse(markdown)
		if err != nil {
			t.Fatalf("parse test skill %s: %v", id, err)
		}
		spec.Revision, spec.Markdown = int64(i+1), markdown
		scan.Skills = append(scan.Skills, spec)
	}

	return NewSkillService(cfg, staticSkillScanner{scan: scan})
}
