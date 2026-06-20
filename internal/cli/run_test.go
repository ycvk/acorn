package cli

import (
	"context"
	"strings"
	"testing"
)

// T-001 RED: `acorn run` must exist as a user-facing one-shot run command
// (direct_response by default, synchronous output). These assert structural
// existence only — a full turn requires a configured provider and is verified
// as an integration check, not a unit test.

func TestRunCommandInUsage(t *testing.T) {
	body := usageText()
	if !strings.Contains(body, "acorn run ") {
		t.Fatalf("usageText should advertise `acorn run`, got:\n%s", body)
	}
}

func TestRunCommandDispatchesNotUnknown(t *testing.T) {
	err := Run(context.Background(), []string{"run"})
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("`acorn run` must dispatch to a run handler, not return unknown-command error; got: %v", err)
	}
}
