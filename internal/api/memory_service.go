package api

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

const (
	defaultMemoryListLimit = 50
	maxMemoryListLimit     = 200
)

// memoryReader is the part of the memory store the client API reads.
type memoryReader interface {
	ListMemoryRecords(ctx context.Context, q core.MemoryQuery) ([]core.MemoryRecord, error)
	SearchMemoryText(ctx context.Context, q core.MemoryQuery) ([]core.MemoryRecord, error)
	MemoryRecordsByIDs(ctx context.Context, ids []string, q core.MemoryQuery) ([]core.MemoryRecord, error)
	ReadMemory(ctx context.Context, id string) (core.MemoryRead, error)
}

// memoryOwner applies the changes the owner makes in the app.
type memoryOwner interface {
	ForgetForOwner(ctx context.Context, id string) (core.MemoryExclusion, error)
	CorrectForOwner(ctx context.Context, id string, revision int64, content string) (core.MemoryRecord, error)
}

// MemoryService lets the owner see what Acorn remembers, trace it to its
// sources, correct facts and forget memories.
type MemoryService struct {
	reader  memoryReader
	owner   memoryOwner
	barrier func(context.Context, core.MemoryExclusion, string) error
}

// NewMemoryService takes barrier to wait until executions that saw a
// forgotten memory have stopped before a forget is acknowledged.
func NewMemoryService(reader memoryReader, owner memoryOwner, barrier func(context.Context, core.MemoryExclusion, string) error) (*MemoryService, error) {
	if reader == nil || owner == nil || barrier == nil {
		return nil, errors.New("memory service: reader, owner and barrier are required")
	}
	return &MemoryService{reader: reader, owner: owner, barrier: barrier}, nil
}

// MemorySummaryDTO is one memory in a listing.
type MemorySummaryDTO struct {
	ID          string     `json:"id"`
	Kind        string     `json:"kind"`
	Content     string     `json:"content"`
	Scope       string     `json:"scope,omitempty"`
	State       string     `json:"state"`
	Revision    int64      `json:"revision"`
	Pinned      bool       `json:"pinned"`
	NeedsReview bool       `json:"needs_review"`
	ValidFrom   *time.Time `json:"valid_from,omitempty"`
	ValidTo     *time.Time `json:"valid_to,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// MemoryListResponse is GET /v1/memories.
type MemoryListResponse struct {
	Memories []MemorySummaryDTO `json:"memories"`
	HasMore  bool               `json:"has_more"`
}

// MemoryEvidenceDTO is a quote a memory rests on and where it came from.
type MemoryEvidenceDTO struct {
	Quote      string    `json:"quote"`
	Relation   string    `json:"relation"`
	SourceKind string    `json:"source_kind"`
	Speaker    string    `json:"speaker"`
	RecordedAt time.Time `json:"recorded_at"`
	ThreadID   string    `json:"thread_id,omitempty"`
	NotePath   string    `json:"note_path,omitempty"`
	ToolName   string    `json:"tool_name,omitempty"`
}

// MemoryEarlierDTO is an earlier wording of a memory.
type MemoryEarlierDTO struct {
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MemoryDetailDTO is GET /v1/memories/{memory_id}.
type MemoryDetailDTO struct {
	Memory   MemorySummaryDTO    `json:"memory"`
	Evidence []MemoryEvidenceDTO `json:"evidence"`
	BasedOn  []MemorySummaryDTO  `json:"based_on"`
	Earlier  []MemoryEarlierDTO  `json:"earlier"`
}

// MemoryForgetResponse is POST /v1/memories/{memory_id}:forget.
type MemoryForgetResponse struct {
	ForgottenIDs []string `json:"forgotten_ids"`
}

// CorrectMemoryRequest is POST /v1/memories/{memory_id}:correct.
type CorrectMemoryRequest struct {
	Revision int64  `json:"revision"`
	Content  string `json:"content"`
}

// List searches with query, or lists memories by last change; kind narrows
// either to fact, insight or thought.
func (s *MemoryService) List(ctx context.Context, kind, query string, limit int) (MemoryListResponse, error) {
	q := core.MemoryQuery{Kind: kind, Query: strings.TrimSpace(query), Limit: limit + 1}
	var (
		records []core.MemoryRecord
		err     error
	)
	if q.Query != "" {
		records, err = s.reader.SearchMemoryText(ctx, q)
	} else {
		records, err = s.reader.ListMemoryRecords(ctx, q)
	}
	if err != nil {
		return MemoryListResponse{}, err
	}
	out := MemoryListResponse{Memories: make([]MemorySummaryDTO, 0, min(len(records), limit)), HasMore: len(records) > limit}
	for _, record := range records[:min(len(records), limit)] {
		out.Memories = append(out.Memories, memorySummary(record))
	}
	return out, nil
}

// Get reads a memory with its evidence, the memories it is based on and its
// earlier wordings. Evidence from forgotten sources is left out.
func (s *MemoryService) Get(ctx context.Context, id string) (MemoryDetailDTO, error) {
	read, err := s.reader.ReadMemory(ctx, id)
	if err != nil {
		return MemoryDetailDTO{}, err
	}
	sources := make(map[string]core.MemorySource, len(read.Sources))
	for _, source := range read.Sources {
		sources[source.ID] = source
	}
	out := MemoryDetailDTO{
		Memory:   memorySummary(read.Record),
		Evidence: make([]MemoryEvidenceDTO, 0, len(read.Record.Evidence)),
		BasedOn:  []MemorySummaryDTO{},
		Earlier:  []MemoryEarlierDTO{},
	}
	for _, evidence := range read.Record.Evidence {
		source, ok := sources[evidence.SourceID]
		if !ok {
			continue
		}
		out.Evidence = append(out.Evidence, memoryEvidence(evidence, source))
	}
	if len(read.Record.Parents) > 0 {
		parents, err := s.reader.MemoryRecordsByIDs(ctx, read.Record.Parents, core.MemoryQuery{Mode: "history", Limit: len(read.Record.Parents)})
		if err != nil {
			return MemoryDetailDTO{}, err
		}
		for _, parent := range parents {
			out.BasedOn = append(out.BasedOn, memorySummary(parent))
		}
	}
	for _, version := range slices.Backward(read.Versions) {
		if version.Revision < read.Record.Revision && version.Content != read.Record.Content {
			out.Earlier = append(out.Earlier, MemoryEarlierDTO{Content: version.Content, UpdatedAt: version.UpdatedAt.UTC()})
		}
	}
	var superseded []string
	for _, link := range read.Links {
		if link.Relation == "supersedes" && link.FromID == read.Record.ID {
			superseded = append(superseded, link.ToID)
		}
	}
	if len(superseded) > 0 {
		earlier, err := s.reader.MemoryRecordsByIDs(ctx, superseded, core.MemoryQuery{Mode: "history", Limit: len(superseded)})
		if err != nil {
			return MemoryDetailDTO{}, err
		}
		for _, record := range earlier {
			out.Earlier = append(out.Earlier, MemoryEarlierDTO{Content: record.Content, UpdatedAt: record.UpdatedAt.UTC()})
		}
	}
	return out, nil
}

// Forget excludes the memory and everything derived from it, and returns
// once executions that saw it have stopped.
func (s *MemoryService) Forget(ctx context.Context, id string) (MemoryForgetResponse, error) {
	exclusion, err := s.owner.ForgetForOwner(ctx, id)
	if err != nil {
		return MemoryForgetResponse{}, err
	}
	if err := s.barrier(ctx, exclusion, ""); err != nil {
		return MemoryForgetResponse{}, err
	}
	return MemoryForgetResponse{ForgottenIDs: nonNilStrings(exclusion.RecordIDs)}, nil
}

// Correct replaces a fact with the owner's wording and returns the new fact.
func (s *MemoryService) Correct(ctx context.Context, id string, req CorrectMemoryRequest) (MemorySummaryDTO, error) {
	record, err := s.owner.CorrectForOwner(ctx, id, req.Revision, req.Content)
	if err != nil {
		return MemorySummaryDTO{}, err
	}
	return memorySummary(record), nil
}

func memorySummary(record core.MemoryRecord) MemorySummaryDTO {
	return MemorySummaryDTO{
		ID:          record.ID,
		Kind:        record.Kind,
		Content:     record.Content,
		Scope:       record.Scope,
		State:       record.State,
		Revision:    record.Revision,
		Pinned:      record.Pinned,
		NeedsReview: record.NeedsReview,
		ValidFrom:   optionalTime(record.ValidFrom),
		ValidTo:     optionalTime(record.ValidTo),
		UpdatedAt:   record.UpdatedAt.UTC(),
	}
}

func memoryEvidence(evidence core.MemoryEvidence, source core.MemorySource) MemoryEvidenceDTO {
	out := MemoryEvidenceDTO{
		Quote:      evidence.Quote,
		Relation:   evidence.Relation,
		SourceKind: source.Kind,
		Speaker:    source.Speaker,
		RecordedAt: source.RecordedAt.UTC(),
		ThreadID:   source.SessionID,
		ToolName:   source.ToolName,
	}
	if source.Kind == "knowledge" {
		out.NotePath = source.ObjectID
	}
	return out
}

func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	utc := t.UTC()
	return &utc
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
