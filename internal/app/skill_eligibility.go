package app

import (
	"os"
	"strings"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/toolkit"
)

func environmentMap() map[string]string {
	env := make(map[string]string)
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		env[key] = value
	}
	return env
}

func localEligibilityToolNames(cfg *config.Config) []string {
	specs := toolkit.ConfiguredLocalSpecs(cfg)
	names := make([]string, 0, len(specs)+11)
	seen := make(map[string]struct{}, len(specs)+11)
	for _, spec := range specs {
		if spec.Enabled() {
			if _, ok := seen[spec.Name]; ok {
				continue
			}
			seen[spec.Name] = struct{}{}
			names = append(names, spec.Name)
		}
	}
	for _, name := range toolkit.BuiltinToolNames() {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

func staticSkillEligibilityContext(cfg *config.Config) skills.EligibilityContext {
	availableTools := localEligibilityToolNames(cfg)
	availableToolsets := make([]string, 0, 1)
	if cfg != nil {
		for _, provider := range cfg.MCP.Providers {
			if !provider.Enabled {
				continue
			}
			availableToolsets = append(availableToolsets, provider.Name)
			availableTools = append(availableTools, provider.ToolNames...)
		}
	}
	return skills.EligibilityContext{
		AvailableTools:    availableTools,
		AvailableToolsets: availableToolsets,
		Env:               environmentMap(),
	}
}
