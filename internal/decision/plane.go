package decision

import (
	"context"

	einomodel "github.com/cloudwego/eino/components/model"
)

type Plane interface {
	Decide(ctx context.Context, req Request) (*Result, error)
	HandleAction(ctx context.Context, req ActionRequest) error
}

type Request struct {
	RunID             string
	SessionID         string
	Input             string
	ExplicitSkillID   string
	HasWorkingContext bool
	AvailableSkills   []RecommendedSkill
}

type Result struct {
	Record        *Record
	SelectedSkill *SelectedSkillResult
	Hint          *DecisionContextHint
}

type SelectedSkillResult struct {
	SkillID      string
	SkillName    string
	Score        int
	MatchedTerms []string
	Explicit     bool
}

type ActionRequest struct {
	RunID     string
	SessionID string
	Input     string
	Result    *Result
	ChatModel einomodel.BaseChatModel
	Skills    []string
}

func IsContinuableAction(action Action) bool {
	switch action {
	case ActionExecuteWithSkill, ActionInspectFirst, ActionExecuteWithoutSkill:
		return true
	default:
		return false
	}
}

func BuildHint(action Action) *DecisionContextHint {
	switch action {
	case ActionInspectFirst:
		return &DecisionContextHint{
			ContextPriority: PriorityBalanced,
		}
	case ActionExecuteWithSkill:
		return &DecisionContextHint{
			ContextPriority: PrioritySkill,
		}
	case ActionAskUser:
		return &DecisionContextHint{
			ContextPriority: PriorityConversation,
		}
	case ActionExecuteWithoutSkill, ActionResumeRun:
		return &DecisionContextHint{
			ContextPriority: PriorityBalanced,
		}
	default:
		return &DecisionContextHint{
			ContextPriority: PriorityBalanced,
		}
	}
}
