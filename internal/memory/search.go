package memory

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// Search runs hybrid retrieval: keyword matching always, plus a vector KNN
// query when embedding is configured. Results are fused via Reciprocal Rank
// Fusion (RRF, k=60). When embedding is not configured or fails, the result
// is keyword-only — the pre-existing behavior.
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

	// Keyword path is always available.
	keywordResult, err := s.searchByKeyword(ctx, req, query, limit)
	if err != nil {
		return nil, err
	}
	keywordItems := keywordResult.Items

	// Vector path is optional. When unavailable, fuse returns keyword as-is.
	var vectorItems []SearchItem
	if s.embedding != nil && s.embedding.Enabled() && s.vectors != nil {
		vectorItems = s.vectorSearch(ctx, req, query, limit)
	}

	items := fuseResults(keywordItems, vectorItems, limit)

	result := &SearchResult{Items: items}
	if req.Explain {
		stages := []SearchStageExplain{
			{Name: searchStageKeyword, CandidateCount: len(keywordItems)},
		}
		if vectorItems != nil {
			stages = append(stages, SearchStageExplain{
				Name:           searchStageVector,
				CandidateCount: len(vectorItems),
			})
		}
		result.Explain = buildSearchExplain(query, req.Scope, items, stages)
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
	searchStageKeyword = "keyword_match"
	searchStageVector  = "vector_knn"
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

// vectorSearch embeds the query and runs a KNN search against the vector
// index. Errors are logged and nil is returned (fuse degrades to keyword-only).
func (s *LocalService) vectorSearch(ctx context.Context, req SearchRequest, query string, limit int) []SearchItem {
	embedding, err := s.embedding.Embed(ctx, query)
	if err != nil {
		slog.Warn("vector search: query embedding failed", "err", err)
		return nil
	}
	kinds := make([]string, 0, len(req.Kinds))
	for _, k := range req.Kinds {
		kinds = append(kinds, string(k))
	}
	matches, err := s.vectors.SearchByVector(ctx, embedding, limit, kinds, req.Scope)
	if err != nil {
		slog.Warn("vector search: KNN query failed", "err", err)
		return nil
	}
	items := make([]SearchItem, 0, len(matches))
	for _, m := range matches {
		items = append(items, SearchItem{
			Ref:     m.Ref,
			Kind:    m.Kind,
			Title:   m.Title,
			Scope:   m.Scope,
			Snippet: m.Title,
		})
	}
	return items
}

// rrfK is the standard Reciprocal Rank Fusion constant (k=60, TREC paper).
const rrfK = 60

// fuseResults merges keyword and vector ranked lists via RRF. Each list
// contributes 1/(k+rank+1) per item; items in both lists get the sum. When
// either list is empty, the other is returned unchanged.
func fuseResults(keywordItems, vectorItems []SearchItem, limit int) []SearchItem {
	if len(vectorItems) == 0 {
		if len(keywordItems) > limit {
			return keywordItems[:limit]
		}
		return keywordItems
	}
	if len(keywordItems) == 0 {
		if len(vectorItems) > limit {
			return vectorItems[:limit]
		}
		return vectorItems
	}

	type rrfEntry struct {
		item     SearchItem
		rrfScore float64
	}
	merged := make(map[string]*rrfEntry, len(keywordItems)+len(vectorItems))

	addList := func(items []SearchItem) {
		for rank, item := range items {
			entry, ok := merged[item.Ref]
			if !ok {
				entry = &rrfEntry{item: item}
				merged[item.Ref] = entry
			}
			entry.rrfScore += 1.0 / float64(rrfK+rank+1)
		}
	}
	addList(keywordItems)
	addList(vectorItems)

	result := make([]SearchItem, 0, len(merged))
	for _, entry := range merged {
		entry.item.Score = entry.rrfScore
		result = append(result, entry.item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		return result[i].Ref < result[j].Ref
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}
