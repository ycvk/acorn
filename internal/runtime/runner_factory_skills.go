package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tooling"
)

func skillEligibilityContextFromCatalog(catalog *tooling.Catalog) skills.EligibilityContext {
	if catalog == nil {
		return skills.EligibilityContext{}
	}
	return tooling.EligibilityContextForProfile(catalog, tooling.ToolProfileRun, nil)
}

func emitSkillSelectionEvents(ctx context.Context, store eventAppender, req RunnerBuildRequest, selected *SelectedSkill, matches []SkillMatch) error {
	if store == nil || strings.TrimSpace(req.RunID) == "" {
		return nil
	}
	candidates := topSkillCandidates(matches, 3)
	discoveredSkill := &StreamSkill{
		Candidates: candidates,
	}
	if selected == nil {
		discoveredSkill.NoSelectionReason = deriveNoSelectionReason(req, matches)
	}
	if _, err := appendStreamItem(ctx, store, req.Sink, StreamItem{
		RunID:     req.RunID,
		Kind:      StreamKindSkillDiscovered,
		CreatedAt: time.Now().UTC(),
		Payload:   &SkillDiscoveredPayload{Skill: discoveredSkill},
	}); err != nil {
		return err
	}
	if selected == nil {
		return nil
	}
	streamSkill := streamSkillFromSelected(selected, candidates)
	if _, err := appendStreamItem(ctx, store, req.Sink, StreamItem{
		RunID:     req.RunID,
		Kind:      StreamKindSkillSelected,
		CreatedAt: time.Now().UTC(),
		Payload:   &SkillSelectedPayload{Skill: streamSkill},
	}); err != nil {
		return err
	}
	if _, err := appendStreamItem(ctx, store, req.Sink, StreamItem{
		RunID:     req.RunID,
		Kind:      StreamKindSkillLoaded,
		CreatedAt: time.Now().UTC(),
		Payload:   &SkillLoadedPayload{Skill: streamSkill},
	}); err != nil {
		return err
	}
	if err := emitProcedureActivationEvents(ctx, store, req.Sink, req.RunID, []memorymodule.ProcedureActivation{
		procedureActivationFromSelectedSkill(req, selected, memorymodule.ProcedureActivationSelected, "decision_selected_skill"),
		procedureActivationFromSelectedSkill(req, selected, memorymodule.ProcedureActivationUsed, "skill_loaded_for_run"),
	}); err != nil {
		return err
	}
	return nil
}

func deriveNoSelectionReason(req RunnerBuildRequest, matches []SkillMatch) string {
	if strings.TrimSpace(req.SkillID) != "" {
		if len(matches) == 0 {
			return "explicit_skill_missing"
		}
		if matches[0].FilteredReason != "" {
			return "explicit_skill_ineligible"
		}
	}
	eligible := make([]SkillMatch, 0, len(matches))
	for _, item := range matches {
		if item.FilteredReason != "" || item.Score <= 0 || !item.TriggerMatched {
			continue
		}
		eligible = append(eligible, item)
	}
	if len(eligible) == 0 {
		return noEligibleSkillMatchReason
	}
	if len(eligible) > 1 && eligible[0].Score == eligible[1].Score {
		return ambiguousTopScoreReason
	}
	return ""
}

func topSkillCandidates(matches []SkillMatch, limit int) []StreamSkillCandidate {
	if limit <= 0 || len(matches) == 0 {
		return nil
	}
	if len(matches) < limit {
		limit = len(matches)
	}
	items := make([]StreamSkillCandidate, 0, limit)
	for _, item := range matches[:limit] {
		items = append(items, StreamSkillCandidate{
			ID:             item.Skill.ID,
			Name:           item.Skill.Name,
			Score:          item.Score,
			MatchedTerms:   append([]string(nil), item.MatchedTerms...),
			FilteredReason: item.FilteredReason,
			Requirements:   streamSkillRequirementsFromDomain(item.Skill.Requires),
			Summary:        item.Skill.Summary,
			Origin:         string(item.Skill.Origin),
			TaskPattern:    item.Skill.TaskPattern,
		})
	}
	return items
}

func streamSkillFromSelected(selected *SelectedSkill, candidates []StreamSkillCandidate) *StreamSkill {
	if selected == nil {
		return nil
	}
	return &StreamSkill{
		SelectedID:   selected.Skill.ID,
		Name:         selected.Skill.Name,
		Candidates:   candidates,
		Source:       selected.Skill.Source,
		Origin:       string(selected.Skill.Origin),
		TaskPattern:  selected.Skill.TaskPattern,
		Path:         selected.Skill.Path,
		Summary:      selected.Skill.Summary,
		Instruction:  selected.Skill.Instruction,
		Scripts:      append([]string(nil), selected.Skill.Scripts...),
		Requirements: streamSkillRequirementsFromDomain(selected.Skill.Requires),
		Score:        selected.Score,
		MatchedTerms: append([]string(nil), selected.MatchedTerms...),
	}
}

func procedureActivationFromSelectedSkill(req RunnerBuildRequest, selected *SelectedSkill, phase memorymodule.ProcedureActivationPhase, reason string) memorymodule.ProcedureActivation {
	if selected == nil {
		return memorymodule.ProcedureActivation{}
	}
	return memorymodule.ProcedureActivation{
		RunID:        strings.TrimSpace(req.RunID),
		SessionID:    strings.TrimSpace(req.SessionID),
		ProcedureRef: strings.TrimSpace(selected.Skill.ID),
		Title:        strings.TrimSpace(selected.Skill.Name),
		Kind:         "executable_skill",
		Phase:        phase,
		Reason:       reason,
		Score:        float64(selected.Score),
		Origin:       memorymodule.ProcedureOrigin(strings.TrimSpace(string(selected.Skill.Origin))),
		SourceRefs:   nonEmptyStrings(selected.Skill.PromotedFrom, selected.Skill.Path),
	}
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
	copy := skills.CopySnapshot(snapshot)
	return &copy, nil
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

func recommendedSkillsFromMatches(matches []SkillMatch) []decision.RecommendedSkill {
	items := make([]decision.RecommendedSkill, 0, len(matches))
	for _, item := range matches {
		items = append(items, decision.RecommendedSkill{
			ID:             item.Skill.ID,
			Name:           item.Skill.Name,
			Score:          item.Score,
			TriggerMatched: item.TriggerMatched,
			FilteredReason: item.FilteredReason,
		})
	}
	return items
}

func emitDecisionEvents(ctx context.Context, store eventAppender, req RunnerBuildRequest, record *decision.Record, explicitSkillID string) error {
	if store == nil || strings.TrimSpace(req.RunID) == "" || record == nil {
		return nil
	}
	finalKind := StreamKindDecisionSelected
	if record.Action == decision.ActionAskUser || record.Action == decision.ActionBlock || record.Action == decision.ActionResumeRun {
		finalKind = StreamKindDecisionBlocked
	}
	decisionPayload := &DecisionSelectedPayload{
		Action:              string(record.Action),
		Intent:              record.Intent,
		SelectedSkillID:     record.SelectedSkillID,
		DecisionReason:      record.DecisionReason,
		DecisionProfileHash: record.DecisionProfileHash,
		ExplicitSkillID:     strings.TrimSpace(explicitSkillID),
	}
	if finalKind == StreamKindDecisionBlocked {
		_, err := appendStreamItem(ctx, store, req.Sink, StreamItem{
			RunID:     req.RunID,
			Kind:      finalKind,
			CreatedAt: time.Now().UTC(),
			Payload: &DecisionBlockedPayload{
				Action:              string(record.Action),
				Intent:              record.Intent,
				SelectedSkillID:     record.SelectedSkillID,
				DecisionReason:      record.DecisionReason,
				DecisionProfileHash: record.DecisionProfileHash,
				ExplicitSkillID:     strings.TrimSpace(explicitSkillID),
			},
		})
		return err
	}
	_, err := appendStreamItem(ctx, store, req.Sink, StreamItem{
		RunID:     req.RunID,
		Kind:      finalKind,
		CreatedAt: time.Now().UTC(),
		Payload:   decisionPayload,
	})
	return err
}

func streamSkillRequirementsFromDomain(item skills.Requirements) StreamSkillRequirements {
	return StreamSkillRequirements{
		Tools:    append([]string(nil), item.Tools...),
		Toolsets: append([]string(nil), item.Toolsets...),
		Bins:     append([]string(nil), item.Bins...),
		Env:      append([]string(nil), item.Env...),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return value
		}
	}
	return ""
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
