package runtime

import (
	"context"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/stream"
)

func emitMemoryPreparedEvent(ctx context.Context, store domain.EventAppender, req RunnerBuildRequest, workspaceScope string, result *memory.PrepareResult) error {
	if store == nil || strings.TrimSpace(req.RunID) == "" {
		return nil
	}
	prepared := &stream.StreamMemoryPrepared{
		Query:          strings.TrimSpace(req.Input),
		WorkspaceScope: strings.TrimSpace(workspaceScope),
	}
	if result != nil {
		prepared.NudgeCount = len(result.Nudges)
		prepared.EntryCount = len(result.Entries)
		prepared.Nudges = streamMemoryNudges(result.Nudges)
		prepared.Entries = streamMemoryEntries(result.Entries)
	}
	_, err := stream.AppendStreamItem(ctx, store, req.Sink, domain.StreamItem{
		RunID:     req.RunID,
		Kind:      domain.StreamKindMemoryPrepared,
		CreatedAt: time.Now().UTC(),
		Payload:   map[string]any{"memory_prepared": prepared},
	})
	return err
}

func streamMemoryNudges(nudges []memory.Nudge) []stream.StreamMemoryPreparedNudge {
	out := make([]stream.StreamMemoryPreparedNudge, 0, len(nudges))
	for _, n := range nudges {
		out = append(out, stream.StreamMemoryPreparedNudge{
			Ref: n.Ref, Kind: n.Kind, Title: n.Title, Status: n.Status, Reason: n.Reason,
		})
	}
	return out
}

func streamMemoryEntries(entries []memory.Entry) []stream.StreamMemoryPreparedEntry {
	out := make([]stream.StreamMemoryPreparedEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, stream.StreamMemoryPreparedEntry{
			Ref: e.Ref, Kind: e.Kind, Title: e.Title,
		})
	}
	return out
}

func emitSkillSelectionEvents(ctx context.Context, store domain.EventAppender, req RunnerBuildRequest, selected *SelectedSkill, matches []SkillMatch) error {
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

func emitSkillDiscovered(ctx context.Context, store domain.EventAppender, req RunnerBuildRequest, candidates []stream.StreamSkillCandidate, selected *SelectedSkill, matches []SkillMatch) error {
	discovered := &stream.StreamSkill{Candidates: candidates}
	if selected == nil {
		discovered.NoSelectionReason = deriveNoSelectionReason(req, matches)
	}
	_, err := stream.AppendStreamItem(ctx, store, req.Sink, domain.StreamItem{
		RunID:     req.RunID,
		Kind:      domain.StreamKindSkillDiscovered,
		CreatedAt: time.Now().UTC(),
		Payload:   map[string]any{"skill": discovered},
	})
	return err
}

func emitSkillSelected(ctx context.Context, store domain.EventAppender, req RunnerBuildRequest, selected *SelectedSkill, candidates []stream.StreamSkillCandidate) error {
	streamSkill := streamSkillFromSelected(selected, candidates)
	for _, kind := range []domain.StreamItemKind{domain.StreamKindSkillSelected, domain.StreamKindSkillLoaded} {
		if _, err := stream.AppendStreamItem(ctx, store, req.Sink, domain.StreamItem{
			RunID:     req.RunID,
			Kind:      kind,
			CreatedAt: time.Now().UTC(),
			Payload:   map[string]any{"skill": streamSkill},
		}); err != nil {
			return err
		}
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
			Requirements:   streamSkillRequirementsFromDomain(item.Skill.Requires),
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
		Requirements: streamSkillRequirementsFromDomain(selected.Skill.Requires),
		Score:        selected.Score,
		MatchedTerms: append([]string(nil), selected.MatchedTerms...),
	}
}

// emitDecisionBlockedEvent emits a decision_blocked stream event when the run
// selection blocks execution (missing required capability or unavailable
// explicit skill). Profile hash and intent were dropped when decision.Engine
// was inlined into run selection; only the block action/reason/explicit skill
// id remain in the payload.
func emitDecisionBlockedEvent(ctx context.Context, store domain.EventAppender, req RunnerBuildRequest, action, reason, explicitSkillID string) error {
	if store == nil || strings.TrimSpace(req.RunID) == "" {
		return nil
	}
	payload := map[string]any{
		"action":            action,
		"decision_reason":   reason,
		"explicit_skill_id": strings.TrimSpace(explicitSkillID),
	}
	_, err := stream.AppendStreamItem(ctx, store, req.Sink, domain.StreamItem{
		RunID:     req.RunID,
		Kind:      domain.StreamKindDecisionBlocked,
		CreatedAt: time.Now().UTC(),
		Payload:   payload,
	})
	return err
}
func streamSkillRequirementsFromDomain(item skills.Requirements) stream.StreamSkillRequirements {
	return stream.StreamSkillRequirements{
		Tools:    append([]string(nil), item.Tools...),
		Toolsets: append([]string(nil), item.Toolsets...),
		Bins:     append([]string(nil), item.Bins...),
		Env:      append([]string(nil), item.Env...),
	}
}
