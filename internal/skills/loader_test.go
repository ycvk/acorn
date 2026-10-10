package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSkillRejectsUnknownFrontmatterField(t *testing.T) {
	dir := t.TempDir()
	writeTestSkill(t, dir, `---
id: skill.old
name: Old Skill
origin: distilled
---

# Old Skill

Do the thing.
`)

	_, problem := loadSkillDir(dir)
	if problem == nil || !strings.Contains(problem.Error, "field origin not found") {
		t.Fatalf("problem = %#v, want unknown field origin", problem)
	}
}

func TestScanSkillsLoadsPackagesFromDirectory(t *testing.T) {
	root := t.TempDir()
	writeTestSkillDir(t, filepath.Join(root, "inspect"), `---
id: skill.inspect
name: Inspect
---

# Inspect

Inspect the repo.
`)
	writeTestSkillDir(t, filepath.Join(root, "inspect-copy"), `---
id: skill.inspect
name: Inspect Copy
---

# Inspect Copy

Second package with the same id.
`)
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	scan, err := NewLoader(root).ScanSkills(context.Background())
	if err != nil {
		t.Fatalf("ScanSkills: %v", err)
	}
	if len(scan.Skills) != 1 || scan.Skills[0].Name != "Inspect" || scan.Skills[0].Path != filepath.Join(root, "inspect") {
		t.Fatalf("skills = %#v, want the first inspect package", scan.Skills)
	}
	if len(scan.Problems) != 1 || !strings.Contains(scan.Problems[0].Error, `duplicate skill id "skill.inspect"`) {
		t.Fatalf("problems = %#v, want duplicate id", scan.Problems)
	}
}

func TestScanSkillsFailsWithoutDirectory(t *testing.T) {
	_, err := NewLoader(filepath.Join(t.TempDir(), "missing")).ScanSkills(context.Background())
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want missing directory", err)
	}
}

func writeTestSkill(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write skill markdown: %v", err)
	}
}

func writeTestSkillDir(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	writeTestSkill(t, dir, body)
}
