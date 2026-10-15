package tools

import (
	"github.com/ycvk/acorn/internal/skills"
)

// EligibilityContext is what the run's enabled tools offer a skill.
func EligibilityContext(catalog *Catalog) skills.EligibilityContext {
	if catalog == nil {
		return skills.EligibilityContext{}
	}
	specs := catalog.EnabledSpecs()
	available := make([]string, 0, len(specs))
	seen := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		if _, ok := seen[spec.Name]; ok {
			continue
		}
		seen[spec.Name] = struct{}{}
		available = append(available, spec.Name)
	}
	return skills.EligibilityContext{AvailableTools: available}
}
