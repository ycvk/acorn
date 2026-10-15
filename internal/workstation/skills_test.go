package workstation

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

func TestSkillPackagesAreWrittenReplacedAndDeleted(t *testing.T) {
	client, home := newTestClient(t)
	ctx := t.Context()
	files := []core.SkillFile{
		{Path: "scripts/run.sh", Content: []byte("#!/bin/sh\necho hi\n")},
		{Path: "references/guide.md", Content: []byte("# Guide\n")},
	}
	if err := client.PutSkillPackage(ctx, "pdf", "hash-1", "---\nname: pdf\n---\nUse it.\n", files); err != nil {
		t.Fatal(err)
	}
	dir, held, err := client.SkillPackages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(home, "skills") || held["pdf"] != "hash-1" || len(held) != 1 {
		t.Fatalf("dir = %s held = %v", dir, held)
	}
	root := filepath.Join(home, "skills", "pdf")
	if got, err := os.ReadFile(filepath.Join(root, "SKILL.md")); err != nil || !strings.Contains(string(got), "name: pdf") {
		t.Fatalf("SKILL.md = %q err = %v", got, err)
	}
	info, err := os.Stat(filepath.Join(root, "scripts", "run.sh"))
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("script info = %v err = %v", info, err)
	}
	if info, err := os.Stat(filepath.Join(root, "references", "guide.md")); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("reference info = %v err = %v", info, err)
	}

	if err := client.PutSkillPackage(ctx, "pdf", "hash-2", "---\nname: pdf\n---\nUse it again.\n", nil); err != nil {
		t.Fatal(err)
	}
	if _, held, err := client.SkillPackages(ctx); err != nil || held["pdf"] != "hash-2" {
		t.Fatalf("held = %v err = %v", held, err)
	}
	if _, err := os.Stat(filepath.Join(root, "scripts")); !os.IsNotExist(err) {
		t.Fatalf("replaced package kept the old scripts: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(home, "skills"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("skills dir entries = %v err = %v", entries, err)
	}

	if err := os.MkdirAll(filepath.Join(home, "skills", "mine"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteSkillPackage(ctx, "mine"); err == nil {
		t.Fatal("deleted a directory Acorn did not put there")
	}
	if err := client.DeleteSkillPackage(ctx, "pdf"); err != nil {
		t.Fatal(err)
	}
	if _, held, err := client.SkillPackages(ctx); err != nil || len(held) != 0 {
		t.Fatalf("held after delete = %v err = %v", held, err)
	}
}

func TestSkillPackagePutRejectsPathsOutsideThePackage(t *testing.T) {
	client, home := newTestClient(t)
	for _, p := range []string{"../escape.md", "/etc/passwd", "a/../../b", "SKILL.md", packageMarker, "a\\b"} {
		err := client.PutSkillPackage(t.Context(), "bad", "hash", "---\nname: bad\n---\nx\n", []core.SkillFile{{Path: p, Content: []byte("x")}})
		if err == nil || !strings.Contains(err.Error(), "invalid package file path") {
			t.Fatalf("path %q err = %v", p, err)
		}
	}
	if err := client.PutSkillPackage(t.Context(), "Bad_Name", "hash", "x", nil); err == nil || !strings.Contains(err.Error(), "invalid skill name") {
		t.Fatalf("bad name err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "escape.md")); !os.IsNotExist(err) {
		t.Fatalf("escape.md exists: %v", err)
	}
}

func TestReadPackageReadsDirectoriesAndZips(t *testing.T) {
	client, home := newTestClient(t)
	ctx := t.Context()
	pkg := filepath.Join(home, "downloads", "pdf")
	for p, content := range map[string]string{"SKILL.md": "skill", "scripts/fill.py": "print(1)"} {
		full := filepath.Join(pkg, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := client.ReadPackage(ctx, "downloads/pdf")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Path != "SKILL.md" || files[1].Path != "scripts/fill.py" || string(files[1].Content) != "print(1)" {
		t.Fatalf("files = %+v", files)
	}

	archive := filepath.Join(home, "downloads", "pdf.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for p, content := range map[string]string{"pdf/SKILL.md": "skill", "pdf/references/forms.md": "forms"} {
		entry, err := w.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	files, err = client.ReadPackage(ctx, archive)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Path != "pdf/SKILL.md" || string(files[1].Content) != "forms" {
		t.Fatalf("zip files = %+v", files)
	}

	if err := os.Symlink("/etc/passwd", filepath.Join(pkg, "passwd")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ReadPackage(ctx, pkg); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("symlink err = %v", err)
	}
	if _, err := client.ReadPackage(ctx, "downloads/missing"); err == nil {
		t.Fatal("read a missing package")
	}
}
