//go:build bleve_faiss && vectors && cgo

package memorymodule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	bleve "github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
	index "github.com/blevesearch/bleve_index_api"
)

const (
	bleveSemanticFieldKind         = "kind"
	bleveSemanticFieldScope        = "scope"
	bleveSemanticFieldScopeEmpty   = "scope_empty"
	bleveSemanticFieldStatus       = "status"
	bleveSemanticFieldOrigin       = "origin"
	bleveSemanticFieldTitle        = "title"
	bleveSemanticFieldBody         = "body"
	bleveSemanticFieldPath         = "path"
	bleveSemanticFieldTags         = "tags"
	bleveSemanticFieldTaskPattern  = "task_pattern"
	bleveSemanticFieldSourceRun    = "source_run"
	bleveSemanticFieldSourceRefs   = "source_refs_json"
	bleveSemanticFieldEvidenceRefs = "evidence_refs_json"
	bleveSemanticFieldRelations    = "relations_json"
	bleveSemanticFieldContentHash  = "content_hash"
	bleveSemanticFieldCreated      = "created"
	bleveSemanticFieldUpdated      = "updated"
	bleveSemanticFieldValidFrom    = "valid_from"
	bleveSemanticFieldValidUntil   = "valid_until"
	bleveSemanticFieldModel        = "model"
	bleveSemanticFieldDimensions   = "dimensions"
	bleveSemanticFieldSchema       = "schema"
	bleveSemanticFieldVector       = "vector"

	bleveSemanticSearchHeadroom = 8
)

type bleveSemanticIndex struct {
	path       string
	indexName  string
	indexPath  string
	dimensions int
}

type bleveSemanticDocument struct {
	Kind             string    `json:"kind"`
	Scope            string    `json:"scope"`
	ScopeEmpty       bool      `json:"scope_empty"`
	Status           string    `json:"status"`
	Origin           string    `json:"origin"`
	Title            string    `json:"title"`
	Body             string    `json:"body"`
	Path             string    `json:"path"`
	Tags             []string  `json:"tags"`
	TaskPattern      string    `json:"task_pattern"`
	SourceRun        string    `json:"source_run"`
	SourceRefsJSON   string    `json:"source_refs_json"`
	EvidenceRefsJSON string    `json:"evidence_refs_json"`
	RelationsJSON    string    `json:"relations_json"`
	ContentHash      string    `json:"content_hash"`
	Created          string    `json:"created"`
	Updated          string    `json:"updated"`
	ValidFrom        string    `json:"valid_from"`
	ValidUntil       string    `json:"valid_until"`
	Model            string    `json:"model"`
	Dimensions       int       `json:"dimensions"`
	Schema           string    `json:"schema"`
	Vector           []float32 `json:"vector"`
}

func NewBleveSemanticIndex(ctx context.Context, cfg BleveSemanticIndexConfig) (SemanticIndex, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := strings.TrimSpace(cfg.Path)
	if path == "" {
		return nil, errors.New("bleve semantic index path is required")
	}
	indexName := strings.TrimSpace(cfg.IndexName)
	if indexName == "" {
		return nil, errors.New("bleve semantic index index name is required")
	}
	if cfg.Dimensions <= 0 {
		return nil, errors.New("bleve semantic index dimensions must be > 0")
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, fmt.Errorf("create bleve semantic index directory %q: %w", path, err)
	}
	return &bleveSemanticIndex{
		path:       path,
		indexName:  indexName,
		indexPath:  filepath.Join(path, indexName),
		dimensions: cfg.Dimensions,
	}, nil
}

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
	if err := idx.Close(); err != nil {
		return nil, fmt.Errorf("close bleve semantic index %q: %w", tempPath, err)
	}
	idx = nil

	oldPath := ""
	if _, err := os.Stat(i.indexPath); err == nil {
		oldPath = i.indexPath + ".rebuild-old"
		_ = os.RemoveAll(oldPath)
		if err := os.Rename(i.indexPath, oldPath); err != nil {
			return nil, fmt.Errorf("move existing bleve semantic index aside: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat bleve semantic index %q: %w", i.indexPath, err)
	}

	if err := os.Rename(tempPath, i.indexPath); err != nil {
		if oldPath != "" {
			_ = os.Rename(oldPath, i.indexPath)
		}
		return nil, fmt.Errorf("commit bleve semantic index %q: %w", i.indexPath, err)
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

func (i *bleveSemanticIndex) Search(ctx context.Context, req SemanticSearchRequest) (*SemanticSearchResult, error) {
	if i == nil {
		return nil, errors.New("bleve semantic index is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := i.validateSearchRequest(req); err != nil {
		return nil, err
	}
	idx, err := i.openIndex()
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = idx.Close()
	}()

	limit, err := resolveLimit("semantic search", req.Limit, defaultSearchLimit, maxSearchLimit)
	if err != nil {
		return nil, err
	}

	textQuery := buildBleveSemanticTextQuery(req.Query)
	filterQuery := buildBleveSemanticFilterQuery(req)
	mainQuery := textQuery
	if strings.TrimSpace(req.Query) != "" && filterQuery != nil {
		mainQuery = bleve.NewConjunctionQuery(textQuery, filterQuery)
	}

	searchReq := bleve.NewSearchRequest(mainQuery)
	searchReq.Size = limit + bleveSemanticSearchHeadroom
	if strings.TrimSpace(req.Query) != "" {
		searchReq.Score = bleve.ScoreRRF
	}
	searchReq.Fields = []string{
		bleveSemanticFieldKind,
		bleveSemanticFieldScope,
		bleveSemanticFieldScopeEmpty,
		bleveSemanticFieldStatus,
		bleveSemanticFieldOrigin,
		bleveSemanticFieldTitle,
		bleveSemanticFieldBody,
		bleveSemanticFieldPath,
		bleveSemanticFieldTags,
		bleveSemanticFieldTaskPattern,
		bleveSemanticFieldSourceRun,
		bleveSemanticFieldSourceRefs,
		bleveSemanticFieldEvidenceRefs,
		bleveSemanticFieldRelations,
		bleveSemanticFieldContentHash,
		bleveSemanticFieldCreated,
		bleveSemanticFieldUpdated,
		bleveSemanticFieldValidFrom,
		bleveSemanticFieldValidUntil,
		bleveSemanticFieldModel,
		bleveSemanticFieldDimensions,
		bleveSemanticFieldSchema,
	}
	if filterQuery != nil {
		searchReq.AddKNNWithFilter(bleveSemanticFieldVector, req.Vector, int64(limit+bleveSemanticSearchHeadroom), 1.0, filterQuery)
	} else {
		searchReq.AddKNN(bleveSemanticFieldVector, req.Vector, int64(limit+bleveSemanticSearchHeadroom), 1.0)
	}

	result, err := idx.SearchInContext(ctx, searchReq)
	if err != nil {
		return nil, fmt.Errorf("query bleve semantic index %q: %w", i.indexPath, err)
	}

	hits := make([]SemanticHit, 0, len(result.Hits))
	for _, hit := range result.Hits {
		if hit == nil {
			continue
		}
		semanticHit, err := semanticHitFromBleveMatch(hit.ID, hit.Fields, hit.Score, semanticStageForMode(req.Mode), req)
		if err != nil {
			return nil, err
		}
		hits = append(hits, semanticHit)
	}
	return &SemanticSearchResult{Hits: hits}, nil
}

func (i *bleveSemanticIndex) Close() error {
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

func (i *bleveSemanticIndex) validateSearchRequest(req SemanticSearchRequest) error {
	if req.Dimensions <= 0 {
		return errors.New("semantic search dimensions must be > 0")
	}
	if req.Dimensions != i.dimensions {
		return fmt.Errorf("bleve semantic index dimensions = %d, want %d; rebuild semantic index", i.dimensions, req.Dimensions)
	}
	if len(req.Vector) != req.Dimensions {
		return fmt.Errorf("semantic search vector dimensions = %d, want %d", len(req.Vector), req.Dimensions)
	}
	if len(req.Vector) != i.dimensions {
		return fmt.Errorf("bleve semantic index vector dimensions = %d, want %d; rebuild semantic index", len(req.Vector), i.dimensions)
	}
	return nil
}

func (i *bleveSemanticIndex) openIndex() (bleve.Index, error) {
	if _, err := os.Stat(i.indexPath); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("bleve semantic index %q is not built; rebuild semantic index", i.indexPath)
		}
		return nil, fmt.Errorf("stat bleve semantic index %q: %w", i.indexPath, err)
	}
	idx, err := bleve.Open(i.indexPath)
	if err != nil {
		return nil, fmt.Errorf("open bleve semantic index %q: %w", i.indexPath, err)
	}
	return idx, nil
}

func (i *bleveSemanticIndex) existingDocCount() (uint64, error) {
	if _, err := os.Stat(i.indexPath); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("stat bleve semantic index %q: %w", i.indexPath, err)
	}
	idx, err := bleve.Open(i.indexPath)
	if err != nil {
		return 0, fmt.Errorf("open bleve semantic index %q: %w", i.indexPath, err)
	}
	defer func() {
		_ = idx.Close()
	}()
	count, err := idx.DocCount()
	if err != nil {
		return 0, fmt.Errorf("count bleve semantic index docs: %w", err)
	}
	return count, nil
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

func buildBleveSemanticTextQuery(text string) query.Query {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return bleve.NewMatchNoneQuery()
	}
	fields := []struct {
		name  string
		boost float64
	}{
		{name: bleveSemanticFieldTitle, boost: 4.0},
		{name: bleveSemanticFieldTaskPattern, boost: 3.0},
		{name: bleveSemanticFieldTags, boost: 2.0},
		{name: bleveSemanticFieldPath, boost: 1.5},
		{name: bleveSemanticFieldBody, boost: 1.0},
	}
	queries := make([]query.Query, 0, len(fields))
	for _, field := range fields {
		q := bleve.NewMatchQuery(trimmed)
		q.SetField(field.name)
		q.SetBoost(field.boost)
		queries = append(queries, q)
	}
	return bleve.NewDisjunctionQuery(queries...)
}

func buildBleveSemanticFilterQuery(req SemanticSearchRequest) query.Query {
	filters := make([]query.Query, 0, 3)
	if scope := strings.TrimSpace(req.Scope); scope != "" {
		scopeQuery := bleve.NewTermQuery(scope)
		scopeQuery.SetField(bleveSemanticFieldScope)
		scopeEmptyQuery := bleve.NewBoolFieldQuery(true)
		scopeEmptyQuery.SetField(bleveSemanticFieldScopeEmpty)
		filters = append(filters, bleve.NewDisjunctionQuery(scopeQuery, scopeEmptyQuery))
	}
	if len(req.Kinds) > 0 {
		kindQueries := make([]query.Query, 0, len(req.Kinds))
		seen := make(map[Kind]struct{}, len(req.Kinds))
		for _, kind := range req.Kinds {
			if _, ok := seen[kind]; ok {
				continue
			}
			seen[kind] = struct{}{}
			kindQuery := bleve.NewTermQuery(string(kind))
			kindQuery.SetField(bleveSemanticFieldKind)
			kindQueries = append(kindQueries, kindQuery)
		}
		if len(kindQueries) > 0 {
			filters = append(filters, bleve.NewDisjunctionQuery(kindQueries...))
		}
	}
	if !req.IncludeRetired {
		activeQueries := []query.Query{}
		for _, status := range []Status{StatusVerified, StatusUnverified} {
			statusQuery := bleve.NewTermQuery(string(status))
			statusQuery.SetField(bleveSemanticFieldStatus)
			activeQueries = append(activeQueries, statusQuery)
		}
		filters = append(filters, bleve.NewDisjunctionQuery(activeQueries...))
	}
	if len(filters) == 0 {
		return nil
	}
	if len(filters) == 1 {
		return filters[0]
	}
	return bleve.NewConjunctionQuery(filters...)
}

func semanticHitFromBleveMatch(id string, fields map[string]interface{}, score float64, stage string, req SemanticSearchRequest) (SemanticHit, error) {
	if strings.TrimSpace(id) == "" {
		return SemanticHit{}, errors.New("semantic hit id is required")
	}
	if fields == nil {
		return SemanticHit{}, fmt.Errorf("semantic hit %q fields are required", id)
	}
	kind, err := bleveFieldString(fields, bleveSemanticFieldKind)
	if err != nil {
		return SemanticHit{}, fmt.Errorf("semantic hit %q: %w", id, err)
	}
	model, err := bleveFieldString(fields, bleveSemanticFieldModel)
	if err != nil {
		return SemanticHit{}, fmt.Errorf("semantic hit %q: %w", id, err)
	}
	dimensions, err := bleveFieldInt(fields, bleveSemanticFieldDimensions)
	if err != nil {
		return SemanticHit{}, fmt.Errorf("semantic hit %q: %w", id, err)
	}
	schema, err := bleveFieldString(fields, bleveSemanticFieldSchema)
	if err != nil {
		return SemanticHit{}, fmt.Errorf("semantic hit %q: %w", id, err)
	}
	if req.Model != "" && model != req.Model {
		return SemanticHit{}, fmt.Errorf("bleve semantic index model = %q, want %q; rebuild semantic index", model, req.Model)
	}
	if req.Dimensions > 0 && dimensions != req.Dimensions {
		return SemanticHit{}, fmt.Errorf("bleve semantic index dimensions = %d, want %d; rebuild semantic index", dimensions, req.Dimensions)
	}
	if schema != SemanticSchemaMemoryRecordsV1 {
		return SemanticHit{}, fmt.Errorf("bleve semantic index schema = %q, want %q; rebuild semantic index", schema, SemanticSchemaMemoryRecordsV1)
	}
	sourceRefs, err := bleveFieldStringSlice(fields, bleveSemanticFieldSourceRefs)
	if err != nil {
		return SemanticHit{}, fmt.Errorf("semantic hit %q: %w", id, err)
	}
	return SemanticHit{
		Ref:        id,
		Kind:       Kind(kind),
		Score:      score,
		Distance:   0,
		Stage:      stage,
		SourceRefs: sourceRefs,
	}, nil
}

func bleveFieldString(fields map[string]interface{}, key string) (string, error) {
	value, ok := fields[key]
	if !ok || value == nil {
		return "", fmt.Errorf("missing %q", key)
	}
	switch typed := value.(type) {
	case string:
		return typed, nil
	case []byte:
		return string(typed), nil
	default:
		return "", fmt.Errorf("field %q has type %T, want string", key, value)
	}
}

func bleveFieldInt(fields map[string]interface{}, key string) (int, error) {
	value, ok := fields[key]
	if !ok || value == nil {
		return 0, fmt.Errorf("missing %q", key)
	}
	switch typed := value.(type) {
	case int:
		return typed, nil
	case int32:
		return int(typed), nil
	case int64:
		return int(typed), nil
	case uint64:
		return int(typed), nil
	case float64:
		return int(typed), nil
	case float32:
		return int(typed), nil
	default:
		return 0, fmt.Errorf("field %q has type %T, want int", key, value)
	}
}

func bleveFieldStringSlice(fields map[string]interface{}, key string) ([]string, error) {
	value, ok := fields[key]
	if !ok || value == nil {
		return nil, nil
	}
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, fmt.Errorf("decode %q: %w", key, err)
	}
	return out, nil
}
