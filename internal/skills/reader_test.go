package skills

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeSkillRelativePathDefaultsToSkillMD(t *testing.T) {
	got, err := normalizeSkillRelativePath("  ")
	if err != nil {
		t.Fatalf("normalizeSkillRelativePath: %v", err)
	}
	if got != "SKILL.md" {
		t.Errorf("got = %q, want SKILL.md (default)", got)
	}
}

func TestNormalizeSkillRelativePathValid(t *testing.T) {
	got, err := normalizeSkillRelativePath("docs/guide.md")
	if err != nil {
		t.Fatalf("normalizeSkillRelativePath: %v", err)
	}
	if got != filepath.Join("docs", "guide.md") {
		t.Errorf("got = %q, want docs/guide.md", got)
	}
}

func TestNormalizeSkillRelativePathRejectsAbsolute(t *testing.T) {
	_, err := normalizeSkillRelativePath("/etc/passwd")
	if err == nil || !strings.Contains(err.Error(), "is invalid") {
		t.Fatalf("error = %v, want 'is invalid'", err)
	}
}

func TestNormalizeSkillRelativePathRejectsTraversal(t *testing.T) {
	tests := []string{
		"../../../etc/passwd",
		"..",
		"../secret",
	}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			_, err := normalizeSkillRelativePath(path)
			if err == nil {
				t.Errorf("normalizeSkillRelativePath(%q) = nil error, want rejection", path)
			}
		})
	}
}
