//go:build bleve_faiss && vectors && cgo

package memorymodule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	bleve "github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	index "github.com/blevesearch/bleve_index_api"
)

func (i *bleveSemanticIndex) Rebuild(ctx context.Context, req SemanticRebuildRequest) (*SemanticRebuildResult, error) {
	if i == nil {
		return nil, errors.New("bleve semantic index is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := i.validateRebuildRequest(req); err != nil {
		return nil, err
	}

	deletedCount, err := i.existingDocCount()
	if err != nil {
		return nil, err
	}

	tempPath, err := os.MkdirTemp(i.path, "."+i.indexName+".rebuild-*")
	if err != nil {
		return nil, fmt.Errorf("create temporary bleve semantic index: %w", err)
	}
	defer func() {
		if tempPath != "" {
			_ = os.RemoveAll(tempPath)
		}
	}()

	idx, err := i.buildTempIndex(ctx, tempPath, req)
	if err != nil {
		return nil, err
	}
	if err := idx.Close(); err != nil {
		return nil, fmt.Errorf("close bleve semantic index %q: %w", tempPath, err)
	}

	oldPath, err := i.setOldIndexAside()
	if err != nil {
		return nil, err
	}
	if err := i.commitTempIndex(tempPath, oldPath); err != nil {
		return nil, err
	}
	tempPath = ""

	if oldPath != "" {
		_ = os.RemoveAll(oldPath)
	}

	return &SemanticRebuildResult{
		Model:        req.Model,
		Dimensions:   req.Dimensions,
		Schema:       req.Schema,
		IndexName:    i.indexName,
		IndexedCount: len(req.Records),
		DeletedCount: int(deletedCount),
	}, nil
}

func (i *bleveSemanticIndex) buildTempIndex(ctx context.Context, tempPath string, req SemanticRebuildRequest) (bleve.Index, error) {
	idxMapping, err := newBleveSemanticIndexMapping(i.dimensions)
	if err != nil {
		return nil, err
	}
	idx, err := bleve.New(tempPath, idxMapping)
	if err != nil {
		return nil, fmt.Errorf("create bleve semantic index %q: %w", tempPath, err)
	}
	defer func() {
		if idx != nil {
			_ = idx.Close()
		}
	}()

	batch := idx.NewBatch()
	for _, indexed := range req.Records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		doc, err := newBleveSemanticDocument(indexed, req)
		if err != nil {
			return nil, err
		}
		if err := batch.Index(indexed.Record.Ref, doc); err != nil {
			return nil, fmt.Errorf("stage bleve semantic record %q: %w", indexed.Record.Ref, err)
		}
	}
	if err := idx.Batch(batch); err != nil {
		return nil, fmt.Errorf("write bleve semantic index %q: %w", i.indexPath, err)
	}

	open := idx
	idx = nil
	return open, nil
}

func (i *bleveSemanticIndex) setOldIndexAside() (string, error) {
	oldPath := ""
	if _, err := os.Stat(i.indexPath); err == nil {
		oldPath = i.indexPath + ".rebuild-old"
		_ = os.RemoveAll(oldPath)
		if err := os.Rename(i.indexPath, oldPath); err != nil {
			return "", fmt.Errorf("move existing bleve semantic index aside: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat bleve semantic index %q: %w", i.indexPath, err)
	}
	return oldPath, nil
}

func (i *bleveSemanticIndex) commitTempIndex(tempPath, oldPath string) error {
	if err := os.Rename(tempPath, i.indexPath); err != nil {
		if oldPath != "" {
			_ = os.Rename(oldPath, i.indexPath)
		}
		return fmt.Errorf("commit bleve semantic index %q: %w", i.indexPath, err)
	}
	return nil
}

func (i *bleveSemanticIndex) validateRebuildRequest(req SemanticRebuildRequest) error {
	if strings.TrimSpace(req.Model) == "" {
		return errors.New("semantic rebuild model is required")
	}
	if req.Dimensions <= 0 {
		return errors.New("semantic rebuild dimensions must be > 0")
	}
	if req.Dimensions != i.dimensions {
		return fmt.Errorf("bleve semantic index dimensions = %d, want %d; rebuild semantic index", i.dimensions, req.Dimensions)
	}
	if strings.TrimSpace(req.Schema) == "" {
		return errors.New("semantic rebuild schema is required")
	}
	if schema := strings.TrimSpace(req.Schema); schema != SemanticSchemaMemoryRecordsV1 {
		return fmt.Errorf("semantic rebuild schema = %q, want %q", schema, SemanticSchemaMemoryRecordsV1)
	}
	if indexName := strings.TrimSpace(req.IndexName); indexName == "" {
		return errors.New("semantic rebuild index name is required")
	} else if indexName != i.indexName {
		return fmt.Errorf("bleve semantic index name = %q, want %q", indexName, i.indexName)
	}
	return i.validateRebuildRecords(req)
}

func (i *bleveSemanticIndex) validateRebuildRecords(req SemanticRebuildRequest) error {
	for _, indexed := range req.Records {
		record := indexed.Record
		if strings.TrimSpace(record.Ref) == "" {
			return errors.New("semantic rebuild record ref is required")
		}
		if strings.TrimSpace(string(record.Kind)) == "" {
			return fmt.Errorf("semantic rebuild record %q kind is required", record.Ref)
		}
		if strings.TrimSpace(string(record.Status)) == "" {
			return fmt.Errorf("semantic rebuild record %q status is required", record.Ref)
		}
		if strings.TrimSpace(record.ContentHash) == "" {
			return fmt.Errorf("semantic rebuild record %q content hash is required", record.Ref)
		}
		if len(indexed.Vector) != req.Dimensions {
			return fmt.Errorf("semantic rebuild record %q vector dimensions = %d, want %d", record.Ref, len(indexed.Vector), req.Dimensions)
		}
	}
	return nil
}

func newBleveSemanticIndexMapping(dimensions int) (*mapping.IndexMappingImpl, error) {
	m := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentStaticMapping()

	textField := bleve.NewTextFieldMapping()
	keywordField := bleve.NewKeywordFieldMapping()
	numericField := bleve.NewNumericFieldMapping()
	booleanField := bleve.NewBooleanFieldMapping()
	vectorField := bleve.NewVectorFieldMapping()
	if vectorField == nil {
		return nil, errors.New("bleve vector field mapping is unavailable; rebuild with -tags vectors")
	}
	vectorField.Dims = dimensions
	vectorField.Similarity = index.CosineSimilarity
	vectorField.VectorIndexOptimizedFor = index.IndexOptimizedForRecall

	docMapping.AddFieldMappingsAt(bleveSemanticFieldKind, keywordField)
	docMapping.AddFieldMappingsAt(bleveSemanticFieldScope, keywordField)
	docMapping.AddFieldMappingsAt(bleveSemanticFieldScopeEmpty, booleanField)
	docMapping.AddFieldMappingsAt(bleveSemanticFieldStatus, keywordField)
	docMapping.AddFieldMappingsAt(bleveSemanticFieldOrigin, keywordField)
	docMapping.AddFieldMappingsAt(bleveSemanticFieldTitle, textField)
	docMapping.AddFieldMappingsAt(bleveSemanticFieldBody, textField)
	docMapping.AddFieldMappingsAt(bleveSemanticFieldPath, textField)
	docMapping.AddFieldMappingsAt(bleveSemanticFieldTags, textField)
	docMapping.AddFieldMappingsAt(bleveSemanticFieldTaskPattern, textField)
	docMapping.AddFieldMappingsAt(bleveSemanticFieldSourceRun, keywordField)
	docMapping.AddFieldMappingsAt(bleveSemanticFieldSourceRefs, keywordField)
	docMapping.AddFieldMappingsAt(bleveSemanticFieldEvidenceRefs, keywordField)
	docMapping.AddFieldMappingsAt(bleveSemanticFieldRelations, textField)
	docMapping.AddFieldMappingsAt(bleveSemanticFieldContentHash, keywordField)
	docMapping.AddFieldMappingsAt(bleveSemanticFieldCreated, keywordField)
	docMapping.AddFieldMappingsAt(bleveSemanticFieldUpdated, keywordField)
	docMapping.AddFieldMappingsAt(bleveSemanticFieldValidFrom, keywordField)
	docMapping.AddFieldMappingsAt(bleveSemanticFieldValidUntil, keywordField)
	docMapping.AddFieldMappingsAt(bleveSemanticFieldModel, keywordField)
	docMapping.AddFieldMappingsAt(bleveSemanticFieldDimensions, numericField)
	docMapping.AddFieldMappingsAt(bleveSemanticFieldSchema, keywordField)
	docMapping.AddFieldMappingsAt(bleveSemanticFieldVector, vectorField)

	m.DefaultMapping = docMapping
	return m, nil
}

func newBleveSemanticDocument(indexed IndexedSemanticRecord, req SemanticRebuildRequest) (*bleveSemanticDocument, error) {
	record := indexed.Record
	sourceRefsJSON, err := json.Marshal(record.SourceRefs)
	if err != nil {
		return nil, fmt.Errorf("encode source refs for %q: %w", record.Ref, err)
	}
	evidenceRefsJSON, err := json.Marshal(record.EvidenceRefs)
	if err != nil {
		return nil, fmt.Errorf("encode evidence refs for %q: %w", record.Ref, err)
	}
	relationsJSON, err := json.Marshal(semanticRelationHashPayloads(record.Relations))
	if err != nil {
		return nil, fmt.Errorf("encode relations for %q: %w", record.Ref, err)
	}
	return &bleveSemanticDocument{
		Kind:             string(record.Kind),
		Scope:            strings.TrimSpace(record.Scope),
		ScopeEmpty:       strings.TrimSpace(record.Scope) == "",
		Status:           string(record.Status),
		Origin:           strings.TrimSpace(record.Origin),
		Title:            strings.TrimSpace(record.Title),
		Body:             strings.TrimSpace(record.Body),
		Path:             strings.TrimSpace(record.Path),
		Tags:             append([]string(nil), record.Tags...),
		TaskPattern:      strings.TrimSpace(record.TaskPattern),
		SourceRun:        strings.TrimSpace(record.SourceRun),
		SourceRefsJSON:   string(sourceRefsJSON),
		EvidenceRefsJSON: string(evidenceRefsJSON),
		RelationsJSON:    string(relationsJSON),
		ContentHash:      strings.TrimSpace(record.ContentHash),
		Created:          strings.TrimSpace(record.Created),
		Updated:          strings.TrimSpace(record.Updated),
		ValidFrom:        strings.TrimSpace(record.ValidFrom),
		ValidUntil:       strings.TrimSpace(record.ValidUntil),
		Model:            strings.TrimSpace(req.Model),
		Dimensions:       req.Dimensions,
		Schema:           strings.TrimSpace(req.Schema),
		Vector:           append([]float32(nil), indexed.Vector...),
	}, nil
}
