package decision

func IsContinuableAction(action Action) bool {
	switch action {
	case ActionExecuteWithSkill, ActionInspectFirst, ActionExecuteWithoutSkill:
		return true
	default:
		return false
	}
}

func ContextPriorityForAction(action Action) ContextPriority {
	switch action {
	case ActionExecuteWithSkill:
		return PrioritySkill
	case ActionAskUser:
		return PriorityConversation
	default:
		return PriorityBalanced
	}
}
