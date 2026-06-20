package runtime

import (
	"context"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/memorymodule"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/stream"
)

func emitSkillSelectionEvents(ctx context.Context, store runtimeapi.EventAppender, req RunnerBuildRequest, selected *SelectedSkill, matches []SkillMatch) error {
	if store == nil || strings.TrimSpace(req.RunID) == "" {
		return nil
	}
	candidates := topSkillCandidates(matches, 3)
	if err := emitSkillDiscovered(ctx, store, req, candidates, selected, matches); err != nil {
		return err
	}
	if selected == nil {
		return nil
	}
	return emitSkillSelected(ctx, store, req, selected, candidates)
}

func emitSkillDiscovered(ctx context.Context, store runtimeapi.EventAppender, req RunnerBuildRequest, candidates []stream.StreamSkillCandidate, selected *SelectedSkill, matches []SkillMatch) error {
	discovered := &stream.StreamSkill{Candidates: candidates}
	if selected == nil {
		discovered.NoSelectionReason = deriveNoSelectionReason(req, matches)
	}
	_, err := stream.AppendStreamItem(ctx, store, req.Sink, stream.StreamItem{
		RunID:     req.RunID,
		Kind:      stream.StreamKindSkillDiscovered,
		CreatedAt: time.Now().UTC(),
		Payload:   map[string]any{"skill": discovered},
	})
	return err
}

func emitSkillSelected(ctx context.Context, store runtimeapi.EventAppender, req RunnerBuildRequest, selected *SelectedSkill, candidates []stream.StreamSkillCandidate) error {
	streamSkill := streamSkillFromSelected(selected, candidates)
	for _, kind := range []stream.StreamItemKind{stream.StreamKindSkillSelected, stream.StreamKindSkillLoaded} {
		if _, err := stream.AppendStreamItem(ctx, store, req.Sink, stream.StreamItem{
			RunID:     req.RunID,
			Kind:      kind,
			CreatedAt: time.Now().UTC(),
			Payload:   map[string]any{"skill": streamSkill},
		}); err != nil {
			return err
		}
	}
	return emitProcedureActivationEvents(ctx, store, req.Sink, req.RunID, []memorymodule.ProcedureActivation{
		procedureActivationFromSelectedSkill(req, selected, memorymodule.ProcedureActivationSelected, "decision_selected_skill"),
		procedureActivationFromSelectedSkill(req, selected, memorymodule.ProcedureActivationUsed, "skill_loaded_for_run"),
	})
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

func topSkillCandidates(matches []SkillMatch, limit int) []stream.StreamSkillCandidate {
	if limit <= 0 || len(matches) == 0 {
		return nil
	}
	if len(matches) < limit {
		limit = len(matches)
	}
	items := make([]stream.StreamSkillCandidate, 0, limit)
	for _, item := range matches[:limit] {
		items = append(items, stream.StreamSkillCandidate{
			ID:             item.Skill.ID,
			Name:           item.Skill.Name,
			Score:          item.Score,
			MatchedTerms:   append([]string(nil), item.MatchedTerms...),
			FilteredReason: item.FilteredReason,
			Requirements:   StreamSkillRequirementsFromDomain(item.Skill.Requires),
			Summary:        item.Skill.Summary,
			Origin:         string(item.Skill.Origin),
			TaskPattern:    item.Skill.TaskPattern,
		})
	}
	return items
}

func streamSkillFromSelected(selected *SelectedSkill, candidates []stream.StreamSkillCandidate) *stream.StreamSkill {
	if selected == nil {
		return nil
	}
	return &stream.StreamSkill{
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
		Requirements: StreamSkillRequirementsFromDomain(selected.Skill.Requires),
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

func emitDecisionEvents(ctx context.Context, store runtimeapi.EventAppender, req RunnerBuildRequest, record *decision.Record, explicitSkillID string) error {
	if store == nil || strings.TrimSpace(req.RunID) == "" || record == nil {
		return nil
	}
	finalKind := stream.StreamKindDecisionSelected
	if record.Action == decision.ActionAskUser || record.Action == decision.ActionBlock {
		finalKind = stream.StreamKindDecisionBlocked
	}
	decisionPayload := map[string]any{
		"action":                string(record.Action),
		"intent":                record.Intent,
		"selected_skill_id":     record.SelectedSkillID,
		"decision_reason":       record.DecisionReason,
		"decision_profile_hash": record.DecisionProfileHash,
		"explicit_skill_id":     strings.TrimSpace(explicitSkillID),
	}
	_, err := stream.AppendStreamItem(ctx, store, req.Sink, stream.StreamItem{
		RunID:     req.RunID,
		Kind:      finalKind,
		CreatedAt: time.Now().UTC(),
		Payload:   decisionPayload,
	})
	return err
}

func StreamSkillRequirementsFromDomain(item skills.Requirements) stream.StreamSkillRequirements {
	return stream.StreamSkillRequirements{
		Tools:    append([]string(nil), item.Tools...),
		Toolsets: append([]string(nil), item.Toolsets...),
		Bins:     append([]string(nil), item.Bins...),
		Env:      append([]string(nil), item.Env...),
	}
}
