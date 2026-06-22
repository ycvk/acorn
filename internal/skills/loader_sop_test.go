package skills

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"

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

func TestCreateSkillPreservesDistilledOriginAndTaskPattern(t *testing.T) {
	root := t.TempDir()
	loader := newTestLoader(t, &config.Config{
		Runtime: config.RuntimeConfig{
			StorageDir: filepath.Join(root, ".acorn"),
		},
		Tools: config.ToolsConfig{
			Workspace: config.WorkspaceToolConfig{RootDir: root},
		},
	})

	spec, err := loader.CreateSkill(context.Background(), CreateInput{
		ID:           "sop.sqlite-rows-error-handling",
		Name:         "SQLite Rows Error Handling SOP",
		Summary:      "Apply the verified loop cleanup pattern for SQLite queries.",
		Origin:       OriginDistilled,
		TaskPattern:  "fix sqlite query loop error handling",
		PromotedFrom: "run_123",
		Instruction:  "1. Locate the query loop.\n2. Check rows.Err after iteration.",
		TriggerHints: []string{"rows.Err sqlite"},
	})
	if err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}
	if spec.Source != GeneratedScope {
		t.Fatalf("source = %q, want %q", spec.Source, GeneratedScope)
	}
	if spec.Origin != OriginDistilled {
		t.Fatalf("origin = %q, want %q", spec.Origin, OriginDistilled)
	}
	if spec.TaskPattern != "fix sqlite query loop error handling" {
		t.Fatalf("task_pattern = %q", spec.TaskPattern)
	}

	body, err := os.ReadFile(filepath.Join(root, ".acorn", "skills", "generated", "sop.sqlite-rows-error-handling", "SKILL.md"))
	if err != nil {
		t.Fatalf("read skill markdown: %v", err)
	}
	text := string(body)
	for _, want := range []string{
		"origin: distilled",
		"task_pattern: fix sqlite query loop error handling",
		"promoted_from: run_123",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("skill markdown missing %q:\n%s", want, text)
		}
	}
}

func TestScanSkillsLoadsBuiltinAndGeneratedSources(t *testing.T) {
	root := t.TempDir()
	writeTestSkillDir(t, filepath.Join(root, "skills", "skill.inspect"), `---
id: skill.inspect
name: Inspect
---

# Inspect

Inspect the repo.
`)
	writeTestSkillDir(t, filepath.Join(root, ".acorn", "skills", "generated", "skill.generated"), `---
id: skill.generated
name: Generated
---

# Generated

Use generated instructions.
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
	if byID["skill.generated"].Source != GeneratedScope {
		t.Fatalf("generated source = %q", byID["skill.generated"].Source)
	}
}

func TestGeneratedSkillCannotShadowBuiltin(t *testing.T) {
	root := t.TempDir()
	writeTestSkillDir(t, filepath.Join(root, "skills", "skill.inspect"), `---
id: skill.inspect
name: Builtin Inspect
---

# Builtin Inspect

Builtin instructions.
`)
	writeTestSkillDir(t, filepath.Join(root, ".acorn", "skills", "generated", "skill.inspect"), `---
id: skill.inspect
name: Generated Inspect
---

# Generated Inspect

Generated instructions.
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

func TestBuiltinSkillIsReadOnly(t *testing.T) {
	root := t.TempDir()
	writeTestSkillDir(t, filepath.Join(root, "skills", "skill.inspect"), `---
id: skill.inspect
name: Builtin Inspect
---

# Builtin Inspect

Builtin instructions.
`)
	loader := newTestLoader(t, &config.Config{
		Runtime: config.RuntimeConfig{StorageDir: filepath.Join(root, ".acorn")},
		Tools:   config.ToolsConfig{Workspace: config.WorkspaceToolConfig{RootDir: root}},
	})
	err := loader.WriteSkillFile(context.Background(), "skill.inspect", "SKILL.md", "changed")
	if err == nil {
		t.Fatal("expected builtin write to fail")
	}
	if !strings.Contains(err.Error(), "source=builtin") {
		t.Fatalf("error = %v, want source=builtin", err)
	}
}

func TestSkillCreateToolWritesGeneratedPackage(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Tools.Workspace.RootDir = root
	cfg.Runtime.StorageDir = filepath.Join(root, ".acorn")
	loader := newTestLoader(t, cfg)
	tools, err := BuildAgentTools(loader)
	if err != nil {
		t.Fatalf("BuildAgentTools: %v", err)
	}
	createTool := mustInvokableSkillTool(t, tools, "skill_create")
	body, err := json.Marshal(ToolCreateInput{
		ID:           "skill.generated",
		Name:         "Generated",
		Summary:      "Generated skill",
		Instruction:  "Use generated workflow.",
		TriggerHints: []string{"generated workflow"},
		Files:        map[string]string{"references/notes.md": "Reference note.\n"},
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	out, err := createTool.InvokableRun(context.Background(), string(body))
	if err != nil {
		t.Fatalf("skill_create: %v", err)
	}
	var spec Spec
	if err := json.Unmarshal([]byte(out), &spec); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if spec.ID != "skill.generated" || spec.Source != GeneratedScope {
		t.Fatalf("spec = %#v", spec)
	}
	if !reflect.DeepEqual(spec.Files, []string{"SKILL.md", "references/notes.md"}) {
		t.Fatalf("files = %#v", spec.Files)
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

func mustInvokableSkillTool(t *testing.T, tools []einotool.BaseTool, name string) einotool.InvokableTool {
	t.Helper()
	for _, item := range tools {
		info, err := item.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info: %v", err)
		}
		if info.Name != name {
			continue
		}
		invokable, ok := item.(einotool.InvokableTool)
		if !ok {
			t.Fatalf("%s is not invokable", name)
		}
		return invokable
	}
	t.Fatalf("missing tool %s", name)
	return nil
}

func writeTestSkillDir(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	writeTestSkill(t, dir, body)
}
