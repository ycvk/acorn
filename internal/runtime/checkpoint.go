package runtime

import (
	"context"

	"github.com/ycvk/acorn/internal/core"
)

// storeCheckpointStore persists adk checkpoints through the session store so
// an interrupted run can resume in a fresh runner or a restarted process.
type storeCheckpointStore struct {
	store core.SessionStore
}

func (s storeCheckpointStore) Get(ctx context.Context, checkpointID string) ([]byte, bool, error) {
	return s.store.LoadCheckpoint(ctx, checkpointID)
}

func (s storeCheckpointStore) Set(ctx context.Context, checkpointID string, data []byte) error {
	return s.store.SaveCheckpoint(ctx, checkpointID, data)
}
