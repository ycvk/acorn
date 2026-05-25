package decision

import "testing"

func TestContextPriorityForAction(t *testing.T) {
	tests := []struct {
		name   string
		action Action
		want   ContextPriority
	}{
		{name: "inspect first", action: ActionInspectFirst, want: PriorityBalanced},
		{name: "execute with skill", action: ActionExecuteWithSkill, want: PrioritySkill},
		{name: "ask user", action: ActionAskUser, want: PriorityConversation},
		{name: "execute without skill", action: ActionExecuteWithoutSkill, want: PriorityBalanced},
		{name: "resume run", action: ActionResumeRun, want: PriorityBalanced},
		{name: "block", action: ActionBlock, want: PriorityBalanced},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContextPriorityForAction(tt.action); got != tt.want {
				t.Fatalf("ContextPriorityForAction(%q) = %q, want %q", tt.action, got, tt.want)
			}
		})
	}
}

func TestIsContinuableAction(t *testing.T) {
	for _, action := range []Action{ActionExecuteWithSkill, ActionInspectFirst, ActionExecuteWithoutSkill} {
		if !IsContinuableAction(action) {
			t.Fatalf("expected %q to be continuable", action)
		}
	}
	for _, action := range []Action{ActionAskUser, ActionBlock, ActionResumeRun} {
		if IsContinuableAction(action) {
			t.Fatalf("expected %q to not be continuable", action)
		}
	}
}
