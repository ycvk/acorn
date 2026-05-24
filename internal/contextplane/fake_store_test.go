package contextplane

import (
	"context"
	"sync"

	"github.com/ycvk/acorn/internal/runtimehistory"
	"github.com/ycvk/acorn/internal/toolresult"
	"github.com/ycvk/acorn/internal/workingstate"
)

// fakeContextStore is a minimal in-memory implementation of the interfaces
// needed by contextplane tests, avoiding a dependency on store/sqlite.
type fakeContextStore struct {
	mu          sync.RWMutex
	snapshots   map[string]runtimehistory.RunContextSnapshot
	checkpoints map[string]workingstate.Checkpoint
	results     map[string]toolresult.Record
}

func newFakeContextStore() *fakeContextStore {
	return &fakeContextStore{
		snapshots:   make(map[string]runtimehistory.RunContextSnapshot),
		checkpoints: make(map[string]workingstate.Checkpoint),
		results:     make(map[string]toolresult.Record),
	}
}

// RunContextSnapshotStore implementation
func (s *fakeContextStore) SaveRunContextSnapshot(_ context.Context, snap runtimehistory.RunContextSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots[snap.RunID] = snap
	return nil
}

func (s *fakeContextStore) LoadRunContextSnapshot(_ context.Context, runID string) (*runtimehistory.RunContextSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap, ok := s.snapshots[runID]
	if !ok {
		return nil, nil
	}
	return &snap, nil
}

// workingstate.Store implementation
func (s *fakeContextStore) GetWorkingCheckpoint(_ context.Context, threadID string) (*workingstate.Checkpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp, ok := s.checkpoints[threadID]
	if !ok {
		return nil, nil
	}
	return &cp, nil
}

func (s *fakeContextStore) UpsertWorkingCheckpoint(_ context.Context, cp workingstate.Checkpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkpoints[cp.ThreadID] = cp
	return nil
}

func (s *fakeContextStore) DeleteWorkingCheckpoint(_ context.Context, threadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.checkpoints, threadID)
	return nil
}

// toolresult.Ledger implementation
func (s *fakeContextStore) Append(_ context.Context, req toolresult.AppendRequest) (toolresult.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ref := toolresult.BuildRef(req.RunID, req.CallID)
	rec := toolresult.Record{
		ResultRef:     ref,
		RunID:         req.RunID,
		SessionID:     req.SessionID,
		CallID:        req.CallID,
		ToolName:      req.ToolName,
		ArgumentsJSON: req.ArgumentsJSON,
		Status:        req.Status,
		ErrorReason:   req.ErrorReason,
		FullText:      req.FullText,
		Preview:       toolresult.Preview(req.FullText, 0),
		TokenEstimate: req.TokenEstimate,
		SideEffects:   req.SideEffects,
		EvidenceRefs:  req.EvidenceRefs,
		CreatedAt:     req.CreatedAt,
	}
	s.results[ref] = rec
	return rec, nil
}

func (s *fakeContextStore) Load(_ context.Context, ref string) (toolresult.Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.results[ref]
	if !ok {
		return toolresult.Record{}, toolresult.ErrToolResultNotFound
	}
	return rec, nil
}

func (s *fakeContextStore) ListByRun(_ context.Context, runID string) ([]toolresult.Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []toolresult.Record
	for _, rec := range s.results {
		if rec.RunID == runID {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (s *fakeContextStore) AppendEvidenceRef(_ context.Context, ref string, ev toolresult.EvidenceRef) (toolresult.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.results[ref]
	if !ok {
		return toolresult.Record{}, toolresult.ErrToolResultNotFound
	}
	rec.EvidenceRefs = append(rec.EvidenceRefs, ev)
	s.results[ref] = rec
	return rec, nil
}
