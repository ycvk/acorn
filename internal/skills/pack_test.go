package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/config"
)

func TestPlanSkillPackReportsCreateUpdateNoop(t *testing.T) {
	root := t.TempDir()
	loader := newTestLoader(t, &config.Config{
		Runtime: config.RuntimeConfig{StorageDir: filepath.Join(root, ".acorn")},
		Tools:   config.ToolsConfig{Workspace: config.WorkspaceToolConfig{RootDir: root}},
	})
	manifest := testPackManifest("v1", "First body")
	receipt, err := loader.ApplySkillPack(context.Background(), manifest, PackTargetGenerated, PackApplyOptions{
		Destructive: true,
		Now:         time.Date(2026, 5, 15, 1, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ApplySkillPack: %v", err)
	}
	if len(receipt.Files) == 0 {
		t.Fatal("receipt has no files")
	}
	updated := testPackManifest("v2", "Updated body")
	updated.Skills = append(updated.Skills, PackSkill{
		ID: "skill.extra",
		Files: map[string]string{
			"SKILL.md": testPackSkillMarkdown("skill.extra", "Extra", "Extra summary.", "extra"),
		},
	})
	plan, err := loader.PlanSkillPack(context.Background(), updated, PackTargetGenerated)
	if err != nil {
		t.Fatalf("PlanSkillPack: %v", err)
	}
	if !hasPackAction(plan.Actions, PackActionUpdate, "skill.pack.test/SKILL.md") {
		t.Fatalf("actions = %#v, want update", plan.Actions)
	}
	if !hasPackAction(plan.Actions, PackActionCreate, "skill.extra/SKILL.md") {
		t.Fatalf("actions = %#v, want create", plan.Actions)
	}
	noopPlan, err := loader.PlanSkillPack(context.Background(), manifest, PackTargetGenerated)
	if err != nil {
		t.Fatalf("PlanSkillPack noop: %v", err)
	}
	if !hasPackAction(noopPlan.Actions, PackActionNoop, "skill.pack.test/SKILL.md") {
		t.Fatalf("actions = %#v, want noop", noopPlan.Actions)
	}
}

func TestPlanSkillPackFailsDependencyClosure(t *testing.T) {
	root := t.TempDir()
	loader := newTestLoader(t, &config.Config{
		Runtime: config.RuntimeConfig{StorageDir: filepath.Join(root, ".acorn")},
		Tools:   config.ToolsConfig{Workspace: config.WorkspaceToolConfig{RootDir: root}},
	})
	manifest := testPackManifest("v1", "First body")
	manifest.Dependencies = []string{"skill.missing"}
	_, err := loader.PlanSkillPack(context.Background(), manifest, PackTargetGenerated)
	if err == nil || !strings.Contains(err.Error(), `dependency "skill.missing" is not available`) {
		t.Fatalf("PlanSkillPack error = %v, want dependency closure error", err)
	}
}

func TestApplySkillPackRejectsUnmanagedOverwrite(t *testing.T) {
	root := t.TempDir()
	loader := newTestLoader(t, &config.Config{
		Runtime: config.RuntimeConfig{StorageDir: filepath.Join(root, ".acorn")},
		Tools:   config.ToolsConfig{Workspace: config.WorkspaceToolConfig{RootDir: root}},
	})
	target := filepath.Join(root, ".acorn", "skills", "generated", "skill.pack.test", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(target, []byte(testPackSkillMarkdown("skill.pack.test", "Pack Test", "Manual.", "manual")), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := loader.ApplySkillPack(context.Background(), testPackManifest("v1", "First body"), PackTargetGenerated, PackApplyOptions{})
	if err == nil || !strings.Contains(err.Error(), "already exists without managed receipt") {
		t.Fatalf("ApplySkillPack error = %v, want unmanaged overwrite error", err)
	}
}

func TestApplySkillPackWritesReceiptAndRescannableSkill(t *testing.T) {
	root := t.TempDir()
	loader := newTestLoader(t, &config.Config{
		Runtime: config.RuntimeConfig{StorageDir: filepath.Join(root, ".acorn")},
		Tools:   config.ToolsConfig{Workspace: config.WorkspaceToolConfig{RootDir: root}},
	})
	receipt, err := loader.ApplySkillPack(context.Background(), testPackManifest("v1", "First body"), PackTargetGenerated, PackApplyOptions{
		Now: time.Date(2026, 5, 15, 2, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ApplySkillPack: %v", err)
	}
	if receipt.PackID != "test-pack" || receipt.Target != GeneratedScope || len(receipt.Files) != 1 {
		t.Fatalf("receipt = %#v", receipt)
	}
	if receipt.Files[0].SHA256 == "" || !receipt.Files[0].Managed {
		t.Fatalf("receipt files = %#v", receipt.Files)
	}
	body, err := os.ReadFile(filepath.Join(root, ".acorn", "skills", "generated", ".acorn-pack-receipts", "test-pack.json"))
	if err != nil {
		t.Fatalf("read receipt: %v", err)
	}
	if !strings.Contains(string(body), `"pack_id": "test-pack"`) {
		t.Fatalf("receipt body = %s", body)
	}
	scan, err := loader.ScanSkills(context.Background())
	if err != nil {
		t.Fatalf("ScanSkills: %v", err)
	}
	found := false
	for _, skill := range scan.Skills {
		if skill.ID == "skill.pack.test" {
			found = true
		}
	}
	if !found {
		t.Fatalf("installed skill not found in scan: %#v", scan.Skills)
	}
}

func TestPlanSkillPackRejectsBuiltinTarget(t *testing.T) {
	loader := &Loader{builtinDir: t.TempDir()}
	_, err := loader.PlanSkillPack(context.Background(), testPackManifest("v1", "First body"), PackTargetSource(BuiltinScope))
	if err == nil || !strings.Contains(err.Error(), "not mutable") {
		t.Fatalf("PlanSkillPack error = %v, want immutable target error", err)
	}
}

func testPackManifest(version string, body string) PackManifest {
	return PackManifest{
		PackID:  "test-pack",
		Version: version,
		Skills: []PackSkill{{
			ID: "skill.pack.test",
			Files: map[string]string{
				"SKILL.md": testPackSkillMarkdown("skill.pack.test", "Pack Test", "Pack summary.", body),
			},
		}},
	}
}

func testPackSkillMarkdown(id string, name string, summary string, body string) string {
	return `---
id: ` + id + `
name: ` + name + `
summary: ` + summary + `
---

# ` + name + `

` + body + `
`
}

func hasPackAction(actions []PackFileAction, action PackAction, path string) bool {
	for _, item := range actions {
		if item.Action == action && item.Path == path {
			return true
		}
	}
	return false
}
