package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tooling"
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

func skillEligibilityContextFromCatalog(catalog *tooling.Catalog) skills.EligibilityContext {
	if catalog == nil {
		return skills.EligibilityContext{}
	}
	return tooling.EligibilityContext(catalog, nil)
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

func runtimeMatchesFromRecommendations(items []skills.Recommendation) []SkillMatch {
	if len(items) == 0 {
		return nil
	}
	out := make([]SkillMatch, 0, len(items))
	for _, item := range items {
		out = append(out, SkillMatch{
			Skill:          skills.CopySpec(item.Skill),
			Score:          item.Score,
			MatchedTerms:   append([]string(nil), item.MatchedTerms...),
			TriggerMatched: item.TriggerMatched,
			FilteredReason: item.FilteredReason,
		})
	}
	return out
}

func nonEmptyStrings(items ...string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return value
		}
	}
	return ""
}
