package memory

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// WorldState is a file-backed key-value store that serves as the agent's
// cross-run decision-relevant projection. It is the ambient memory layer
// between Session (per-run, temporary) and facts (explicit, durable): trigger
// fires produce deltas that accumulate here, so the agent reads a consistent
// world view instead of re-perceiving from zero each run.
//
// Only ApplyDelta mutates state. Load returns a snapshot. The in-memory cache
// avoids reading the file on every Load; the mutex serializes writes so
// concurrent trigger fires do not corrupt the file.
//
// See docs/adr/0001-ambient-agent-direction.md §WorldState.
type WorldState struct {
	dir   string
	mu    sync.Mutex
	cache map[string]string
}

// NewWorldState opens or creates a WorldState rooted at dir. The state file
// is {dir}/state.json; it is created lazily on the first ApplyDelta.
func NewWorldState(dir string) (*WorldState, error) {
	if dir == "" {
		return nil, errors.New("world state directory is required")
	}
	ws := &WorldState{dir: dir, cache: make(map[string]string)}
	// Eagerly load if the file exists so Load is consistent.
	if err := ws.loadFromFile(); err != nil {
		return nil, err
	}
	return ws, nil
}

// WorldStateDelta is the structured change contract. Upserts set or replace
// keys; Deletes remove them. Only ApplyDelta applies a delta.
type WorldStateDelta struct {
	Upserts map[string]string
	Deletes []string
}

// Load returns a copy of the current world state projection.
func (w *WorldState) Load(_ context.Context) (map[string]string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make(map[string]string, len(w.cache))
	for k, v := range w.cache {
		out[k] = v
	}
	return out, nil
}

// ApplyDelta applies upserts and deletes to the world state, then persists
// to disk. An empty delta is a no-op (no file write).
func (w *WorldState) ApplyDelta(_ context.Context, delta WorldStateDelta) error {
	if len(delta.Upserts) == 0 && len(delta.Deletes) == 0 {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	for k, v := range delta.Upserts {
		w.cache[k] = v
	}
	for _, k := range delta.Deletes {
		delete(w.cache, k)
	}

	return w.persistLocked()
}

// loadFromFile reads state.json into the cache if it exists.
func (w *WorldState) loadFromFile() error {
	path := filepath.Join(w.dir, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no file yet — empty state
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, &w.cache)
}

// persistLocked writes the cache to state.json atomically. Caller must hold mu.
func (w *WorldState) persistLocked() error {
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(w.cache, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(w.dir, "state.json.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(w.dir, "state.json"))
}
