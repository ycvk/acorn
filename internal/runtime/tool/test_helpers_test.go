package tool

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/contextplane"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/store"
)

// NewTestToolLifecycleContext creates a tool lifecycle context suitable for tests.
// It registers all tools in the node as loaded and attaches the lifecycle to ctx.
func NewTestToolLifecycleContext(ctx context.Context, node *SafeParallelToolsNode) context.Context {
	sessionID := runtimeapi.SessionIDFromContext(ctx)
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "sess_test"
		ctx = runtimeapi.WithSessionID(ctx, sessionID)
	}
	runID := runtimeapi.GetRunID(ctx)
	if strings.TrimSpace(runID) == "" {
		runID = "run_test"
		ctx = runtimeapi.WithRunID(ctx, runID)
	}
	state := &contextplane.ToolLifecycleState{
		RunID:         runID,
		SessionID:     sessionID,
		LoadedTools:   make(map[string]contextplane.LoadedToolRecord, len(node.tools)),
		DeferredTools: make(map[string]contextplane.DeferredToolRecord),
		MaxAgeTurns:   2,
		MaxResultRefs: 32,
	}
	infos := make([]*schema.ToolInfo, 0, len(node.tools))
	for name, entry := range node.tools {
		state.LoadedTools[name] = contextplane.LoadedToolRecord{Name: name, LoadSource: "test"}
		info, err := entry.Tool.Info(context.Background())
		if err != nil {
			continue
		}
		if info != nil {
			infos = append(infos, info)
		}
	}
	return contextplane.WithToolLifecycleContext(ctx, NewMemoryToolResultLedger(), state, nil, infos)
}

// MustInferTool infers a tool from a function and fails the test on error.
func MustInferTool[T any, R any](t *testing.T, name string, fn func(context.Context, T) (R, error)) einotool.BaseTool {
	t.Helper()
	tool, err := toolutils.InferTool(name, name, fn)
	if err != nil {
		t.Fatalf("infer tool: %v", err)
	}
	return tool
}

// SafeParallelLifecycleContextFromWithLedger creates a tool lifecycle context for tests
// with an explicit tool-result ledger. If ledger is nil, it falls back to NewTestToolLifecycleContext.
func SafeParallelLifecycleContextFromWithLedger(t *testing.T, ctx context.Context, node *SafeParallelToolsNode, ledger store.ToolResultLedger) context.Context {
	t.Helper()
	if ledger == nil {
		return NewTestToolLifecycleContext(ctx, node)
	}
	sessionID := runtimeapi.SessionIDFromContext(ctx)
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "sess_test"
		ctx = runtimeapi.WithSessionID(ctx, sessionID)
	}
	runID := runtimeapi.GetRunID(ctx)
	if strings.TrimSpace(runID) == "" {
		runID = "run_test"
		ctx = runtimeapi.WithRunID(ctx, runID)
	}
	state := &contextplane.ToolLifecycleState{
		RunID:         runID,
		SessionID:     sessionID,
		LoadedTools:   make(map[string]contextplane.LoadedToolRecord, len(node.tools)),
		DeferredTools: make(map[string]contextplane.DeferredToolRecord),
		MaxAgeTurns:   2,
		MaxResultRefs: 32,
	}
	infos := make([]*schema.ToolInfo, 0, len(node.tools))
	for name, entry := range node.tools {
		state.LoadedTools[name] = contextplane.LoadedToolRecord{Name: name, LoadSource: "test"}
		info, err := entry.Tool.Info(context.Background())
		if err != nil {
			continue
		}
		if info != nil {
			infos = append(infos, info)
		}
	}
	return contextplane.WithToolLifecycleContext(ctx, ledger, state, nil, infos)
}

// --- Memory tool result ledger (copied from original runtime test helpers) ---

type memoryToolResultLedger struct {
	mu      sync.Mutex
	records map[string]store.ToolResultRecord
	order   []string
}

// NewMemoryToolResultLedger creates an in-memory tool result ledger for testing.
func NewMemoryToolResultLedger() store.ToolResultLedger {
	return &memoryToolResultLedger{records: make(map[string]store.ToolResultRecord)}
}

func (m *memoryToolResultLedger) Append(_ context.Context, req store.ToolResultAppendRequest) (store.ToolResultRecord, error) {
	req, err := store.NormalizeToolResultAppendRequest(req)
	if err != nil {
		return store.ToolResultRecord{}, err
	}
	rec := store.ToolResultRecord{
		ResultRef:     store.BuildToolResultRef(req.RunID, req.CallID),
		RunID:         req.RunID,
		SessionID:     req.SessionID,
		TurnIndex:     req.TurnIndex,
		CallID:        req.CallID,
		ToolName:      req.ToolName,
		ArgumentsJSON: req.ArgumentsJSON,
		Status:        req.Status,
		ErrorReason:   req.ErrorReason,
		Preview:       store.PreviewToolResult(req.FullText, 240),
		FullText:      req.FullText,
		TokenEstimate: req.TokenEstimate,
		SideEffects:   append([]store.SideEffectRef(nil), req.SideEffects...),
		EvidenceRefs:  append([]store.EvidenceRef(nil), req.EvidenceRefs...),
		CreatedAt:     req.CreatedAt,
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.records == nil {
		m.records = make(map[string]store.ToolResultRecord)
	}
	if _, exists := m.records[rec.ResultRef]; !exists {
		m.order = append(m.order, rec.ResultRef)
	}
	m.records[rec.ResultRef] = rec
	return rec, nil
}

func (m *memoryToolResultLedger) Load(_ context.Context, ref string) (store.ToolResultRecord, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return store.ToolResultRecord{}, fmt.Errorf("tool result ref is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[ref]
	if !ok {
		return store.ToolResultRecord{}, store.ErrToolResultNotFound
	}
	return record, nil
}

func (m *memoryToolResultLedger) ListByRun(_ context.Context, runID string) ([]store.ToolResultRecord, error) {
	runID = strings.TrimSpace(runID)
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]store.ToolResultRecord, 0)
	for _, ref := range m.order {
		record := m.records[ref]
		if record.RunID == runID {
			out = append(out, record)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ResultRef < out[j].ResultRef
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return append([]store.ToolResultRecord(nil), out...), nil
}

func (m *memoryToolResultLedger) AppendEvidenceRef(_ context.Context, resultRef string, ref store.EvidenceRef) (store.ToolResultRecord, error) {
	ref, err := store.NormalizeEvidenceRef(ref)
	if err != nil {
		return store.ToolResultRecord{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[strings.TrimSpace(resultRef)]
	if !ok {
		return store.ToolResultRecord{}, store.ErrToolResultNotFound
	}
	for _, existing := range record.EvidenceRefs {
		if existing.Kind == ref.Kind && existing.Ref == ref.Ref {
			return record, nil
		}
	}
	record.EvidenceRefs = append(record.EvidenceRefs, ref)
	m.records[record.ResultRef] = record
	return record, nil
}
