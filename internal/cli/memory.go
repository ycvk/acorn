package cli

import (
	"context"
	"fmt"
)

func runMemory(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("memory requires a subcommand")
	}
	return fmt.Errorf("unknown memory subcommand %q", args[0])
}
