package skills

import (
	"strings"
	"testing"
)

func TestFilterDuplicateSkillNames(t *testing.T) {
	items := []Spec{
		{ID: "a", Name: "Same", Source: "workspace"},
		{ID: "b", Name: "Same", Source: "generated"},
		{ID: "c", Name: "Unique", Source: "workspace"},
		{ID: "d", Name: "  ", Source: "workspace"},
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

func TestSortSkillProblemsBySourcePathError(t *testing.T) {
	items := []Problem{
		{Source: "workspace", Path: "/b", Error: "z"},
		{Source: "builtin", Path: "/a", Error: "a"},
		{Source: "workspace", Path: "/a", Error: "b"},
	}
	sortSkillProblems(items)
	if items[0].Source != "builtin" {
		t.Errorf("first = %q, want builtin (sorted by source)", items[0].Source)
	}
	if items[1].Path != "/a" || items[2].Path != "/b" {
		t.Errorf("order = %v, want sorted by path within same source", items)
	}
}

func TestSamePathRoot(t *testing.T) {
	tests := []struct {
		left, right string
		want        bool
	}{
		{"./skills", "skills", true},
		{"skills/", "skills", true},
		{"skills", "generated", false},
		{"/abs/path", "/abs/path", true},
	}
	for _, tt := range tests {
		got := samePathRoot(tt.left, tt.right)
		if got != tt.want {
			t.Errorf("samePathRoot(%q, %q) = %v, want %v", tt.left, tt.right, got, tt.want)
		}
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
			got := firstNonEmpty(tt.values...)
			if got != tt.want {
				t.Fatalf("firstNonEmpty(%v) = %q, want %q", tt.values, got, tt.want)
			}
		})
	}
}

func TestShadowedSkillProblem(t *testing.T) {
	shadowed := Spec{ID: "old", Name: "Old", Source: "generated", Path: "/path/old"}
	winner := Spec{ID: "new", Name: "New", Source: "workspace", Path: "/path/new"}
	p := shadowedSkillProblem(shadowed, winner)
	if p.ID != "old" {
		t.Errorf("ID = %q, want old", p.ID)
	}
	if !strings.Contains(p.Error, "shadowed by new") {
		t.Errorf("Error = %q, want 'shadowed by new'", p.Error)
	}
	if !strings.Contains(p.Error, "workspace") {
		t.Errorf("Error = %q, want to contain winner source", p.Error)
	}
}

func TestSkillProblemForDir(t *testing.T) {
	p := skillProblemForDir("/dir", "workspace", "  id  ", "  name  ", "  text  ")
	if p == nil {
		t.Fatal("got nil problem")
	}
	if p.ID != "id" || p.Name != "name" || p.Source != "workspace" || p.Path != "/dir" {
		t.Errorf("problem = {ID: %q, Name: %q, Source: %q, Path: %q}, want trimmed values", p.ID, p.Name, p.Source, p.Path)
	}
	if p.Error != "text" {
		t.Errorf("Error = %q, want 'text' (trimmed)", p.Error)
	}
}
