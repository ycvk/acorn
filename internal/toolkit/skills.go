package toolkit

import "github.com/ycvk/acorn/internal/skills"

func EligibilityContext(catalog *Catalog, env map[string]string) skills.EligibilityContext {
	if catalog == nil {
		return skills.EligibilityContext{Env: copyEnv(env)}
	}
	specs := catalog.EnabledSpecs()
	availableTools := make([]string, 0, len(specs))
	availableToolsets := make([]string, 0, len(specs))
	seenTools := make(map[string]struct{}, len(specs))
	seenToolsets := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		if _, ok := seenTools[spec.Name]; !ok {
			seenTools[spec.Name] = struct{}{}
			availableTools = append(availableTools, spec.Name)
		}
		if spec.Source == "" {
			continue
		}
		if _, ok := seenToolsets[spec.Source]; ok {
			continue
		}
		seenToolsets[spec.Source] = struct{}{}
		availableToolsets = append(availableToolsets, spec.Source)
	}
	return skills.EligibilityContext{
		AvailableTools:    availableTools,
		AvailableToolsets: availableToolsets,
		Env:               copyEnv(env),
	}
}

func copyEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string, len(env))
	for key, value := range env {
		out[key] = value
	}
	return out
}
