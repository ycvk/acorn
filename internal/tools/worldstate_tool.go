package tools

import (
	"context"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
)

// WorldStateDelta is the structured change contract for WorldState updates.
// Defined here (not imported from internal/memory) so this package has no
// dependency on the memory package — the WorldStateUpdater interface bridges.
type WorldStateDelta struct {
	Upserts map[string]string `json:"upserts,omitempty"`
	Deletes []string          `json:"deletes,omitempty"`
}

// WorldStateUpdater is the interface for reading and writing the agent's
// cross-run WorldState projection. Implemented by memory.WorldState via an
// adapter in wire/container.go.
type WorldStateUpdater interface {
	ApplyDelta(ctx context.Context, delta WorldStateDelta) error
	Load(ctx context.Context) (map[string]string, error)
}

// WorldStateUpdateInput is the tool input for worldstate_update.
type WorldStateUpdateInput struct {
	Upserts map[string]string `json:"upserts,omitempty" jsonschema:"description=Key-value pairs to set or replace in the world state."`
	Deletes []string          `json:"deletes,omitempty" jsonschema:"description=Keys to remove from the world state."`
}

// WorldStateUpdateOutput is the tool result.
type WorldStateUpdateOutput struct {
	Updated int `json:"updated"`
}

// WorldStateLoadInput is the tool input for worldstate_load.
type WorldStateLoadInput struct{}

// WorldStateLoadOutput is the tool result.
type WorldStateLoadOutput struct {
	State map[string]string `json:"state"`
}

func buildWorldStateUpdateTool(updater WorldStateUpdater) (einotool.BaseTool, error) {
	if updater == nil {
		return nil, fmt.Errorf("worldstate_update requires a world state updater")
	}
	tool, err := inferProgressTool("worldstate_update", "Update the agent's cross-run world state projection. Use upserts to set or replace keys, deletes to remove them. This state persists across runs and is injected into trigger-fired runs.", func(ctx context.Context, input WorldStateUpdateInput, emit ToolProgressEmitter) (WorldStateUpdateOutput, error) {
		if len(input.Upserts) == 0 && len(input.Deletes) == 0 {
			return WorldStateUpdateOutput{}, fmt.Errorf("worldstate_update requires at least one upsert or delete")
		}
		upserts := make(map[string]string, len(input.Upserts))
		for k, v := range input.Upserts {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			upserts[k] = v
		}
		var deletes []string
		for _, k := range input.Deletes {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			deletes = append(deletes, k)
		}
		if len(upserts) == 0 && len(deletes) == 0 {
			return WorldStateUpdateOutput{}, fmt.Errorf("worldstate_update requires at least one non-empty key")
		}
		if err := updater.ApplyDelta(ctx, WorldStateDelta{Upserts: upserts, Deletes: deletes}); err != nil {
			return WorldStateUpdateOutput{}, fmt.Errorf("worldstate_update: %w", err)
		}
		return WorldStateUpdateOutput{Updated: len(upserts) + len(deletes)}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build worldstate_update tool: %w", err)
	}
	return tool, nil
}

func buildWorldStateLoadTool(updater WorldStateUpdater) (einotool.BaseTool, error) {
	if updater == nil {
		return nil, fmt.Errorf("worldstate_load requires a world state updater")
	}
	tool, err := inferProgressTool("worldstate_load", "Load the current world state projection. Returns all key-value pairs that persist across runs and are injected into trigger-fired runs.", func(ctx context.Context, _ WorldStateLoadInput, emit ToolProgressEmitter) (WorldStateLoadOutput, error) {
		state, err := updater.Load(ctx)
		if err != nil {
			return WorldStateLoadOutput{}, fmt.Errorf("worldstate_load: %w", err)
		}
		return WorldStateLoadOutput{State: state}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build worldstate_load tool: %w", err)
	}
	return tool, nil
}
