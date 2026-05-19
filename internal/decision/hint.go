package decision

type DecisionContextHint struct {
	ContextPriority ContextPriority
}

type ContextPriority string

const (
	PriorityBalanced     ContextPriority = "balanced"
	PrioritySkill        ContextPriority = "skill"
	PriorityConversation ContextPriority = "conversation"
)
