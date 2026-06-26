package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/wire"
)

// runSmoke runs a single owner-local smoke task through the real runtime and
// prints its terminal outcome. It is a non-interactive, one-shot operator probe
// (sibling of `acorn doctor`), not a terminal chat surface: it answers "does my
// install actually execute a run, and if not, what is the real error" without
// needing a paired mobile device.
func runSmoke(ctx context.Context, args []string) error {
	fs := newFlagSet("smoke")
	configPath := addConfigFlag(fs)
	input := fs.String("input", "", "task input to send (or pass as positional args)")
	jsonMode := fs.Bool("json", false, "print the run result as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	text := strings.TrimSpace(*input)
	if text == "" {
		text = strings.TrimSpace(strings.Join(fs.Args(), " "))
	}
	if text == "" {
		return errors.New(`smoke input is required: acorn smoke "your task" or acorn smoke --input "..."`)
	}
	return withContainer(ctx, *configPath, func(container *wire.Container) error {
		result, err := container.RunOnce(ctx, text)
		if err != nil {
			// In --json mode the run could not start (e.g. execution_not_ready); a
			// scripted consumer still expects parseable JSON on stdout, not just a
			// stderr string. Emit a failed-status object, then return the error so
			// the exit code stays non-zero.
			if *jsonMode {
				if body, mErr := json.MarshalIndent(smokeCommandOutput{
					Status: "failed",
					Error:  err.Error(),
				}, "", "  "); mErr == nil {
					fmt.Println(string(body))
				}
			}
			return err
		}
		out, err := renderSmokeResult(result, *jsonMode)
		if err != nil {
			return err
		}
		fmt.Print(out)
		return smokeRunError(result)
	})
}

// smokeRunError reports whether a smoke run completed cleanly. Only "succeeded"
// is a clean success (exit 0). "failed" carries the failure reason; "interrupted"
// (and any other non-succeeded terminal status) means the run did NOT complete —
// it must not masquerade as success via a zero exit code, since operators and
// scripts rely on the exit code to gate "did my install actually run a task".
func smokeRunError(result *wire.RunOnceResult) error {
	switch result.Status {
	case "succeeded":
		return nil
	case "failed":
		return fmt.Errorf("smoke run %s failed: %s", result.RunID, skills.FirstNonEmpty(result.Error, "no error detail recorded"))
	default:
		return fmt.Errorf("smoke run %s did not complete (status=%s)", result.RunID, result.Status)
	}
}

type smokeCommandOutput struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
	Mode   string `json:"mode"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// renderSmokeResult renders a smoke run outcome as human text or JSON. It is a
// pure function so the formatting is unit-testable without a live runtime.
func renderSmokeResult(result *wire.RunOnceResult, jsonMode bool) (string, error) {
	if result == nil {
		return "", errors.New("smoke result is nil")
	}
	if jsonMode {
		body, err := json.MarshalIndent(smokeCommandOutput{
			RunID:  result.RunID,
			Status: result.Status,
			Mode:   "direct_response",
			Output: result.Output,
			Error:  result.Error,
		}, "", "  ")
		if err != nil {
			return "", fmt.Errorf("encode smoke result: %w", err)
		}
		return string(body) + "\n", nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Run: %s  (mode=direct_response)\n", result.RunID)
	fmt.Fprintf(&b, "Status: %s\n", result.Status)
	if strings.TrimSpace(result.Output) != "" {
		fmt.Fprintf(&b, "Output:\n%s\n", result.Output)
	}
	if strings.TrimSpace(result.Error) != "" {
		fmt.Fprintf(&b, "Error: %s\n", result.Error)
	}
	return b.String(), nil
}
