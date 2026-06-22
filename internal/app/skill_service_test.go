package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/skills"
)

func newTestSkillService(t *testing.T) (*SkillService, *skills.Loader, *config.Config) {
	t.Helper()
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Tools.Workspace.RootDir = root
	cfg.Tools.Mutation.RootDir = root
	cfg.Tools.RunCommand.WorkDir = root
	cfg.Runtime.StorageDir = filepath.Join(root, ".acorn")
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	loader := skills.NewLoader(cfg)
	svc := NewSkillService(cfg, loader)
	return svc, loader, cfg
}

func TestNewSkillService(t *testing.T) {
	svc, _, _ := newTestSkillService(t)
	if svc == nil {
		t.Fatal("service should not be nil")
	}
}

func TestSkillServiceListEmpty(t *testing.T) {
	svc, _, _ := newTestSkillService(t)
	items, err := svc.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 skills in empty workspace, got %d", len(items))
	}
}

func TestSkillServiceCreateAndGet(t *testing.T) {
	svc, _, _ := newTestSkillService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, CreateSkillInput{
		ID:          "skill.test",
		Name:        "Test Skill",
		Summary:     "A test skill",
		Instruction: "Do the test thing.",
		Tags:        []string{"test"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID != "skill.test" {
		t.Errorf("created ID = %q, want skill.test", created.ID)
	}
	if created.Name != "Test Skill" {
		t.Errorf("created Name = %q, want Test Skill", created.Name)
	}

	view, err := svc.Get(ctx, "skill.test")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if view.ID != "skill.test" {
		t.Errorf("get ID = %q, want skill.test", view.ID)
	}
}

func TestSkillServiceGetNotFound(t *testing.T) {
	svc, _, _ := newTestSkillService(t)
	_, err := svc.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent skill, got nil")
	}
}

func TestSkillServiceGetEmptyID(t *testing.T) {
	svc, _, _ := newTestSkillService(t)
	_, err := svc.Get(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty id, got nil")
	}
}

func TestSkillServiceGetWhitespaceID(t *testing.T) {
	svc, _, _ := newTestSkillService(t)
	_, err := svc.Get(context.Background(), "   ")
	if err == nil {
		t.Fatal("expected error for whitespace-only id, got nil")
	}
}

func TestSkillServiceCreateDuplicate(t *testing.T) {
	svc, _, _ := newTestSkillService(t)
	ctx := context.Background()

	input := CreateSkillInput{
		ID:          "skill.dup",
		Name:        "Dup",
		Instruction: "Do dup.",
	}
	if _, err := svc.Create(ctx, input); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := svc.Create(ctx, input); err == nil {
		t.Fatal("expected error for duplicate create, got nil")
	}
}

func TestSkillServiceListAfterCreate(t *testing.T) {
	svc, _, _ := newTestSkillService(t)
	ctx := context.Background()

	for _, id := range []string{"skill.a", "skill.b", "skill.c"} {
		if _, err := svc.Create(ctx, CreateSkillInput{
			ID:          id,
			Name:        id,
			Instruction: "Do " + id,
		}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}

	items, err := svc.List(ctx, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 skills, got %d", len(items))
	}
}

func TestSkillServiceListFilteredWithLimit(t *testing.T) {
	svc, _, cfg := newTestSkillService(t)
	ctx := context.Background()

	// Create workspace skills by writing SKILL.md files directly.
	for _, id := range []string{"skill.a", "skill.b", "skill.c"} {
		dir := filepath.Join(cfg.Tools.Workspace.RootDir, "skills", id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", id, err)
		}
		content := "---\nid: " + id + "\nname: " + id + "\nsummary: test\n---\n# " + id + "\nDo " + id + ".\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", id, err)
		}
	}

	items, total, err := svc.ListFiltered(ctx, SkillListFilter{Limit: 2})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(items) != 2 {
		t.Errorf("items = %d, want 2", len(items))
	}
}

func TestSkillServiceListFilteredOffsetBeyondTotal(t *testing.T) {
	svc, _, _ := newTestSkillService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, CreateSkillInput{
		ID:          "skill.offset",
		Name:        "Offset",
		Instruction: "Do offset.",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	items, total, err := svc.ListFiltered(ctx, SkillListFilter{Limit: 10, Offset: 100})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(items) != 0 {
		t.Errorf("items = %d, want 0 (offset beyond total)", len(items))
	}
}

func TestSkillServiceDelete(t *testing.T) {
	svc, _, _ := newTestSkillService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, CreateSkillInput{
		ID:          "skill.delete",
		Name:        "Delete",
		Instruction: "Do delete.",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.Delete(ctx, "skill.delete"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.Get(ctx, "skill.delete"); err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestSkillServiceDeleteNotFound(t *testing.T) {
	svc, _, _ := newTestSkillService(t)
	err := svc.Delete(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for deleting nonexistent skill, got nil")
	}
}

func TestSkillServicePatch(t *testing.T) {
	svc, _, _ := newTestSkillService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, CreateSkillInput{
		ID:          "skill.patch",
		Name:        "Patch",
		Instruction: "Original instruction.",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	patched, err := svc.Patch(ctx, "skill.patch", "Updated instruction.", "manual")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if patched.ID != "skill.patch" {
		t.Errorf("patched ID = %q", patched.ID)
	}
}

func TestSkillServiceReadFile(t *testing.T) {
	svc, _, _ := newTestSkillService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, CreateSkillInput{
		ID:          "skill.readfile",
		Name:        "ReadFile",
		Instruction: "Do readfile.",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	view, err := svc.ReadFile(ctx, "skill.readfile", "")
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if view.SkillID != "skill.readfile" {
		t.Errorf("SkillID = %q", view.SkillID)
	}
	if view.Path != "SKILL.md" {
		t.Errorf("Path = %q, want SKILL.md (default)", view.Path)
	}
	if view.Content == "" {
		t.Error("Content should not be empty")
	}
}

func TestSkillServiceSnapshotEmpty(t *testing.T) {
	svc, _, _ := newTestSkillService(t)
	snap, err := svc.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap == nil {
		t.Fatal("snapshot should not be nil")
	}
}

func TestSkillServiceHealthEmpty(t *testing.T) {
	svc, _, _ := newTestSkillService(t)
	report, err := svc.Health(context.Background())
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if report == nil {
		t.Fatal("report should not be nil")
	}
}

func TestTranslateSkillStoreErrorNil(t *testing.T) {
	if err := translateSkillStoreError(nil); err != nil {
		t.Errorf("expected nil for nil input, got %v", err)
	}
}

func TestTranslateSkillStoreErrorAlreadyExists(t *testing.T) {
	err := translateSkillStoreError(skills.ErrAlreadyExists)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestTranslateSkillStoreErrorNotFound(t *testing.T) {
	err := translateSkillStoreError(skills.ErrNotFound)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestTranslateSkillStoreErrorOther(t *testing.T) {
	otherErr := context.DeadlineExceeded
	result := translateSkillStoreError(otherErr)
	if result != otherErr {
		t.Errorf("expected original error for non-skill error, got %v", result)
	}
}
