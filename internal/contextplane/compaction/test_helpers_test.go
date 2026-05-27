package compaction

import (
	"context"
	"sort"
	"sync"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/localit-io/tiktoken-go"
	tiktokenloader "github.com/pkoukk/tiktoken-go-loader"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/workingstate"
)

type testCompactionEngine struct {
	called  bool
	request CompactRequest
	result  *CompactionResult
	err     error
}

func (e *testCompactionEngine) Compact(_ context.Context, req CompactRequest) (*CompactionResult, error) {
	e.called = true
	e.request = req
	if e.err != nil {
		return nil, e.err
	}
	return e.result, nil
}

type testBudgetGovernor struct {
	pressure contextplane.BudgetPressure
	dynamic  bool
}

func (g testBudgetGovernor) Evaluate(_ context.Context, req contextplane.BudgetEvaluateRequest) (contextplane.BudgetPressure, error) {
	if g.dynamic && len(req.Messages) <= 3 {
		p := g.pressure
		p.State = contextplane.PressureOK
		return p, nil
	}
	return g.pressure, nil
}

func (g testBudgetGovernor) AutoCompactThreshold(contextplane.ModelProfile) (int, error) {
	return g.pressure.AutoCompactThresholdTokens, nil
}

func testPressure(state contextplane.BudgetPressureState) contextplane.BudgetPressure {
	return contextplane.BudgetPressure{
		EstimatedInputTokens:       100,
		EffectiveWindowTokens:      1000,
		WarningThresholdTokens:     800,
		AutoCompactThresholdTokens: 900,
		BlockingThresholdTokens:    990,
		PercentUsed:                10,
		State:                      state,
	}
}

func testTokenCounter(t *testing.T) *contextplane.CompressionTokenCounter {
	t.Helper()
	counter, err := contextplane.NewCompressionTokenCounter(config.ContextConfig{TokenEncoding: "o200k_base"})
	if err != nil {
		t.Fatalf("NewCompressionTokenCounter: %v", err)
	}
	return counter
}

func testContextSessionProfile() contextplane.ModelProfile {
	return contextplane.ModelProfile{
		ContextWindowTokens:         200000,
		ReservedOutputTokens:        4096,
		ReservedSummaryOutputTokens: 2048,
		StaticOverheadTokens:        4096,
		WarningBufferTokens:         20000,
		AutoCompactBufferTokens:     13000,
		BlockingBufferTokens:        3000,
	}
}

// fakeContextStore is a minimal in-memory implementation of the interfaces
// needed by compaction tests, avoiding a dependency on store/sqlite.
type fakeContextStore struct {
	mu          sync.RWMutex
	snapshots   map[string]model.RunContextSnapshot
	boundaries  map[string]model.ContextBoundary
	checkpoints map[string]workingstate.Checkpoint
	results     map[string]store.ToolResultRecord
}

func newFakeContextStore() *fakeContextStore {
	return &fakeContextStore{
		snapshots:   make(map[string]model.RunContextSnapshot),
		boundaries:  make(map[string]model.ContextBoundary),
		checkpoints: make(map[string]workingstate.Checkpoint),
		results:     make(map[string]store.ToolResultRecord),
	}
}

func (s *fakeContextStore) SaveRunContextSnapshot(_ context.Context, snap model.RunContextSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots[snap.RunID] = snap
	return nil
}

func (s *fakeContextStore) LoadRunContextSnapshot(_ context.Context, runID string) (*model.RunContextSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap, ok := s.snapshots[runID]
	if !ok {
		return nil, nil
	}
	return &snap, nil
}

func (s *fakeContextStore) SaveContextBoundary(_ context.Context, boundary model.ContextBoundary) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.boundaries[boundary.BoundaryID] = boundary
	return nil
}

func (s *fakeContextStore) LoadContextBoundary(_ context.Context, boundaryID string) (*model.ContextBoundary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	boundary, ok := s.boundaries[boundaryID]
	if !ok {
		return nil, nil
	}
	return &boundary, nil
}

func (s *fakeContextStore) LoadLatestContextBoundary(ctx context.Context, sessionID string) (*model.ContextBoundary, error) {
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

func (s *fakeContextStore) ListContextBoundaries(_ context.Context, sessionID string) ([]model.ContextBoundary, error) {
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

func (s *fakeContextStore) Append(_ context.Context, req store.ToolResultAppendRequest) (store.ToolResultRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ref := store.BuildToolResultRef(req.RunID, req.CallID)
	rec := store.ToolResultRecord{
		ResultRef:     ref,
		RunID:         req.RunID,
		SessionID:     req.SessionID,
		CallID:        req.CallID,
		ToolName:      req.ToolName,
		ArgumentsJSON: req.ArgumentsJSON,
		Status:        req.Status,
		ErrorReason:   req.ErrorReason,
		FullText:      req.FullText,
		Preview:       store.PreviewToolResult(req.FullText, 0),
		TokenEstimate: req.TokenEstimate,
		SideEffects:   req.SideEffects,
		EvidenceRefs:  req.EvidenceRefs,
		CreatedAt:     req.CreatedAt,
	}
	s.results[ref] = rec
	return rec, nil
}

func (s *fakeContextStore) Load(_ context.Context, ref string) (store.ToolResultRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.results[ref]
	if !ok {
		return store.ToolResultRecord{}, store.ErrToolResultNotFound
	}
	return rec, nil
}

func (s *fakeContextStore) ListByRun(_ context.Context, runID string) ([]store.ToolResultRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []store.ToolResultRecord
	for _, rec := range s.results {
		if rec.RunID == runID {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (s *fakeContextStore) AppendEvidenceRef(_ context.Context, ref string, ev store.EvidenceRef) (store.ToolResultRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.results[ref]
	if !ok {
		return store.ToolResultRecord{}, store.ErrToolResultNotFound
	}
	rec.EvidenceRefs = append(rec.EvidenceRefs, ev)
	s.results[ref] = rec
	return rec, nil
}

var testCompressionTokenLoaderOnce sync.Once

func ensureCompressionTokenLoader() error {
	testCompressionTokenLoaderOnce.Do(func() {
		tiktoken.SetBpeLoader(tiktokenloader.NewOfflineLoader())
	})
	return nil
}

func normalizeCompressionMessage(msg adk.Message) *schema.Message {
	if msg == nil {
		return &schema.Message{}
	}
	return &schema.Message{
		Role:                     msg.Role,
		Content:                  msg.Content,
		UserInputMultiContent:    append([]schema.MessageInputPart(nil), msg.UserInputMultiContent...),
		AssistantGenMultiContent: append([]schema.MessageOutputPart(nil), msg.AssistantGenMultiContent...),
		Name:                     msg.Name,
		ToolCalls:                append([]schema.ToolCall(nil), msg.ToolCalls...),
		ToolCallID:               msg.ToolCallID,
		ToolName:                 msg.ToolName,
		ReasoningContent:         msg.ReasoningContent,
	}
}

func normalizeCompressionTool(tool *schema.ToolInfo) *schema.ToolInfo {
	if tool == nil {
		return &schema.ToolInfo{}
	}
	clone := *tool
	clone.Extra = nil
	return &clone
}
