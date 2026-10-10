package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/ycvk/acorn/internal/wire"
)

func runMemory(ctx context.Context, args []string) error {
	if len(args) == 0 || (args[0] != "reindex" && args[0] != "preflight") {
		return errors.New("usage: acorn memory {preflight|reindex} [-c path] [--json]")
	}
	fs := newFlagSet("memory " + args[0])
	configPath := addConfigFlag(fs)
	jsonMode := fs.Bool("json", false, "print memory maintenance report")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("memory maintenance takes no positional arguments")
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	if args[0] == "preflight" {
		report, err := wire.PreflightMemory(ctx, cfg)
		if err != nil {
			return err
		}
		if *jsonMode {
			if err := printJSON(report); err != nil {
				return err
			}
		} else {
			fmt.Printf("Memory migration: ready=%t schema=%s integrity=%s active=%d interrupted=%d pending_actions=%d legacy=%d\n", report.Ready, report.Schema, report.Integrity, report.ActiveRuns, report.InterruptedRuns, report.PendingActions, report.LegacyEntries)
			if report.Reason != "" {
				fmt.Println(report.Reason)
			}
		}
		if !report.Ready {
			return errors.New("memory migration preflight failed")
		}
		return nil
	}
	index, err := wire.ReindexMemory(ctx, cfg)
	if err != nil {
		return err
	}
	if *jsonMode {
		return printJSON(index)
	}
	fmt.Printf("Memory index ready: model=%s dimensions=%d generation=%d\n", index.Model, index.Dimensions, index.Generation)
	return nil
}
