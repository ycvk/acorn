package memorymodule

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Search runs semantic retrieval when an embedder+vector store are wired,
// otherwise falls back to simple keyword matching over record title/body/tags.
func (s *LocalService) Search(ctx context.Context, req SearchRequest) (*SearchResult, error) {
	if s == nil {
		return nil, fmt.Errorf("memory service is nil")
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

	runtime := s.semanticRuntimeSnapshot()
	if runtime != nil && runtime.Embedder != nil && runtime.VectorStore != nil {
		return s.searchByVector(ctx, req, query, limit, runtime)
	}
	return s.searchByKeyword(ctx, req, query, limit)
}

// searchByVector embeds the query and searches the vector store, then resolves
// hits to full records through the selection + scope filters.
func (s *LocalService) searchByVector(ctx context.Context, req SearchRequest, query string, limit int, runtime *SemanticRuntimeOptions) (*SearchResult, error) {
	embedReq := EmbedRequest{Inputs: []EmbedInput{{Ref: "query", Text: query}}}
	embedResult, err := runtime.Embedder.Embed(ctx, embedReq)
	if err != nil {
		return nil, fmt.Errorf("embed search query: %w", err)
	}
	if err := ValidateEmbedResult(embedReq, embedResult, runtime.Dimensions); err != nil {
		return nil, err
	}
	if embedResult.Model != runtime.Model {
		return nil, fmt.Errorf("search embed model = %q, want %q", embedResult.Model, runtime.Model)
	}
	hits, err := runtime.VectorStore.Search(ctx, embedResult.Vectors[0].Values, limit+8)
	if err != nil {
		return nil, fmt.Errorf("query vector store: %w", err)
	}

	selection := RecordSelection{
		IncludeInactive: req.IncludeInactive,
		IncludeRetired:  req.IncludeRetired,
	}
	selectedRefs, err := s.selectedRecordRefs(ctx, selection)
	if err != nil {
		return nil, fmt.Errorf("select search records: %w", err)
	}

	kindFilter := kindSet(req.Kinds)
	items := make([]SearchItem, 0, len(hits))
	for _, hit := range hits {
		if strings.TrimSpace(hit.Ref) == "" {
			continue
		}
		record, err := s.GetRecordByRef(ctx, hit.Ref)
		if err != nil {
			// A stale vector (record deleted from FS) is skipped, not fatal.
			continue
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
		score := hit.Score + sourceStatusScore(*record)
		items = append(items, SearchItemFromRecord(*record, score))
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
		result.Explain = buildSearchExplain(req.Query, req.Scope, items, []SearchStageExplain{
			{Name: searchStageSemanticVector, CandidateCount: len(items)},
		})
	}
	return result, nil
}

// searchByKeyword is the embedding-not-configured fallback: substring match
// over title/body/tags, scored by hit count and source status.
func (s *LocalService) searchByKeyword(ctx context.Context, req SearchRequest, query string, limit int) (*SearchResult, error) {
	records, err := s.allRecords(ctx)
	if err != nil {
		return nil, err
	}
	selection := RecordSelection{
		IncludeInactive: req.IncludeInactive,
		IncludeRetired:  req.IncludeRetired,
	}
	selectedRefs, err := s.selectedRecordRefs(ctx, selection)
	if err != nil {
		return nil, fmt.Errorf("select keyword search records: %w", err)
	}
	kindFilter := kindSet(req.Kinds)
	terms := strings.Fields(strings.ToLower(query))
	items := make([]SearchItem, 0)
	for _, record := range records {
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
		score := keywordScore(record, terms)
		if score <= 0 {
			continue
		}
		score += sourceStatusScore(record)
		items = append(items, SearchItemFromRecord(record, score))
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
		result.Explain = buildSearchExplain(req.Query, req.Scope, items, []SearchStageExplain{
			{Name: searchStageKeyword, CandidateCount: len(items)},
		})
	}
	return result, nil
}

// SetSemanticRuntime wires the optional embedder + vector store. Both must be
// non-nil when called; pass nil via the service constructor to disable.
func (s *LocalService) SetSemanticRuntime(opts SemanticRuntimeOptions) error {
	if s == nil {
		return fmt.Errorf("memory service is nil")
	}
	if opts.Embedder == nil {
		return fmt.Errorf("semantic runtime embedder is required")
	}
	if opts.VectorStore == nil {
		return fmt.Errorf("semantic runtime vector store is required")
	}
	if strings.TrimSpace(opts.Model) == "" {
		return fmt.Errorf("semantic runtime model is required")
	}
	if opts.Dimensions <= 0 {
		return fmt.Errorf("semantic runtime dimensions must be > 0")
	}
	s.mu.Lock()
	s.semanticRuntime = &opts
	s.mu.Unlock()
	return nil
}

func (s *LocalService) semanticRuntimeSnapshot() *SemanticRuntimeOptions {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	runtime := s.semanticRuntime
	s.mu.RUnlock()
	return runtime
}

func (s *LocalService) allRecords(ctx context.Context) ([]Record, error) {
	s.mu.RLock()
	idx := s.index
	s.mu.RUnlock()

	if idx == nil {
		return s.allRecordsFromFS(ctx)
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	records := make([]Record, 0, len(idx.byRef))
	for ref := range idx.byRef {
		record, err := s.GetRecordByRef(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("load indexed memory record %q: %w", ref, err)
		}
		records = append(records, *record)
	}
	return records, nil
}

func (s *LocalService) allRecordsFromFS(ctx context.Context) ([]Record, error) {
	facts, err := s.scanKindFromFS(ctx, KindFact, s.path("facts"))
	if err != nil {
		return nil, err
	}
	skills, err := s.scanKindFromFS(ctx, KindSkill, s.path("skills"))
	if err != nil {
		return nil, err
	}
	history, err := s.listHistory(ctx)
	if err != nil {
		return nil, err
	}
	records := make([]Record, 0, len(facts)+len(skills)+len(history))
	records = append(records, facts...)
	records = append(records, skills...)
	records = append(records, history...)
	return records, nil
}

func kindSet(kinds []Kind) map[Kind]struct{} {
	result := make(map[Kind]struct{}, len(kinds))
	for _, kind := range kinds {
		result[kind] = struct{}{}
	}
	return result
}

func SearchItemFromRecord(record Record, score float64) SearchItem {
	return SearchItem{
		Ref:         record.Ref,
		Kind:        string(record.Kind),
		Title:       record.Title,
		Status:      string(record.Status),
		Scope:       record.Scope,
		Tags:        append([]string(nil), record.Tags...),
		Origin:      record.Origin,
		TaskPattern: record.TaskPattern,
		Path:        record.RelPath,
		Snippet:     snippet(record.Body),
		Score:       score,
		SourceRun:   record.SourceRun,
		SourceRefs:  append([]string(nil), record.SourceRefs...),
		Created:     record.Created,
		Updated:     record.Updated,
	}
}

const (
	searchStageSemanticUnwired = "semantic_runtime_unwired"
	searchStageSemanticVector  = "semantic_vector"
	searchStageKeyword         = "keyword_match"
)

func buildSearchExplain(query string, scope string, items []SearchItem, stages []SearchStageExplain) *SearchExplain {
	explainItems := make([]SearchItemExplain, 0, len(items))
	for _, item := range items {
		explainItems = append(explainItems, SearchItemExplain{
			Ref:        item.Ref,
			FinalScore: item.Score,
		})
	}
	return &SearchExplain{
		Query:  strings.TrimSpace(query),
		Scope:  strings.TrimSpace(scope),
		Stages: append([]SearchStageExplain(nil), stages...),
		Items:  explainItems,
	}
}

func sourceStatusScore(record Record) float64 {
	var score float64
	if record.Status == StatusVerified {
		score += 0.5
	}
	if record.Kind == KindSkill {
		score += 0.25
	}
	return score
}

// keywordScore counts how many query terms appear in the record's title, body,
// or tags (case-insensitive). Each term in the title counts double.
func keywordScore(record Record, terms []string) float64 {
	if len(terms) == 0 {
		return 0
	}
	title := strings.ToLower(record.Title)
	body := strings.ToLower(record.Body)
	tagSet := make(map[string]struct{}, len(record.Tags))
	for _, tag := range record.Tags {
		tagSet[tag] = struct{}{}
	}
	var score float64
	for _, term := range terms {
		if strings.Contains(title, term) {
			score += 2
		}
		if strings.Contains(body, term) {
			score += 1
		}
		if _, ok := tagSet[term]; ok {
			score += 1.5
		}
	}
	return score
}

func scopeMatches(requestScope string, itemScope string) bool {
	scope := strings.TrimSpace(requestScope)
	if scope == "" {
		return true
	}
	item := strings.TrimSpace(itemScope)
	return item == "" || item == scope
}
