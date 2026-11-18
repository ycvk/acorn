package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/wire"
)

func TestRenderSmokeResultSucceeded(t *testing.T) {
	out, err := renderSmokeResult(&wire.RunOnceResult{
		RunID:  "run_123",
		Status: "succeeded",
		Output: "Hi there!",
	}, false)
	if err != nil {
		t.Fatalf("renderSmokeResult error = %v", err)
	}
	for _, want := range []string{
		"Run: run_123",
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
	out, err := renderSmokeResult(&wire.RunOnceResult{
		RunID:  "run_456",
		Status: "failed",
		Error:  "semantic search runtime is required",
	}, false)
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
	out, err := renderSmokeResult(&wire.RunOnceResult{
		RunID:  "run_789",
		Status: "failed",
		Error:  "execution_not_ready: model.api_key is required",
	}, true)
	if err != nil {
		t.Fatalf("renderSmokeResult json error = %v", err)
	}
	for _, want := range []string{
		`"run_id": "run_789"`,
		`"status": "failed"`,
		`"error": "execution_not_ready: model.api_key is required"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderSmokeResult json missing %q:\n%s", want, out)
		}
	}
}

func TestSmokeRunErrorExitSemantics(t *testing.T) {
	if err := smokeRunError("smoke", &wire.RunOnceResult{RunID: "run_1", Status: "succeeded", Output: "hi"}); err != nil {
		t.Fatalf("succeeded must be a clean success (nil error), got: %v", err)
	}
	failedErr := smokeRunError("smoke", &wire.RunOnceResult{RunID: "run_2", Status: "failed", Error: "boom"})
	if failedErr == nil || !strings.Contains(failedErr.Error(), "failed") || !strings.Contains(failedErr.Error(), "boom") {
		t.Fatalf("failed must return a non-nil error carrying the reason, got: %v", failedErr)
	}
	// interrupted is terminal-but-incomplete: it must NOT exit 0 like a success.
	interruptedErr := smokeRunError("smoke", &wire.RunOnceResult{RunID: "run_3", Status: "interrupted"})
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

// acorn run, like acorn smoke, prints a failure object in --json mode when
// the run cannot start.
func TestRunAndSmokePrintJSONWhenTheRunCannotStart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	path := filepath.Join(dir, "acorn.yaml")
	// No provider has a key, so execution is not ready.
	body := "runtime:\n  storage_dir: " + filepath.Join(dir, "state") + "\nmemory:\n  embedding:\n    api_key: fixture\n    base_url: http://127.0.0.1:1\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"run", "smoke"} {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		saved := os.Stdout
		os.Stdout = w
		runErr := Run(context.Background(), []string{name, "-c", path, "--json", "在吗"})
		os.Stdout = saved
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		out, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		var got smokeCommandOutput
		if runErr == nil || json.Unmarshal(out, &got) != nil || got.Status != "failed" || got.Error == "" {
			t.Fatalf("%s --json = %q, err %v", name, out, runErr)
		}
	}
}
