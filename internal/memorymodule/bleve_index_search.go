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
	"github.com/blevesearch/bleve/v2/search/query"
)

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

	searchReq, err := i.buildSearchRequest(req, limit)
	if err != nil {
		return nil, err
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

func (i *bleveSemanticIndex) buildSearchRequest(req SemanticSearchRequest, limit int) (*bleve.SearchRequest, error) {
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
	return searchReq, nil
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
