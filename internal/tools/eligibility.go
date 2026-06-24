package tools

import (
	"github.com/ycvk/acorn/internal/port"
	"github.com/ycvk/acorn/internal/skills"
)

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
		if _, ok := seenTools[spec.Contract.Name]; !ok {
			seenTools[spec.Contract.Name] = struct{}{}
			availableTools = append(availableTools, spec.Contract.Name)
		}
		if spec.Contract.Source == "" {
			continue
		}
		if _, ok := seenToolsets[spec.Contract.Source]; ok {
			continue
		}
		seenToolsets[spec.Contract.Source] = struct{}{}
		availableToolsets = append(availableToolsets, spec.Contract.Source)
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

// ensure port import is used
var _ port.ToolSpec
