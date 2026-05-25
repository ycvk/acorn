package orchestration

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/ycvk/acorn/internal/store"
)

type memoryToolResultLedger struct {
	mu      sync.Mutex
	records map[string]store.ToolResultRecord
	order   []string
}

func newMemoryToolResultLedger() *memoryToolResultLedger {
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
