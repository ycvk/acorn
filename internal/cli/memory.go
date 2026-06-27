package cli

import (
	"context"
	"fmt"

	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/wire"
)

func runMemory(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("memory requires a subcommand: reindex")
	}
	switch args[0] {
	case "reindex":
		return runMemoryReindex(ctx, args[1:])
	default:
		return fmt.Errorf("unknown memory subcommand %q", args[0])
	}
}

// runMemoryReindex regenerates embeddings for all file-backed memory records.
// It is the one-shot path to populate the vector index after embedding is first
// enabled — the write path only embeds new/updated records, so old data would
// otherwise remain invisible to semantic search.
func runMemoryReindex(ctx context.Context, args []string) error {
	fs := newFlagSet("memory reindex")
	configPath := addConfigFlag(fs)
	jsonMode := fs.Bool("json", false, "print result as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return withContainer(ctx, *configPath, func(container *wire.Container) error {
		// Reindex is a *LocalService capability, not part of the Service
		// interface (it touches embedding internals that test stubs and
		// other consumers don't have). Use a type assertion, same pattern
		// as Container.Close's interface{ Close() error } check.
		type reindexer interface {
			ReindexEmbeddings(ctx context.Context) (*memory.ReindexResult, error)
		}
		svc := container.Memory()
		r, ok := svc.(reindexer)
		if !ok {
			return fmt.Errorf("memory service does not support reindex (is embedding enabled in config?)")
		}
		result, err := r.ReindexEmbeddings(ctx)
		if err != nil {
			return err
		}
		if *jsonMode {
			return printJSON(result)
		}
		fmt.Printf("Reindexed %d/%d records (skipped %d, failed %d)\n",
			result.Indexed, result.Total, result.Skipped, result.Failed)
		return nil
	})
}
