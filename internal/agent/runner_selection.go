package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tools"
)

const capabilityDiscoveryInstruction = `Capability discovery rules:
- Before answering a capability question or saying you cannot do something, inspect the skill catalog and currently loaded tools already present in context.
- If a relevant skill may exist but the catalog summary is not enough, call skill_list or skill_view before answering.
- If a relevant capability depends on deferred tools, call load_tools before concluding the capability is unavailable.
- Prefer the matching skill and tool path over a generic limitation answer.`

func buildStableInstruction(base string, instructionSuffix string) string {
	parts := []string{
		strings.TrimSpace(base),
		strings.TrimSpace(capabilityDiscoveryInstruction),
		strings.TrimSpace(instructionSuffix),
	}
	out := make([]string, 0, len(parts))
	for _, item := range parts {
		if strings.TrimSpace(item) != "" {
			out = append(out, strings.TrimSpace(item))
		}
	}
	return strings.Join(out, "\n\n")
}

func skillEligibilityContextFromCatalog(catalog *tools.Catalog) skills.EligibilityContext {
	if catalog == nil {
		return skills.EligibilityContext{}
	}
	return tools.EligibilityContext(catalog, nil)
}

func loadStableSkillSnapshot(ctx context.Context, loader interface {
	ScanSkills(context.Context) (*skills.ScanResult, error)
}, eligibility skills.EligibilityContext) (*skills.Snapshot, error) {
	if loader == nil {
		return nil, nil
	}
	scan, err := loader.ScanSkills(ctx)
	if err != nil {
		return nil, fmt.Errorf("load skills: %w", err)
	}
	if scan == nil {
		return nil, nil
	}
	snapshot, err := skills.BuildSnapshot(*scan, eligibility)
	if err != nil {
		return nil, fmt.Errorf("build skill snapshot: %w", err)
	}
	copied := skills.CopySnapshot(snapshot)
	return &copied, nil
}

func stableSkillsFromSnapshot(snapshot *skills.Snapshot) []skills.Spec {
	if snapshot == nil || len(snapshot.Skills) == 0 {
		return nil
	}
	items := make([]skills.Spec, 0, len(snapshot.Skills))
	for _, item := range snapshot.Skills {
		items = append(items, skills.CopySpec(item.Spec))
	}
	return items
}
