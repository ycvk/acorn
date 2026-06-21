package memorymodule

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Embedder converts text inputs into dense float32 vectors via an external
// embedding model (e.g. an OpenAI-compatible embeddings API).
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

const SemanticSchemaMemoryRecordsV1 = "memory_records_v1"

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

// embedRecordText is the text fed to the embedder for a record. It is a
// compact newline-joined projection of the record's searchable fields.
func embedRecordText(record Record) string {
	parts := []string{
		"kind: " + string(record.Kind),
		"scope: " + strings.TrimSpace(record.Scope),
		"status: " + string(record.Status),
		"origin: " + strings.TrimSpace(record.Origin),
		"path: " + strings.TrimSpace(record.RelPath),
		"title: " + strings.TrimSpace(record.Title),
		"tags: " + strings.Join(record.Tags, ", "),
		"task_pattern: " + strings.TrimSpace(record.TaskPattern),
		"source_run: " + strings.TrimSpace(record.SourceRun),
		"source_refs: " + strings.Join(record.SourceRefs, ", "),
		"created: " + strings.TrimSpace(record.Created),
		"updated: " + strings.TrimSpace(record.Updated),
		"body: " + strings.TrimSpace(record.Body),
	}
	return compactSemanticText(parts)
}

// recordContentHash is a stable hash of the record's embeddable content, used
// to skip re-embedding when a record's text is unchanged.
func recordContentHash(record Record) (string, error) {
	payload := struct {
		Ref         string   `json:"ref"`
		Kind        Kind     `json:"kind"`
		Scope       string   `json:"scope"`
		Status      Status   `json:"status"`
		Origin      string   `json:"origin"`
		Title       string   `json:"title"`
		Body        string   `json:"body"`
		Path        string   `json:"path"`
		Tags        []string `json:"tags"`
		TaskPattern string   `json:"task_pattern"`
		SourceRun   string   `json:"source_run"`
		SourceRefs  []string `json:"source_refs"`
		Created     string   `json:"created"`
		Updated     string   `json:"updated"`
	}{
		Ref:         strings.TrimSpace(record.Ref),
		Kind:        record.Kind,
		Scope:       strings.TrimSpace(record.Scope),
		Status:      record.Status,
		Origin:      strings.TrimSpace(record.Origin),
		Title:       strings.TrimSpace(record.Title),
		Body:        strings.TrimSpace(record.Body),
		Path:        strings.TrimSpace(record.RelPath),
		Tags:        append([]string(nil), record.Tags...),
		TaskPattern: strings.TrimSpace(record.TaskPattern),
		SourceRun:   strings.TrimSpace(record.SourceRun),
		SourceRefs:  append([]string(nil), record.SourceRefs...),
		Created:     strings.TrimSpace(record.Created),
		Updated:     strings.TrimSpace(record.Updated),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal record hash payload: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
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
