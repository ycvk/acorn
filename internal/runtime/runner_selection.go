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

type runSelection struct {
	selectedSkill *SelectedSkill
}

func (f *RunnerFactory) resolveRunSelection(
	ctx context.Context,
	req RunnerBuildRequest,
	caps *runCapabilities,
) (*runSelection, error) {
	if caps == nil {
		return nil, fmt.Errorf("run capabilities are required")
	}
	if isEmptyRunSelectionInput(req) {
		return &runSelection{}, nil
	}
	if strings.TrimSpace(req.Input) != "" || strings.TrimSpace(req.SkillID) != "" {
		return f.resolveRunSelectionByDecision(ctx, req, caps)
	}
	return f.resolveRunSelectionByResume(ctx, req, caps)
}

func isEmptyRunSelectionInput(req RunnerBuildRequest) bool {
	return strings.TrimSpace(req.Input) == "" && strings.TrimSpace(req.SkillID) == "" && strings.TrimSpace(req.RunID) == ""
}

// resolveRunSelectionByDecision inlines the former decision.Engine logic:
// skill selection comes from skills.RetrieveCandidates; the only remaining
// decision is whether to block (missing required capability or unavailable
// explicit skill), proceed with a skill, or proceed without one.
func (f *RunnerFactory) resolveRunSelectionByDecision(ctx context.Context, req RunnerBuildRequest, caps *runCapabilities) (*runSelection, error) {
	discovered, err := f.retrieveSkillCandidates(req, caps)
	if err != nil {
		return nil, err
	}
	if hasCapabilityFailure(discovered) {
		return f.blockRun(ctx, req, "missing_required_capability", "")
	}
	if explicitID := strings.TrimSpace(req.SkillID); explicitID != "" {
		return f.resolveExplicitSkill(ctx, req, caps, discovered, explicitID)
	}
	if top, ok := topRecommendedSkill(discovered); ok {
		return f.resolveTopSkill(ctx, req, caps, discovered, top)
	}
	if emitErr := emitSkillSelectionEvents(ctx, f.deps.Store, req, nil, discovered); emitErr != nil {
		return nil, emitErr
	}
	return &runSelection{}, nil
}

func (f *RunnerFactory) blockRun(ctx context.Context, req RunnerBuildRequest, reason, explicitSkillID string) (*runSelection, error) {
	if emitErr := emitDecisionBlockedEvent(ctx, f.deps.Store, req, "block", reason, explicitSkillID); emitErr != nil {
		return nil, emitErr
	}
	if explicitSkillID != "" {
		return nil, fmt.Errorf("decision blocked execution: %s: %s", reason, explicitSkillID)
	}
	return nil, fmt.Errorf("decision blocked execution: %s", reason)
}

func (f *RunnerFactory) resolveExplicitSkill(ctx context.Context, req RunnerBuildRequest, caps *runCapabilities, discovered []SkillMatch, explicitID string) (*runSelection, error) {
	match, ok := findEligibleSkillByID(discovered, explicitID)
	if !ok {
		return f.blockRun(ctx, req, "explicit_skill_unavailable", explicitID)
	}
	selected := selectedSkillFromMatch(match, caps.stableSkills, true)
	if emitErr := emitSkillSelectionEvents(ctx, f.deps.Store, req, selected, discovered); emitErr != nil {
		return nil, emitErr
	}
	return &runSelection{selectedSkill: selected}, nil
}

func (f *RunnerFactory) resolveTopSkill(ctx context.Context, req RunnerBuildRequest, caps *runCapabilities, discovered []SkillMatch, top SkillMatch) (*runSelection, error) {
	selected := selectedSkillFromMatch(top, caps.stableSkills, false)
	if emitErr := emitSkillSelectionEvents(ctx, f.deps.Store, req, selected, discovered); emitErr != nil {
		return nil, emitErr
	}
	return &runSelection{selectedSkill: selected}, nil
}

func (f *RunnerFactory) retrieveSkillCandidates(req RunnerBuildRequest, caps *runCapabilities) ([]SkillMatch, error) {
	retrieved, err := skills.RetrieveCandidates(skills.CandidateQuery{
		Input:           req.Input,
		ExplicitSkillID: req.SkillID,
		Eligibility:     skillEligibilityContextFromCatalog(caps.catalog),
	}, caps.stableSkills)
	if err != nil {
		return nil, err
	}
	if retrieved == nil {
		return nil, nil
	}
	return runtimeMatchesFromRecommendations(retrieved.Candidates), nil
}

func (f *RunnerFactory) resolveRunSelectionByResume(ctx context.Context, req RunnerBuildRequest, caps *runCapabilities) (*runSelection, error) {

	explicitID := strings.TrimSpace(req.SkillID)
	if explicitID == "" {
		return &runSelection{}, nil
	}
	for _, spec := range caps.stableSkills {
		if spec.ID == explicitID {
			return &runSelection{
				selectedSkill: &SelectedSkill{
					Skill:    skills.CopySpec(spec),
					Explicit: true,
				},
			}, nil
		}
	}
	return nil, fmt.Errorf("resume selected skill %q not found", explicitID)
}

// hasCapabilityFailure reports whether any candidate was filtered out due to
// a missing required capability.
func hasCapabilityFailure(matches []SkillMatch) bool {
	for _, m := range matches {
		if strings.Contains(m.FilteredReason, "missing_required_") {
			return true
		}
	}
	return false
}

func findEligibleSkillByID(matches []SkillMatch, skillID string) (SkillMatch, bool) {
	normalized := strings.TrimSpace(skillID)
	if normalized == "" {
		return SkillMatch{}, false
	}
	for _, m := range matches {
		if strings.TrimSpace(m.Skill.ID) == normalized && isEligibleMatch(m) {
			return m, true
		}
	}
	return SkillMatch{}, false
}

func topRecommendedSkill(matches []SkillMatch) (SkillMatch, bool) {
	var best SkillMatch
	found := false
	for _, m := range matches {
		if !isEligibleMatch(m) {
			continue
		}
		if !found || m.Score > best.Score || (m.Score == best.Score && m.Skill.ID < best.Skill.ID) {
			best = m
			found = true
		}
	}
	if !found {
		return SkillMatch{}, false
	}
	return best, true
}

func isEligibleMatch(m SkillMatch) bool {
	return strings.TrimSpace(m.Skill.ID) != "" && m.FilteredReason == "" && m.TriggerMatched && m.Score > 0
}

func selectedSkillFromMatch(match SkillMatch, stableSkills []skills.Spec, explicit bool) *SelectedSkill {
	score := match.Score
	matchedTerms := append([]string(nil), match.MatchedTerms...)
	for _, item := range stableSkills {
		if item.ID == match.Skill.ID {
			return &SelectedSkill{
				Skill:        skills.CopySpec(item),
				Score:        score,
				MatchedTerms: matchedTerms,
				Explicit:     explicit,
			}
		}
	}
	return &SelectedSkill{
		Skill:        skills.CopySpec(match.Skill),
		Score:        score,
		MatchedTerms: matchedTerms,
		Explicit:     explicit,
	}
}
