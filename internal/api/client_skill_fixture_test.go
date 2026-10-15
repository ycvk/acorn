package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
)

type testSkillFixture struct {
	name        string
	description string
	instruction string
}

type staticSkillLibrary struct{ scan skills.ScanResult }

func (s staticSkillLibrary) Scan(context.Context) (*skills.ScanResult, error) {
	scan := s.scan
	return &scan, nil
}

func (s staticSkillLibrary) Import(context.Context, []core.SkillFile, bool) (core.Skill, error) {
	return core.Skill{}, errors.New("static skill library does not import")
}

func newTestSkillService(t *testing.T, fixtures ...testSkillFixture) *SkillService {
	t.Helper()

	cfg := config.DefaultConfig()
	cfg.Runtime.StorageDir = t.TempDir()

	var scan skills.ScanResult
	for i, fixture := range fixtures {
		name := strings.TrimSpace(fixture.name)
		if name == "" {
			t.Fatal("test skill name is required")
		}
		instruction := strings.TrimSpace(fixture.instruction)
		if instruction == "" {
			instruction = "Use repo inspection."
		}
		markdown := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s\n", name, strings.TrimSpace(fixture.description), instruction)
		spec, err := skills.Parse(markdown)
		if err != nil {
			t.Fatalf("parse test skill %s: %v", name, err)
		}
		spec.Revision, spec.Markdown = int64(i+1), markdown
		scan.Skills = append(scan.Skills, spec)
	}

	return NewSkillService(cfg, staticSkillLibrary{scan: scan})
}
