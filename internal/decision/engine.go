package decision

import (
	"context"
	"strings"
)

type Engine struct {
	profile Profile
}

func NewEngine(profile Profile) *Engine {
	return &Engine{profile: profile}
}

// Decide resolves a run's decision action from capability state, an explicit
// skill, or the top recommended skill. It never classifies intent by substring
// nor routes by intent (P0-C demotion). Intent is always "general".
func (e *Engine) Decide(ctx context.Context, input DecideInput) (*Record, error) {
	_ = ctx
	action := ActionExecuteWithoutSkill
	skillID := ""
	reason := "general_execution"

	if hasCapabilityFailure(input.AvailableSkills) {
		action = e.defaultMissingCapabilityAction()
		reason = "missing_required_capability"
	} else if explicitID := strings.TrimSpace(input.ExplicitSkillID); explicitID != "" {
		if _, ok := eligibleSkillByID(input.AvailableSkills, explicitID); ok {
			action = ActionExecuteWithSkill
			skillID = explicitID
			reason = "explicit_skill"
		} else {
			action = ActionBlock
			reason = "explicit_skill_unavailable"
		}
	} else if top, ok := topRecommendedSkill(input.AvailableSkills); ok {
		action = ActionExecuteWithSkill
		skillID = top.ID
		reason = "top_skill_recommendation"
	} else if !input.HasWorkingContext {
		action = e.defaultMissingContextAction()
		reason = "missing_context"
	}

	return &Record{
		RunID:           strings.TrimSpace(input.RunID),
		SessionID:       strings.TrimSpace(input.SessionID),
		Action:          action,
		Intent:          "general",
		SelectedSkillID: skillID,
		DecisionReason:  reason,
	}, nil
}

func (e *Engine) defaultMissingContextAction() string {
	if e != nil && e.profile.Defaults.MissingContext != "" {
		return e.profile.Defaults.MissingContext
	}
	return ActionInspectFirst
}

func (e *Engine) defaultMissingCapabilityAction() string {
	if e != nil && e.profile.Defaults.MissingRequiredCapability != "" {
		return e.profile.Defaults.MissingRequiredCapability
	}
	return ActionBlock
}

func topRecommendedSkill(items []RecommendedSkill) (RecommendedSkill, bool) {
	var best RecommendedSkill
	for _, item := range items {
		if !isEligibleRecommendedSkill(item) {
			continue
		}
		if best.ID == "" ||
			item.Score > best.Score ||
			(item.Score == best.Score && item.ID < best.ID) {
			best = item
		}
	}
	if best.ID == "" {
		return RecommendedSkill{}, false
	}
	return best, true
}

func eligibleSkillByID(items []RecommendedSkill, skillID string) (RecommendedSkill, bool) {
	normalizedID := strings.TrimSpace(skillID)
	if normalizedID == "" {
		return RecommendedSkill{}, false
	}
	for _, item := range items {
		if strings.TrimSpace(item.ID) == normalizedID && isEligibleRecommendedSkill(item) {
			return item, true
		}
	}
	return RecommendedSkill{}, false
}

func isEligibleRecommendedSkill(item RecommendedSkill) bool {
	return strings.TrimSpace(item.ID) != "" &&
		item.FilteredReason == "" &&
		item.TriggerMatched &&
		item.Score > 0
}

func hasCapabilityFailure(items []RecommendedSkill) bool {
	for _, item := range items {
		if strings.Contains(item.FilteredReason, "missing_required_") {
			return true
		}
	}
	return false
}
