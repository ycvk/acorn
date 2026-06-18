package decision

func IsContinuableAction(action Action) bool {
	switch action {
	case ActionExecuteWithSkill, ActionInspectFirst, ActionExecuteWithoutSkill:
		return true
	default:
		return false
	}
}
