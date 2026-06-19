package memorymodule

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

func (s *LocalService) SearchSemantic(ctx context.Context, req SearchRequest) (*SearchResult, error) {
	runtime := s.semanticRuntimeSnapshot()
	if runtime == nil {
		return nil, fmt.Errorf("semantic search runtime is required")
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		result := &SearchResult{}
		if req.Explain {
			result.Explain = buildSearchExplain(req.Query, req.Scope, nil, nil)
		}
		return result, nil
	}
	limit, err := resolveLimit("search", req.Limit, defaultSearchLimit, maxSearchLimit)
	if err != nil {
		return nil, err
	}
	embedReq := EmbedRequest{Inputs: []EmbedInput{{Ref: "query", Text: query}}}
	embedResult, err := runtime.Embedder.Embed(ctx, embedReq)
	if err != nil {
		return nil, fmt.Errorf("embed semantic search query: %w", err)
	}
	if err := ValidateEmbedResult(embedReq, embedResult, runtime.Dimensions); err != nil {
		return nil, err
	}
	if embedResult.Model != runtime.Model {
		return nil, fmt.Errorf("semantic search embed model = %q, want %q", embedResult.Model, runtime.Model)
	}
	semanticResult, err := runtime.Index.Search(ctx, SemanticSearchRequest{
		Query:           query,
		Vector:          append([]float32(nil), embedResult.Vectors[0].Values...),
		Scope:           req.Scope,
		Kinds:           append([]Kind(nil), req.Kinds...),
		Limit:           limit + 8,
		IncludeInactive: req.IncludeInactive,
		IncludeRetired:  req.IncludeRetired,
		Mode:            runtime.Mode,
		Model:           runtime.Model,
		Dimensions:      runtime.Dimensions,
		Explain:         req.Explain,
	})
	if err != nil {
		return nil, fmt.Errorf("query semantic index: %w", err)
	}
	selection := RecordSelection{
		IncludeInactive: req.IncludeInactive,
		IncludeRetired:  req.IncludeRetired,
	}
	selectedRefs, err := s.selectedRecordRefs(ctx, selection)
	if err != nil {
		return nil, fmt.Errorf("select semantic search records: %w", err)
	}
	items, matchedRefs, err := s.searchItemsFromSemanticHits(ctx, semanticResult, req, selectedRefs)
	if err != nil {
		return nil, err
	}
	stages := []SearchStageExplain{{
		Name:           semanticStageForMode(runtime.Mode),
		CandidateCount: len(items),
	}}
	boosted, err := s.applySourceRefBoost(ctx, &items, matchedRefs, req.Scope, selectedRefs)
	if err != nil {
		return nil, err
	}
	if boosted > 0 {
		stages = append(stages, SearchStageExplain{Name: searchStageSourceRefBacklink, CandidateCount: boosted})
	}
	relationCounts, err := s.applyRelationBoost(ctx, &items, matchedRefs, req.Scope, selectedRefs)
	if err != nil {
		return nil, err
	}
	for _, stage := range []string{
		searchStageRelationSupports,
		searchStageRelationDerived,
		searchStageRelationSupersede,
		searchStageRelationContradict,
	} {
		if count := relationCounts[stage]; count > 0 {
			stages = append(stages, SearchStageExplain{Name: stage, CandidateCount: count})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		return items[i].Ref < items[j].Ref
	})
	if len(items) > limit {
		items = items[:limit]
	}
	result := &SearchResult{Items: items}
	if req.Explain {
		result.Explain = buildSearchExplain(req.Query, req.Scope, items, stages)
	}
	return result, nil
}

func (s *LocalService) searchItemsFromSemanticHits(ctx context.Context, result *SemanticSearchResult, req SearchRequest, selectedRefs map[string]struct{}) ([]SearchItem, []string, error) {
	if result == nil {
		return nil, nil, fmt.Errorf("semantic search result is required")
	}
	kindFilter := kindSet(req.Kinds)
	items := make([]SearchItem, 0, len(result.Hits))
	matchedRefs := make([]string, 0, len(result.Hits))
	for _, hit := range result.Hits {
		if strings.TrimSpace(hit.Ref) == "" {
			return nil, nil, fmt.Errorf("semantic hit ref is required")
		}
		record, err := s.GetRecordByRef(ctx, hit.Ref)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve semantic hit %q: %w", hit.Ref, err)
		}
		if _, ok := selectedRefs[record.Ref]; !ok {
			continue
		}
		if len(kindFilter) > 0 {
			if _, ok := kindFilter[record.Kind]; !ok {
				continue
			}
		}
		if !scopeMatches(req.Scope, record.Scope) {
			continue
		}
		score := hit.Score
		if score == 0 && hit.Distance > 0 {
			score = 1 / (1 + hit.Distance)
		}
		score += sourceStatusScore(*record)
		items = append(items, SearchItemFromRecord(*record, score))
		matchedRefs = append(matchedRefs, record.Ref)
	}
	return items, matchedRefs, nil
}

func semanticStageForMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "fts":
		return searchStageSemanticFTS
	case "hybrid":
		return searchStageSemanticHybrid
	default:
		return searchStageSemanticVector
	}
}
