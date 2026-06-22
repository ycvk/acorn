package storetest

import (
	"context"
	"sort"
	"sync"

	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/workingstate"
)

// FakeContextStore is a minimal in-memory implementation of the interfaces
// needed by contextplane tests, avoiding a dependency on store/sqlite.
type FakeContextStore struct {
	mu          sync.RWMutex
	snapshots   map[string]model.RunContextSnapshot
	boundaries  map[string]model.ContextBoundary
	checkpoints map[string]workingstate.Checkpoint
}

func NewFakeContextStore() *FakeContextStore {
	return &FakeContextStore{
		snapshots:   make(map[string]model.RunContextSnapshot),
		boundaries:  make(map[string]model.ContextBoundary),
		checkpoints: make(map[string]workingstate.Checkpoint),
	}
}

// RunContextSnapshotStore implementation

func (s *FakeContextStore) SaveRunContextSnapshot(_ context.Context, snap model.RunContextSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots[snap.RunID] = snap
	return nil
}

func (s *FakeContextStore) LoadRunContextSnapshot(_ context.Context, runID string) (*model.RunContextSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap, ok := s.snapshots[runID]
	if !ok {
		return nil, nil
	}
	return &snap, nil
}

// ContextBoundaryStore implementation

func (s *FakeContextStore) SaveContextBoundary(_ context.Context, boundary model.ContextBoundary) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.boundaries[boundary.BoundaryID] = boundary
	return nil
}

func (s *FakeContextStore) LoadContextBoundary(_ context.Context, boundaryID string) (*model.ContextBoundary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	boundary, ok := s.boundaries[boundaryID]
	if !ok {
		return nil, nil
	}
	return &boundary, nil
}

func (s *FakeContextStore) LoadLatestContextBoundary(ctx context.Context, sessionID string) (*model.ContextBoundary, error) {
	boundaries, err := s.ListContextBoundaries(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if len(boundaries) == 0 {
		return nil, nil
	}
	latest := boundaries[len(boundaries)-1]
	return &latest, nil
}

func (s *FakeContextStore) ListContextBoundaries(_ context.Context, sessionID string) ([]model.ContextBoundary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []model.ContextBoundary
	for _, boundary := range s.boundaries {
		if boundary.SessionID == sessionID {
			out = append(out, boundary)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sequence == out[j].Sequence {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].Sequence < out[j].Sequence
	})
	return out, nil
}

// workingstate.Store implementation

func (s *FakeContextStore) GetWorkingCheckpoint(_ context.Context, threadID string) (*workingstate.Checkpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp, ok := s.checkpoints[threadID]
	if !ok {
		return nil, nil
	}
	return &cp, nil
}

func (s *FakeContextStore) UpsertWorkingCheckpoint(_ context.Context, cp workingstate.Checkpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkpoints[cp.ThreadID] = cp
	return nil
}

func (s *FakeContextStore) DeleteWorkingCheckpoint(_ context.Context, threadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.checkpoints, threadID)
	return nil
}
