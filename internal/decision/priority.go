package decision

type ContextPriority string

const (
	PriorityBalanced     ContextPriority = "balanced"
	PrioritySkill        ContextPriority = "skill"
	PriorityConversation ContextPriority = "conversation"
)
