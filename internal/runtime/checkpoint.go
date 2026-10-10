package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ycvk/acorn/internal/core"
)

// Checkpoints carry the visibility version of their model input. A changed
// exclusion version requires a fresh run from filtered persisted history.
type storeCheckpointStore struct {
	store      core.SessionStore
	memory     core.MemoryStore
	visibility *runMemory
}
type memoryCheckpoint struct {
	Epoch int64  `json:"epoch"`
	Data  []byte `json:"data"`
}

func (s storeCheckpointStore) Get(ctx context.Context, id string) ([]byte, bool, error) {
	data, ok, err := s.store.LoadCheckpoint(ctx, id)
	if err != nil || !ok {
		return data, ok, err
	}
	var cp memoryCheckpoint
	if err = json.Unmarshal(data, &cp); err != nil {
		return nil, false, fmt.Errorf("decode checkpoint visibility: %w", err)
	}
	ex, err := s.memory.MemoryExclusions(ctx)
	if err != nil {
		return nil, false, err
	}
	if cp.Epoch != ex.Epoch {
		return nil, false, fmt.Errorf("%w: checkpoint input version %d, current %d; start a fresh run", core.ErrMemoryExcluded, cp.Epoch, ex.Epoch)
	}
	return cp.Data, true, nil
}
func (s storeCheckpointStore) Set(ctx context.Context, id string, data []byte) error {
	ex, err := s.memory.MemoryExclusions(ctx)
	if err != nil {
		return err
	}
	if s.visibility.epoch.Load() != ex.Epoch {
		return core.ErrMemoryExcluded
	}
	encoded, err := json.Marshal(memoryCheckpoint{Epoch: ex.Epoch, Data: data})
	if err != nil {
		return err
	}
	return s.memory.SaveMemoryCheckpoint(ctx, id, encoded, ex.Epoch, s.visibility.clock())
}
