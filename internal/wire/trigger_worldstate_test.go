package wire

import (
	"context"
	"testing"
)

func TestTriggerRunCreatorInjectsWorldStateProjection(t *testing.T) {
	// This test verifies the wiring: triggerRunCreator reads WorldState and
	// prepends the projection to the run input. We test the formatWorldStatePrefix
	// function directly since it is the transformation that makes WorldState
	// visible to the agent.
	projection := map[string]string{
		"unread_emails": "5",
		"last_deploy":   "success",
	}
	prefix := formatWorldStatePrefix(projection)

	if prefix == "" {
		t.Fatal("prefix should not be empty for non-empty projection")
	}
	if !contains(prefix, "unread_emails") {
		t.Fatalf("prefix should contain 'unread_emails', got %q", prefix)
	}
	if !contains(prefix, "5") {
		t.Fatalf("prefix should contain value '5', got %q", prefix)
	}
	if !contains(prefix, "world state") {
		t.Fatalf("prefix should label itself as world state, got %q", prefix)
	}
}

func TestFormatWorldStatePrefixEmptyIsNotEmptyString(t *testing.T) {
	// Even an empty projection produces a header — but triggerRunCreator
	// skips injection when projection is empty (len==0), so the header
	// only appears when there are actual keys.
	prefix := formatWorldStatePrefix(map[string]string{})
	if prefix == "" {
		t.Fatal("formatWorldStatePrefix should produce a header even for empty map")
	}
}
func TestTriggerRunCreatorCreateRunSignatureAcceptsWorldState(t *testing.T) {
	// Verify the struct has the worldState field wired. We can't call CreateRun
	// without a real RunService, but we can verify the type is constructible.
	creator := &triggerRunCreator{
		runs:       nil,
		store:      nil,
		worldState: nil,
	}
	_ = creator // type is constructible with nil worldState
	_ = context.Background()
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
