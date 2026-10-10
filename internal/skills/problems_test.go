package skills

import (
	"strings"
	"testing"
)

func TestFilterDuplicateSkillNames(t *testing.T) {
	items := []Spec{
		{ID: "a", Name: "Same"},
		{ID: "b", Name: "Same"},
		{ID: "c", Name: "Unique"},
		{ID: "d", Name: "  "},
	}
	out, problems := filterDuplicateSkillNames(items)
	if len(out) != 3 {
		t.Fatalf("out = %d items, want 3 (unique + unnamed)", len(out))
	}
	if len(problems) != 1 {
		t.Fatalf("problems = %d, want 1 duplicate", len(problems))
	}
	if !strings.Contains(problems[0].Error, "duplicate skill name") {
		t.Errorf("problem error = %q, want duplicate name message", problems[0].Error)
	}
	if problems[0].ID != "b" {
		t.Errorf("problem ID = %q, want 'b' (the duplicate)", problems[0].ID)
	}
}

func TestFilterDuplicateSkillNamesAllUnique(t *testing.T) {
	items := []Spec{
		{ID: "a", Name: "A"},
		{ID: "b", Name: "B"},
	}
	out, problems := filterDuplicateSkillNames(items)
	if len(out) != 2 {
		t.Fatalf("out = %d, want 2", len(out))
	}
	if len(problems) != 0 {
		t.Fatalf("problems = %d, want 0", len(problems))
	}
}

func TestSortSkillsByNameThenID(t *testing.T) {
	items := []Spec{
		{ID: "z", Name: "B"},
		{ID: "y", Name: "A"},
		{ID: "x", Name: "A"},
	}
	sortSkills(items)
	if items[0].ID != "x" || items[1].ID != "y" || items[2].ID != "z" {
		t.Errorf("order = %v, want sorted by name then ID", items)
	}
}

func TestSortSkillProblemsByPathThenError(t *testing.T) {
	items := []Problem{
		{Path: "/b", Error: "z"},
		{Path: "/a", Error: "b"},
		{Path: "/a", Error: "a"},
	}
	sortSkillProblems(items)
	if items[0].Error != "a" || items[1].Error != "b" || items[2].Path != "/b" {
		t.Errorf("order = %v, want sorted by path then error", items)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{"all empty", []string{"", "  ", "\t"}, ""},
		{"first non-empty", []string{"", "first", "second"}, "first"},
		{"trimmed", []string{"  trimmed  "}, "trimmed"},
		{"nil", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FirstNonEmpty(tt.values...)
			if got != tt.want {
				t.Fatalf("firstNonEmpty(%v) = %q, want %q", tt.values, got, tt.want)
			}
		})
	}
}

func TestSkillProblemForDir(t *testing.T) {
	p := skillProblemForDir("/dir", "  id  ", "  name  ", "  text  ")
	if p.ID != "id" || p.Name != "name" || p.Path != "/dir" || p.Error != "text" {
		t.Errorf("problem = %#v, want trimmed values", p)
	}
}
