package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/config"
)

func TestLoadSkillOriginDefaultsToHuman(t *testing.T) {
	dir := t.TempDir()
	writeTestSkill(t, dir, `---
id: skill.human
name: Human Skill
summary: Existing skill without origin
---

# Human Skill

Do the thing.
`)

	spec, problem := loadSkillDir(dir, WorkspaceScope)
	if problem != nil {
		t.Fatalf("loadSkillDir problem = %v", problem.Error)
	}
	if spec.Source != WorkspaceScope {
		t.Fatalf("source = %q, want %q", spec.Source, WorkspaceScope)
	}
	if spec.Origin != OriginHuman {
		t.Fatalf("origin = %q, want %q", spec.Origin, OriginHuman)
	}
	if spec.TaskPattern != "" {
		t.Fatalf("task_pattern = %q, want empty", spec.TaskPattern)
	}
}

func TestLoadSkillRejectsInvalidOrigin(t *testing.T) {
	dir := t.TempDir()
	writeTestSkill(t, dir, `---
id: skill.bad
name: Bad Skill
origin: mystery
---

# Bad Skill

Do the thing.
`)

	_, problem := loadSkillDir(dir, WorkspaceScope)
	if problem == nil {
		t.Fatal("expected skill problem")
	}
	if !strings.Contains(problem.Error, `origin "mystery" is invalid`) {
		t.Fatalf("problem = %q, want invalid origin", problem.Error)
	}
}

func TestLoadSkillRejectsDistilledWithoutTaskPattern(t *testing.T) {
	dir := t.TempDir()
	writeTestSkill(t, dir, `---
id: skill.distilled
name: Distilled Skill
origin: distilled
---

# Distilled Skill

Do the thing.
`)

	_, problem := loadSkillDir(dir, WorkspaceScope)
	if problem == nil {
		t.Fatal("expected skill problem")
	}
	if !strings.Contains(problem.Error, "task_pattern is required for distilled origin") {
		t.Fatalf("problem = %q, want missing task_pattern", problem.Error)
	}
}

func TestScanSkillsLoadsBuiltinAndWorkspaceSources(t *testing.T) {
	root := t.TempDir()
	writeTestSkillDir(t, filepath.Join(root, "skills", "skill.inspect"), `---
id: skill.inspect
name: Inspect
---

# Inspect

Inspect the repo.
`)
	writeTestSkillDir(t, filepath.Join(root, ".acorn", "skills", "workspace", "skill.workspace"), `---
id: skill.workspace
name: Workspace
---

# Workspace

Use workspace instructions.
`)
	loader := newTestLoader(t, &config.Config{
		Runtime: config.RuntimeConfig{StorageDir: filepath.Join(root, ".acorn")},
		Tools:   config.ToolsConfig{Workspace: config.WorkspaceToolConfig{RootDir: root}},
	})
	scan, err := loader.ScanSkills(context.Background())
	if err != nil {
		t.Fatalf("ScanSkills: %v", err)
	}
	byID := map[string]Spec{}
	for _, item := range scan.Skills {
		byID[item.ID] = item
	}
	if byID["skill.inspect"].Source != BuiltinScope {
		t.Fatalf("builtin source = %q", byID["skill.inspect"].Source)
	}
	if byID["skill.workspace"].Source != WorkspaceScope {
		t.Fatalf("workspace source = %q", byID["skill.workspace"].Source)
	}
}

func TestWorkspaceSkillCannotShadowBuiltin(t *testing.T) {
	root := t.TempDir()
	writeTestSkillDir(t, filepath.Join(root, "skills", "skill.inspect"), `---
id: skill.inspect
name: Builtin Inspect
---

# Builtin Inspect

Builtin instructions.
`)
	writeTestSkillDir(t, filepath.Join(root, ".acorn", "skills", "workspace", "skill.inspect"), `---
id: skill.inspect
name: Workspace Inspect
---

# Workspace Inspect

Workspace instructions.
`)
	loader := newTestLoader(t, &config.Config{
		Runtime: config.RuntimeConfig{StorageDir: filepath.Join(root, ".acorn")},
		Tools:   config.ToolsConfig{Workspace: config.WorkspaceToolConfig{RootDir: root}},
	})
	scan, err := loader.ScanSkills(context.Background())
	if err != nil {
		t.Fatalf("ScanSkills: %v", err)
	}
	if len(scan.Skills) != 1 || scan.Skills[0].Name != "Builtin Inspect" {
		t.Fatalf("skills = %#v, want only builtin", scan.Skills)
	}
	if len(scan.Problems) != 1 || !strings.Contains(scan.Problems[0].Error, "cannot shadow builtin") {
		t.Fatalf("problems = %#v, want builtin shadow problem", scan.Problems)
	}
}

func writeTestSkill(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write skill markdown: %v", err)
	}
}

func newTestLoader(t *testing.T, cfg *config.Config) *Loader {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return NewLoader(cfg)
}

func writeTestSkillDir(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	writeTestSkill(t, dir, body)
}
