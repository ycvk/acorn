package events

import "testing"

func TestPendingActionKindElicitationValue(t *testing.T) {
	if PendingActionKindElicitation != "elicitation" {
		t.Errorf("PendingActionKindElicitation = %q, want %q", PendingActionKindElicitation, "elicitation")
	}
	if PendingActionKindOperatorQuestion != "operator_question" {
		t.Errorf("PendingActionKindOperatorQuestion = %q, want %q", PendingActionKindOperatorQuestion, "operator_question")
	}
}
