package skills

import (
	"sort"
	"strings"
)

func sortSkills(items []Spec) {
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
}

func sortSkillProblems(items []Problem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].Error < items[j].Error
	})
}

func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
