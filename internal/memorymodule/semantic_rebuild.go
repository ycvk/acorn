package memorymodule

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func (s *LocalService) RebuildSemanticIndex(ctx context.Context, opts SemanticRebuildOptions) (*SemanticRebuildResult, error) {
	if s == nil {
		return nil, fmt.Errorf("memory service is nil")
	}
	if err := validateSemanticRebuildOptions(opts); err != nil {
		return nil, err
	}
	if err := s.EnsureLayout(ctx); err != nil {
		return nil, err
	}
	if err := s.BuildIndex(ctx); err != nil {
		return nil, fmt.Errorf("build memory index before semantic rebuild: %w", err)
	}
	records, err := s.semanticRecords(ctx)
	if err != nil {
		return nil, err
	}
	indexed, skipped, err := embedSemanticRecords(ctx, records, opts)
	if err != nil {
		return nil, err
	}
	result, err := opts.Index.Rebuild(ctx, SemanticRebuildRequest{
		Records:    indexed,
		Model:      strings.TrimSpace(opts.Model),
		Dimensions: opts.Dimensions,
		Schema:     strings.TrimSpace(opts.Schema),
		IndexName:  strings.TrimSpace(opts.IndexName),
	})
	if err != nil {
		return nil, fmt.Errorf("rebuild semantic index: %w", err)
	}
	if result == nil {
		return nil, errors.New("semantic index rebuild returned nil result")
	}
	result.SkippedCount += skipped
	return result, nil
}

func (s *LocalService) semanticRecords(ctx context.Context) ([]SemanticRecord, error) {
	records, err := s.allRecords(ctx)
	if err != nil {
		return nil, err
	}
	selected, err := SelectRecords(records, RecordSelection{IncludeInactive: true})
	if err != nil {
		return nil, err
	}
	out := make([]SemanticRecord, 0, len(selected))
	for _, record := range selected {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		semantic := SemanticRecordFromRecord(record)
		hash, err := SemanticRecordContentHash(semantic)
		if err != nil {
			return nil, fmt.Errorf("hash semantic record %q: %w", record.Ref, err)
		}
		semantic.ContentHash = hash
		out = append(out, semantic)
	}
	SortSemanticRecordsByRef(out)
	return out, nil
}

func embedSemanticRecords(ctx context.Context, records []SemanticRecord, opts SemanticRebuildOptions) ([]IndexedSemanticRecord, int, error) {
	indexed := make([]IndexedSemanticRecord, 0, len(records))
	batchSize := opts.BatchSize
	skipped := 0
	for start := 0; start < len(records); {
		inputs := make([]EmbedInput, 0, batchSize)
		batchRecords := make([]SemanticRecord, 0, batchSize)
		for start < len(records) && len(inputs) < batchSize {
			record := records[start]
			start++
			text := SemanticRecordText(record)
			if strings.TrimSpace(text) == "" {
				skipped++
				continue
			}
			inputs = append(inputs, EmbedInput{Ref: record.Ref, Text: text})
			batchRecords = append(batchRecords, record)
		}
		if len(inputs) == 0 {
			continue
		}
		result, err := opts.Embedder.Embed(ctx, EmbedRequest{Inputs: inputs})
		if err != nil {
			return nil, skipped, fmt.Errorf("embed semantic records: %w", err)
		}
		if err := ValidateEmbedResult(EmbedRequest{Inputs: inputs}, result, opts.Dimensions); err != nil {
			return nil, skipped, err
		}
		if result.Model != opts.Model {
			return nil, skipped, fmt.Errorf("embed result model = %q, want %q", result.Model, opts.Model)
		}
		for i, vector := range result.Vectors {
			indexed = append(indexed, IndexedSemanticRecord{
				Record: batchRecords[i],
				Vector: append([]float32(nil), vector.Values...),
			})
		}
	}
	return indexed, skipped, nil
}

func validateSemanticRebuildOptions(opts SemanticRebuildOptions) error {
	if opts.Index == nil {
		return errors.New("semantic rebuild index is required")
	}
	if opts.Embedder == nil {
		return errors.New("semantic rebuild embedder is required")
	}
	if strings.TrimSpace(opts.Model) == "" {
		return errors.New("semantic rebuild model is required")
	}
	if opts.Dimensions <= 0 {
		return errors.New("semantic rebuild dimensions must be > 0")
	}
	if opts.BatchSize <= 0 {
		return errors.New("semantic rebuild batch size must be > 0")
	}
	if strings.TrimSpace(opts.Schema) == "" {
		return errors.New("semantic rebuild schema is required")
	}
	if strings.TrimSpace(opts.IndexName) == "" {
		return errors.New("semantic rebuild index name is required")
	}
	return nil
}
