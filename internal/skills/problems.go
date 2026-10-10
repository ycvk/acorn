package skills

import (
	"fmt"
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
				ID:    item.ID,
				Name:  item.Name,
				Path:  item.Path,
				Error: fmt.Sprintf("duplicate skill name %q (conflicts with %s)", item.Name, previousID),
			})
			continue
		}
		seen[key] = item.ID
		out = append(out, item)
	}
	return out, problems
}

func skillProblemForDir(dir, id, name, text string) *Problem {
	return &Problem{
		ID:    strings.TrimSpace(id),
		Name:  strings.TrimSpace(name),
		Path:  strings.TrimSpace(dir),
		Error: strings.TrimSpace(text),
	}
}

func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
