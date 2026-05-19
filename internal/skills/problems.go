package skills

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

func sortSkills(items []Spec) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].ID < items[j].ID
	})
}

func sortSkillProblems(items []Problem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Source != items[j].Source {
			return items[i].Source < items[j].Source
		}
		if items[i].Path != items[j].Path {
			return items[i].Path < items[j].Path
		}
		return items[i].Error < items[j].Error
	})
}

func filterDuplicateSkillNames(items []Spec) ([]Spec, []Problem) {
	seen := make(map[string]string, len(items))
	out := make([]Spec, 0, len(items))
	problems := make([]Problem, 0)
	for _, item := range items {
		key := strings.TrimSpace(item.Name)
		if key == "" {
			out = append(out, item)
			continue
		}
		if previousID, ok := seen[key]; ok {
			problems = append(problems, Problem{
				ID:     item.ID,
				Name:   item.Name,
				Source: item.Source,
				Path:   item.Path,
				Error:  fmt.Sprintf("duplicate skill name %q (conflicts with %s)", item.Name, previousID),
			})
			continue
		}
		seen[key] = item.ID
		out = append(out, item)
	}
	return out, problems
}

func samePathRoot(left, right string) bool {
	return filepath.Clean(left) == filepath.Clean(right)
}

func shadowedSkillProblem(shadowed, winner Spec) Problem {
	return Problem{
		ID:     shadowed.ID,
		Name:   shadowed.Name,
		Source: shadowed.Source,
		Path:   shadowed.Path,
		Error:  fmt.Sprintf("shadowed by %s from %s", winner.ID, winner.Source),
	}
}

func skillProblemForDir(dir, scope, id, name, text string) *Problem {
	return &Problem{
		ID:     strings.TrimSpace(id),
		Name:   strings.TrimSpace(name),
		Source: strings.TrimSpace(scope),
		Path:   strings.TrimSpace(dir),
		Error:  strings.TrimSpace(text),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
