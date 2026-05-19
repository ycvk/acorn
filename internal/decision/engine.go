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

func (e *Engine) Decide(ctx context.Context, input DecideInput) (*Record, error) {
	_ = ctx
	intent := detectIntent(input.Input)
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
	} else if route := routeForIntent(e.profile.Routes, intent); route != nil {
		if routedAction, routedSkillID, routedReason, ok := e.resolveProfileRoute(*route, input.AvailableSkills); ok {
			action = routedAction
			skillID = routedSkillID
			reason = routedReason
		} else if top, ok := topRecommendedSkill(input.AvailableSkills); ok {
			action = ActionExecuteWithSkill
			skillID = top.ID
			reason = "top_skill_recommendation"
		} else if !input.HasWorkingContext {
			action = e.defaultMissingContextAction()
			reason = "missing_context"
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
		Intent:          intent,
		SelectedSkillID: skillID,
		DecisionReason:  reason,
	}, nil
}

func (e *Engine) resolveProfileRoute(route Route, skills []RecommendedSkill) (Action, string, string, bool) {
	switch route.Action {
	case ActionExecuteWithSkill:
		skillID := strings.TrimSpace(route.SkillID)
		if skillID == "" {
			return ActionBlock, "", "profile_route_missing_skill", false
		}
		if _, ok := eligibleSkillByID(skills, skillID); !ok {
			return ActionBlock, "", "profile_route_skill_unavailable", false
		}
		return ActionExecuteWithSkill, skillID, "profile_route", true
	default:
		return route.Action, "", "profile_route", true
	}
}

func (e *Engine) defaultMissingContextAction() Action {
	if e != nil && e.profile.Defaults.MissingContext != "" {
		return e.profile.Defaults.MissingContext
	}
	return ActionInspectFirst
}

func (e *Engine) defaultMissingCapabilityAction() Action {
	if e != nil && e.profile.Defaults.MissingRequiredCapability != "" {
		return e.profile.Defaults.MissingRequiredCapability
	}
	return ActionBlock
}

func detectIntent(input string) string {
	normalized := strings.ToLower(strings.TrimSpace(input))
	switch {
	case strings.Contains(normalized, "inspect"), strings.Contains(normalized, "analyze"), strings.Contains(normalized, "read"):
		return "inspect"
	case strings.Contains(normalized, "debug"), strings.Contains(normalized, "fix"), strings.Contains(normalized, "error"):
		return "debug"
	case strings.Contains(normalized, "ship"), strings.Contains(normalized, "commit"), strings.Contains(normalized, "patch"):
		return "ship"
	default:
		return "general"
	}
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

func routeForIntent(routes []Route, intent string) *Route {
	for i := range routes {
		if strings.TrimSpace(routes[i].Intent) == strings.TrimSpace(intent) {
			return &routes[i]
		}
	}
	return nil
}
