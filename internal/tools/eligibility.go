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
	for _, spec := range specs {
		available = append(available, spec.Name)
	}
	return skills.EligibilityContext{AvailableTools: available}
}
