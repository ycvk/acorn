package cli

import (
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/app"
)

func TestRenderSmokeResultSucceeded(t *testing.T) {
	out, err := renderSmokeResult(&app.RunOnceResult{
		RunID:  "run_123",
		Status: "succeeded",
		Output: "Hi there!",
	}, "direct_response", false)
	if err != nil {
		t.Fatalf("renderSmokeResult error = %v", err)
	}
	for _, want := range []string{
		"Run: run_123  (mode=direct_response)",
		"Status: succeeded",
		"Output:\nHi there!",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderSmokeResult missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Error:") {
		t.Fatalf("succeeded result should not print an error line:\n%s", out)
	}
}

func TestRenderSmokeResultFailedSurfacesError(t *testing.T) {
	out, err := renderSmokeResult(&app.RunOnceResult{
		RunID:  "run_456",
		Status: "failed",
		Error:  "semantic search runtime is required",
	}, "direct_response", false)
	if err != nil {
		t.Fatalf("renderSmokeResult error = %v", err)
	}
	for _, want := range []string{
		"Status: failed",
		"Error: semantic search runtime is required",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderSmokeResult missing %q:\n%s", want, out)
		}
	}
}

func TestRenderSmokeResultJSON(t *testing.T) {
	out, err := renderSmokeResult(&app.RunOnceResult{
		RunID:  "run_789",
		Status: "failed",
		Error:  "execution_not_ready: model.api_key is required",
	}, "plan_execute", true)
	if err != nil {
		t.Fatalf("renderSmokeResult json error = %v", err)
	}
	for _, want := range []string{
		`"run_id": "run_789"`,
		`"status": "failed"`,
		`"mode": "plan_execute"`,
		`"error": "execution_not_ready: model.api_key is required"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderSmokeResult json missing %q:\n%s", want, out)
		}
	}
}

func TestSmokeRunErrorExitSemantics(t *testing.T) {
	if err := smokeRunError(&app.RunOnceResult{RunID: "run_1", Status: "succeeded", Output: "hi"}); err != nil {
		t.Fatalf("succeeded must be a clean success (nil error), got: %v", err)
	}
	failedErr := smokeRunError(&app.RunOnceResult{RunID: "run_2", Status: "failed", Error: "boom"})
	if failedErr == nil || !strings.Contains(failedErr.Error(), "failed") || !strings.Contains(failedErr.Error(), "boom") {
		t.Fatalf("failed must return a non-nil error carrying the reason, got: %v", failedErr)
	}
	// interrupted is terminal-but-incomplete: it must NOT exit 0 like a success.
	interruptedErr := smokeRunError(&app.RunOnceResult{RunID: "run_3", Status: "interrupted"})
	if interruptedErr == nil || !strings.Contains(interruptedErr.Error(), "did not complete") {
		t.Fatalf("interrupted must return a non-nil error, got: %v", interruptedErr)
	}
}

func TestUsageIncludesSmoke(t *testing.T) {
	body := usageText()
	if !strings.Contains(body, "acorn smoke [-c path]") {
		t.Fatalf("usageText should advertise acorn smoke, got:\n%s", body)
	}
}
