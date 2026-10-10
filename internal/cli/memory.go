package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/ycvk/acorn/internal/wire"
)

func runMemory(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "reindex" {
		return errors.New("usage: acorn memory reindex [-c path] [--json]")
	}
	fs := newFlagSet("memory reindex")
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
