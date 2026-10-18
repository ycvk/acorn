package api

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/store"
)

type testSkillFixture struct {
	name        string
	description string
	instruction string
}

// skillTestEnv is a skill service over a real store, its library and the
// store itself, for writes the service does not make.
type skillTestEnv struct {
	db      *store.Store
	library *skills.Library
	service *SkillService
}

func newSkillTestEnv(t *testing.T) skillTestEnv {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	now := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	library, err := skills.NewLibrary(db, func() time.Time { return now }, nil)
	if err != nil {
		t.Fatal(err)
	}
	status, err := skills.NewStatus(library, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Runtime.StorageDir = t.TempDir()
	cfg.Workstation.URL = "http://workstation.test:8780"
	service, err := NewSkillService(cfg, library, status)
	if err != nil {
		t.Fatal(err)
	}
	return skillTestEnv{db: db, library: library, service: service}
}

// importSkill installs a package as the owner does with acorn skills import.
func (e skillTestEnv) importSkill(t *testing.T, files ...core.SkillFile) core.Skill {
	t.Helper()
	skill, err := e.library.Import(t.Context(), files, false)
	if err != nil {
		t.Fatal(err)
	}
	return skill
}

func skillMarkdown(name, description, instruction string) string {
	return fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s\n", name, description, instruction)
}

func newTestSkillService(t *testing.T, fixtures ...testSkillFixture) *SkillService {
	t.Helper()
	env := newSkillTestEnv(t)
	for _, fixture := range fixtures {
		instruction := strings.TrimSpace(fixture.instruction)
		if instruction == "" {
			instruction = "Use repo inspection."
		}
		env.importSkill(t, core.SkillFile{Path: skills.SkillFileName, Content: []byte(skillMarkdown(fixture.name, fixture.description, instruction))})
	}
	return env.service
}
