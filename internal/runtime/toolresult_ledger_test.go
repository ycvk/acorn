package runtime

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/ycvk/acorn/internal/toolresult"
)

type memoryToolResultLedger struct {
	mu      sync.Mutex
	records map[string]toolresult.Record
	order   []string
}

func newMemoryToolResultLedger() *memoryToolResultLedger {
	return &memoryToolResultLedger{records: make(map[string]toolresult.Record)}
}

func (m *memoryToolResultLedger) Append(_ context.Context, req toolresult.AppendRequest) (toolresult.Record, error) {
	req, err := toolresult.NormalizeAppendRequest(req)
	if err != nil {
		return toolresult.Record{}, err
	}
	rec := toolresult.Record{
		ResultRef:     toolresult.BuildRef(req.RunID, req.CallID),
		RunID:         req.RunID,
		SessionID:     req.SessionID,
		TurnIndex:     req.TurnIndex,
		CallID:        req.CallID,
		ToolName:      req.ToolName,
		ArgumentsJSON: req.ArgumentsJSON,
		Status:        req.Status,
		ErrorReason:   req.ErrorReason,
		Preview:       toolresult.Preview(req.FullText, 240),
		FullText:      req.FullText,
		TokenEstimate: req.TokenEstimate,
		SideEffects:   append([]toolresult.SideEffectRef(nil), req.SideEffects...),
		EvidenceRefs:  append([]toolresult.EvidenceRef(nil), req.EvidenceRefs...),
		CreatedAt:     req.CreatedAt,
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.records == nil {
		m.records = make(map[string]toolresult.Record)
	}
	if _, exists := m.records[rec.ResultRef]; !exists {
		m.order = append(m.order, rec.ResultRef)
	}
	m.records[rec.ResultRef] = rec
	return rec, nil
}

func (m *memoryToolResultLedger) Load(_ context.Context, ref string) (toolresult.Record, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return toolresult.Record{}, fmt.Errorf("tool result ref is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[ref]
	if !ok {
		return toolresult.Record{}, toolresult.ErrToolResultNotFound
	}
	return record, nil
}

func (m *memoryToolResultLedger) ListByRun(_ context.Context, runID string) ([]toolresult.Record, error) {
	runID = strings.TrimSpace(runID)
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]toolresult.Record, 0)
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
	return append([]toolresult.Record(nil), out...), nil
}

func (m *memoryToolResultLedger) AppendEvidenceRef(_ context.Context, resultRef string, ref toolresult.EvidenceRef) (toolresult.Record, error) {
	ref, err := toolresult.NormalizeEvidenceRef(ref)
	if err != nil {
		return toolresult.Record{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[strings.TrimSpace(resultRef)]
	if !ok {
		return toolresult.Record{}, toolresult.ErrToolResultNotFound
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
