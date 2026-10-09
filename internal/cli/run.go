package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/wire"
)

// runRun executes one owner task and prints its terminal outcome synchronously.
// It shares RunOnce and result rendering with the installation smoke command.
func runRun(ctx context.Context, args []string) error {
	fs := newFlagSet("run")
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
		return errors.New(`run input is required: acorn run "your task" or acorn run --input "..."`)
	}
	return withContainer(ctx, *configPath, func(container *wire.Container) error {
		result, err := container.RunOnce(ctx, text)
		if err != nil {
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
