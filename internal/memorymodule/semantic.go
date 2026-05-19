package memorymodule

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const SemanticSchemaMemoryRecordsV1 = "memory_records_v1"

type Embedder interface {
	Embed(ctx context.Context, req EmbedRequest) (*EmbedResult, error)
}

type EmbedRequest struct {
	Inputs []EmbedInput
}

type EmbedInput struct {
	Ref  string
	Text string
}

type EmbedResult struct {
	Model      string
	Dimensions int
	Vectors    []EmbeddingVector
}

type EmbeddingVector struct {
	Ref    string
	Values []float32
}

type SemanticIndex interface {
	Rebuild(ctx context.Context, req SemanticRebuildRequest) (*SemanticRebuildResult, error)
	Search(ctx context.Context, req SemanticSearchRequest) (*SemanticSearchResult, error)
	Close() error
}

type SemanticRecord struct {
	Ref          string
	Kind         Kind
	Scope        string
	Status       Status
	Origin       string
	Title        string
	Body         string
	Path         string
	Tags         []string
	TaskPattern  string
	SourceRun    string
	SourceRefs   []string
	EvidenceRefs []string
	Relations    []RecordRelation
	ContentHash  string
	Created      string
	Updated      string
	ValidFrom    string
	ValidUntil   string
}

type semanticRelationHashPayload struct {
	Type   RelationType `json:"type"`
	Target string       `json:"target"`
	Reason string       `json:"reason,omitempty"`
}

type IndexedSemanticRecord struct {
	Record SemanticRecord
	Vector []float32
}

type SemanticRebuildRequest struct {
	Records    []IndexedSemanticRecord
	Model      string
	Dimensions int
	Schema     string
	IndexName  string
}

type SemanticRebuildResult struct {
	Model        string
	Dimensions   int
	Schema       string
	IndexName    string
	IndexedCount int
	DeletedCount int
	SkippedCount int
}

type SemanticSearchRequest struct {
	Query           string
	Vector          []float32
	Scope           string
	Kinds           []Kind
	Limit           int
	IncludeInactive bool
	IncludeRetired  bool
	Mode            string
	Model           string
	Dimensions      int
	Explain         bool
}

type SemanticSearchResult struct {
	Hits []SemanticHit
}

type SemanticHit struct {
	Ref        string
	Kind       Kind
	Score      float64
	Distance   float64
	Stage      string
	SourceRefs []string
}

type SemanticRebuildOptions struct {
	Index      SemanticIndex
	Embedder   Embedder
	Model      string
	Dimensions int
	BatchSize  int
	Schema     string
	IndexName  string
}

type BleveSemanticIndexConfig struct {
	Path       string
	IndexName  string
	Dimensions int
}

func ValidateEmbedResult(req EmbedRequest, result *EmbedResult, dimensions int) error {
	if result == nil {
		return errors.New("embed result is required")
	}
	if len(result.Vectors) != len(req.Inputs) {
		return fmt.Errorf("embed result vector count = %d, want %d", len(result.Vectors), len(req.Inputs))
	}
	if result.Dimensions != dimensions {
		return fmt.Errorf("embed result dimensions = %d, want %d", result.Dimensions, dimensions)
	}
	if strings.TrimSpace(result.Model) == "" {
		return errors.New("embed result model is required")
	}
	for i, vector := range result.Vectors {
		if strings.TrimSpace(vector.Ref) == "" {
			return fmt.Errorf("embed result vectors[%d].ref is required", i)
		}
		if i < len(req.Inputs) && vector.Ref != req.Inputs[i].Ref {
			return fmt.Errorf("embed result vectors[%d].ref = %q, want %q", i, vector.Ref, req.Inputs[i].Ref)
		}
		if len(vector.Values) != dimensions {
			return fmt.Errorf("embed result vector %q dimensions = %d, want %d", vector.Ref, len(vector.Values), dimensions)
		}
	}
	return nil
}

func CloneEmbedResult(result *EmbedResult) *EmbedResult {
	if result == nil {
		return nil
	}
	clone := &EmbedResult{
		Model:      result.Model,
		Dimensions: result.Dimensions,
		Vectors:    make([]EmbeddingVector, 0, len(result.Vectors)),
	}
	for _, vector := range result.Vectors {
		clone.Vectors = append(clone.Vectors, EmbeddingVector{
			Ref:    vector.Ref,
			Values: append([]float32(nil), vector.Values...),
		})
	}
	return clone
}

func SemanticRecordFromRecord(record Record) SemanticRecord {
	return SemanticRecord{
		Ref:          record.Ref,
		Kind:         record.Kind,
		Scope:        record.Scope,
		Status:       record.Status,
		Origin:       record.Origin,
		Title:        record.Title,
		Body:         record.Body,
		Path:         record.RelPath,
		Tags:         append([]string(nil), record.Tags...),
		TaskPattern:  record.TaskPattern,
		SourceRun:    record.SourceRun,
		SourceRefs:   append([]string(nil), record.SourceRefs...),
		EvidenceRefs: append([]string(nil), record.EvidenceRefs...),
		Relations:    append([]RecordRelation(nil), record.Relations...),
		Created:      record.Created,
		Updated:      record.Updated,
		ValidFrom:    record.ValidFrom,
		ValidUntil:   record.ValidUntil,
	}
}

func SemanticRecordText(record SemanticRecord) string {
	parts := []string{
		"kind: " + string(record.Kind),
		"scope: " + strings.TrimSpace(record.Scope),
		"status: " + string(record.Status),
		"origin: " + strings.TrimSpace(record.Origin),
		"path: " + strings.TrimSpace(record.Path),
		"title: " + strings.TrimSpace(record.Title),
		"tags: " + strings.Join(record.Tags, ", "),
		"task_pattern: " + strings.TrimSpace(record.TaskPattern),
		"source_run: " + strings.TrimSpace(record.SourceRun),
		"source_refs: " + strings.Join(record.SourceRefs, ", "),
		"evidence_refs: " + strings.Join(record.EvidenceRefs, ", "),
		"relations: " + semanticRelationText(record.Relations),
		"created: " + strings.TrimSpace(record.Created),
		"updated: " + strings.TrimSpace(record.Updated),
		"valid_from: " + strings.TrimSpace(record.ValidFrom),
		"valid_until: " + strings.TrimSpace(record.ValidUntil),
		"body: " + strings.TrimSpace(record.Body),
	}
	return compactSemanticText(parts)
}

func SemanticRecordContentHash(record SemanticRecord) (string, error) {
	payload := struct {
		Ref          string                        `json:"ref"`
		Kind         Kind                          `json:"kind"`
		Scope        string                        `json:"scope"`
		Status       Status                        `json:"status"`
		Origin       string                        `json:"origin"`
		Title        string                        `json:"title"`
		Body         string                        `json:"body"`
		Path         string                        `json:"path"`
		Tags         []string                      `json:"tags"`
		TaskPattern  string                        `json:"task_pattern"`
		SourceRun    string                        `json:"source_run"`
		SourceRefs   []string                      `json:"source_refs"`
		EvidenceRefs []string                      `json:"evidence_refs"`
		Relations    []semanticRelationHashPayload `json:"relations"`
		Created      string                        `json:"created"`
		Updated      string                        `json:"updated"`
		ValidFrom    string                        `json:"valid_from"`
		ValidUntil   string                        `json:"valid_until"`
	}{
		Ref:          strings.TrimSpace(record.Ref),
		Kind:         record.Kind,
		Scope:        strings.TrimSpace(record.Scope),
		Status:       record.Status,
		Origin:       strings.TrimSpace(record.Origin),
		Title:        strings.TrimSpace(record.Title),
		Body:         strings.TrimSpace(record.Body),
		Path:         strings.TrimSpace(record.Path),
		Tags:         append([]string(nil), record.Tags...),
		TaskPattern:  strings.TrimSpace(record.TaskPattern),
		SourceRun:    strings.TrimSpace(record.SourceRun),
		SourceRefs:   append([]string(nil), record.SourceRefs...),
		EvidenceRefs: append([]string(nil), record.EvidenceRefs...),
		Relations:    semanticRelationHashPayloads(record.Relations),
		Created:      strings.TrimSpace(record.Created),
		Updated:      strings.TrimSpace(record.Updated),
		ValidFrom:    strings.TrimSpace(record.ValidFrom),
		ValidUntil:   strings.TrimSpace(record.ValidUntil),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal semantic record hash payload: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func CloneSemanticRecords(records []SemanticRecord) []SemanticRecord {
	if len(records) == 0 {
		return nil
	}
	cloned := make([]SemanticRecord, 0, len(records))
	for _, record := range records {
		cloned = append(cloned, SemanticRecord{
			Ref:          record.Ref,
			Kind:         record.Kind,
			Scope:        record.Scope,
			Status:       record.Status,
			Origin:       record.Origin,
			Title:        record.Title,
			Body:         record.Body,
			Path:         record.Path,
			Tags:         append([]string(nil), record.Tags...),
			TaskPattern:  record.TaskPattern,
			SourceRun:    record.SourceRun,
			SourceRefs:   append([]string(nil), record.SourceRefs...),
			EvidenceRefs: append([]string(nil), record.EvidenceRefs...),
			Relations:    append([]RecordRelation(nil), record.Relations...),
			ContentHash:  record.ContentHash,
			Created:      record.Created,
			Updated:      record.Updated,
			ValidFrom:    record.ValidFrom,
			ValidUntil:   record.ValidUntil,
		})
	}
	return cloned
}

func CloneIndexedSemanticRecords(records []IndexedSemanticRecord) []IndexedSemanticRecord {
	if len(records) == 0 {
		return nil
	}
	cloned := make([]IndexedSemanticRecord, 0, len(records))
	for _, record := range records {
		cloned = append(cloned, IndexedSemanticRecord{
			Record: CloneSemanticRecords([]SemanticRecord{record.Record})[0],
			Vector: append([]float32(nil), record.Vector...),
		})
	}
	return cloned
}

func semanticRelationHashPayloads(relations []RecordRelation) []semanticRelationHashPayload {
	if len(relations) == 0 {
		return nil
	}
	out := make([]semanticRelationHashPayload, 0, len(relations))
	for _, relation := range relations {
		out = append(out, semanticRelationHashPayload(relation))
	}
	return out
}

func semanticRelationText(relations []RecordRelation) string {
	if len(relations) == 0 {
		return ""
	}
	parts := make([]string, 0, len(relations))
	for _, relation := range relations {
		part := string(relation.Type) + " " + strings.TrimSpace(relation.Target)
		if reason := strings.TrimSpace(relation.Reason); reason != "" {
			part += " " + reason
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "; ")
}

func SortSemanticRecordsByRef(records []SemanticRecord) {
	sort.Slice(records, func(i, j int) bool {
		return records[i].Ref < records[j].Ref
	})
}

func compactSemanticText(parts []string) string {
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" && !strings.HasSuffix(trimmed, ":") {
			lines = append(lines, trimmed)
		}
	}
	return strings.Join(lines, "\n")
}
