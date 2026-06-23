package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeSkillDirIDValid(t *testing.T) {
	got, err := normalizeSkillDirID("my-skill")
	if err != nil {
		t.Fatalf("normalizeSkillDirID: %v", err)
	}
	if got != "my-skill" {
		t.Errorf("got = %q, want my-skill", got)
	}
}

func TestNormalizeSkillDirIDTrims(t *testing.T) {
	got, err := normalizeSkillDirID("  my-skill  ")
	if err != nil {
		t.Fatalf("normalizeSkillDirID: %v", err)
	}
	if got != "my-skill" {
		t.Errorf("got = %q, want my-skill", got)
	}
}

func TestNormalizeSkillDirIDEmpty(t *testing.T) {
	_, err := normalizeSkillDirID("   ")
	if err == nil || !strings.Contains(err.Error(), "skill id is required") {
		t.Fatalf("error = %v, want 'skill id is required'", err)
	}
}

func TestNormalizeSkillDirIDRejectsPathTraversal(t *testing.T) {
	tests := []string{
		"../escape",
		"a/b",
		"a" + string(os.PathSeparator) + "b",
		"..",
	}
	for _, id := range tests {
		t.Run(id, func(t *testing.T) {
			_, err := normalizeSkillDirID(id)
			if err == nil {
				t.Errorf("normalizeSkillDirID(%q) = nil error, want rejection", id)
			}
		})
	}
}

func TestValidateSkillWriteContentValid(t *testing.T) {
	got, err := validateSkillWriteContent("patch", "my-skill", "new content")
	if err != nil {
		t.Fatalf("validateSkillWriteContent: %v", err)
	}
	if got != "new content" {
		t.Errorf("got = %q, want 'new content'", got)
	}
}

func TestValidateSkillWriteContentTrims(t *testing.T) {
	got, err := validateSkillWriteContent("patch", "id", "  content  ")
	if err != nil {
		t.Fatalf("validateSkillWriteContent: %v", err)
	}
	if got != "content" {
		t.Errorf("got = %q, want 'content' (trimmed)", got)
	}
}

func TestValidateSkillWriteContentEmpty(t *testing.T) {
	_, err := validateSkillWriteContent("patch", "id", "   ")
	if err == nil || !strings.Contains(err.Error(), "content is empty") {
		t.Fatalf("error = %v, want 'content is empty'", err)
	}
}

func TestValidateSkillWriteContentRejectsFrontmatterDelimiter(t *testing.T) {
	tests := []string{
		"---",
		"text\n---\nmore",
		"  ---  ",
	}
	for _, content := range tests {
		t.Run(content, func(t *testing.T) {
			_, err := validateSkillWriteContent("patch", "id", content)
			if err == nil || !strings.Contains(err.Error(), "frontmatter delimiters") {
				t.Fatalf("error = %v, want 'frontmatter delimiters' rejection", err)
			}
		})
	}
}

func TestSkillModifiable(t *testing.T) {
	tests := []struct {
		source string
		want   bool
	}{
		{WorkspaceScope, true},
		{GeneratedScope, true},
		{UserScope, true},
		{BuiltinScope, false},
		{"unknown", false},
		{"", false},
		{"  " + WorkspaceScope + "  ", true}, // trims
	}
	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			if got := skillModifiable(tt.source); got != tt.want {
				t.Errorf("skillModifiable(%q) = %v, want %v", tt.source, got, tt.want)
			}
		})
	}
}

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

func TestNormalizeCreateInputValid(t *testing.T) {
	input := CreateInput{
		ID:          "my-skill",
		Name:        "My Skill",
		Instruction: "Do the thing",
	}
	got, err := normalizeCreateInput(input)
	if err != nil {
		t.Fatalf("normalizeCreateInput: %v", err)
	}
	if got.ID != "my-skill" {
		t.Errorf("ID = %q", got.ID)
	}
	if got.Name != "My Skill" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.Origin != OriginHuman {
		t.Errorf("Origin = %q, want %q (default)", got.Origin, OriginHuman)
	}
}

func TestNormalizeCreateInputErrors(t *testing.T) {
	tests := []struct {
		name  string
		input CreateInput
		err   string
	}{
		{
			name:  "missing name",
			input: CreateInput{ID: "x", Instruction: "inst"},
			err:   "skill name is required",
		},
		{
			name:  "missing instruction",
			input: CreateInput{ID: "x", Name: "X"},
			err:   "instruction is required",
		},
		{
			name:  "distilled without task_pattern",
			input: CreateInput{ID: "x", Name: "X", Instruction: "i", Origin: OriginDistilled},
			err:   "task_pattern is required for distilled origin",
		},
		{
			name:  "invalid id path traversal",
			input: CreateInput{ID: "../x", Name: "X", Instruction: "i"},
			err:   "invalid",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizeCreateInput(tt.input)
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Fatalf("error = %v, want containing %q", err, tt.err)
			}
		})
	}
}

func TestBuildCreateFrontmatter(t *testing.T) {
	input := CreateInput{
		ID:          "test",
		Name:        "Test",
		Version:     "v2",
		Summary:     "A test skill",
		TaskPattern: "test pattern",
		Tags:        []string{"a", "b"},
	}
	fm := buildCreateFrontmatter(input)
	if fm.ID != "test" || fm.Name != "Test" || fm.Version != "v2" {
		t.Errorf("frontmatter = {ID: %q, Name: %q, Version: %q}", fm.ID, fm.Name, fm.Version)
	}
	if fm.Summary != "A test skill" {
		t.Errorf("Summary = %q", fm.Summary)
	}
	if fm.TaskPattern != "test pattern" {
		t.Errorf("TaskPattern = %q", fm.TaskPattern)
	}
}
