package decision

import "testing"

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
