package memorymodule

import (
	"context"
	"fmt"
	"strings"
)

func (s *LocalService) Search(ctx context.Context, req SearchRequest) (*SearchResult, error) {
	if s == nil {
		return nil, fmt.Errorf("memory service is nil")
	}
	return s.SearchSemantic(ctx, req)
}

func (s *LocalService) SetSemanticRuntime(opts SemanticRuntimeOptions) error {
	if s == nil {
		return fmt.Errorf("memory service is nil")
	}
	if opts.Index == nil {
		return fmt.Errorf("semantic runtime index is required")
	}
	if opts.Embedder == nil {
		return fmt.Errorf("semantic runtime embedder is required")
	}
	if strings.TrimSpace(opts.Model) == "" {
		return fmt.Errorf("semantic runtime model is required")
	}
	if opts.Dimensions <= 0 {
		return fmt.Errorf("semantic runtime dimensions must be > 0")
	}
	mode := strings.TrimSpace(opts.Mode)
	if mode == "" {
		mode = "hybrid"
	}
	clone := opts
	clone.Model = strings.TrimSpace(opts.Model)
	clone.Mode = mode
	s.mu.Lock()
	s.semanticRuntime = &clone
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
	if runtime == nil {
		return nil
	}
	return new(*runtime)
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
		Ref:          record.Ref,
		Kind:         string(record.Kind),
		Title:        record.Title,
		Status:       string(record.Status),
		Scope:        record.Scope,
		Tags:         append([]string(nil), record.Tags...),
		Origin:       record.Origin,
		TaskPattern:  record.TaskPattern,
		Path:         record.RelPath,
		Snippet:      snippet(record.Body),
		Score:        score,
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

const (
	searchStageSemanticUnwired    = "semantic_runtime_unwired"
	searchStageSourceRefBacklink  = "source_ref_backlink"
	searchStageSemanticVector     = "semantic_vector"
	searchStageSemanticFTS        = "semantic_fts"
	searchStageSemanticHybrid     = "semantic_hybrid"
	searchStageRelationSupports   = "relation_supports"
	searchStageRelationDerived    = "relation_derived_from"
	searchStageRelationSupersede  = "relation_supersedes"
	searchStageRelationContradict = "relation_contradicts"
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

func scopeMatches(requestScope string, itemScope string) bool {
	scope := strings.TrimSpace(requestScope)
	if scope == "" {
		return true
	}
	item := strings.TrimSpace(itemScope)
	return item == "" || item == scope
}
