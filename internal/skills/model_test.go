package skills

import (
	"strings"
	"testing"
)

func TestNormalizeSpecTrimsFields(t *testing.T) {
	spec := Spec{
		ID:        "  skill.id  ",
		Name:      "  Test  ",
		Tags:      []string{"  a  ", "b", "", "a"},
		Platforms: []string{"Linux", "linux", "  Darwin  "},
		Replaces:  []string{"old", "old", ""},
	}
	got, err := NormalizeSpec(spec)
	if err != nil {
		t.Fatalf("NormalizeSpec: %v", err)
	}
	if got.ID != "skill.id" {
		t.Errorf("ID = %q, want skill.id", got.ID)
	}
	if got.Name != "Test" {
		t.Errorf("Name = %q, want Test", got.Name)
	}
	if got.Version != "v1" {
		t.Errorf("Version = %q, want v1 (default)", got.Version)
	}
	if got.Source != "unknown" {
		t.Errorf("Source = %q, want unknown (default)", got.Source)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "a" || got.Tags[1] != "b" {
		t.Errorf("Tags = %v, want [a b]", got.Tags)
	}
	if len(got.Platforms) != 2 || got.Platforms[0] != "linux" || got.Platforms[1] != "darwin" {
		t.Errorf("Platforms = %v, want [linux darwin] (lowercased, deduped)", got.Platforms)
	}
	if len(got.Replaces) != 1 || got.Replaces[0] != "old" {
		t.Errorf("Replaces = %v, want [old]", got.Replaces)
	}
}

func TestNormalizeSpecErrors(t *testing.T) {
	tests := []struct {
		name string
		spec Spec
		err  string
	}{
		{
			name: "missing id",
			spec: Spec{Name: "X"},
			err:  "skill id is required",
		},
		{
			name: "missing name",
			spec: Spec{ID: "x"},
			err:  "skill x name is required",
		},
		{
			name: "distilled without task_pattern",
			spec: Spec{ID: "x", Name: "X", Origin: OriginDistilled},
			err:  "task_pattern is required for distilled origin",
		},
		{
			name: "invalid origin",
			spec: Spec{ID: "x", Name: "X", Origin: Origin("bogus")},
			err:  `origin "bogus" is invalid`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeSpec(tt.spec)
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Fatalf("error = %v, want containing %q", err, tt.err)
			}
		})
	}
}

func TestNormalizeSpecDefaultsHumanOrigin(t *testing.T) {
	spec := Spec{ID: "x", Name: "X"}
	got, err := NormalizeSpec(spec)
	if err != nil {
		t.Fatalf("NormalizeSpec: %v", err)
	}
	if got.Origin != OriginHuman {
		t.Errorf("Origin = %q, want %q (default)", got.Origin, OriginHuman)
	}
}

func TestCopySpecDeepCopiesSlices(t *testing.T) {
	original := Spec{
		ID:           "x",
		Name:         "X",
		Tags:         []string{"a", "b"},
		Platforms:    []string{"linux"},
		TriggerHints: []string{"hint"},
		Replaces:     []string{"old"},
		Requires: Requirements{
			Tools: []string{"read_file"},
		},
	}
	copy := CopySpec(original)
	copy.Tags[0] = "mutated"
	copy.Requires.Tools[0] = "mutated"
	if original.Tags[0] != "a" {
		t.Error("CopySpec did not deep-copy Tags")
	}
	if original.Requires.Tools[0] != "read_file" {
		t.Error("CopySpec did not deep-copy Requires.Tools")
	}
}

func TestUniqueNonEmpty(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil", nil, nil},
		{"all empty", []string{"", "  ", "\t"}, nil},
		{"dedup preserving order", []string{"b", "a", "b", "a"}, []string{"b", "a"}},
		{"trims before dedup", []string{" a", "a ", "a"}, []string{"a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uniqueNonEmpty(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("uniqueNonEmpty(%v) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("uniqueNonEmpty(%v)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestUniqueLowerNonEmpty(t *testing.T) {
	got := uniqueLowerNonEmpty([]string{"Linux", "linux", "DARWIN", "darwin", ""})
	if len(got) != 2 || got[0] != "linux" || got[1] != "darwin" {
		t.Errorf("uniqueLowerNonEmpty = %v, want [linux darwin]", got)
	}
}
