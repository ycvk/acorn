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
	clone := *runtime
	return &clone
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

func queryTerms(query string) []string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && !(r >= '\u4e00' && r <= '\u9fff')
	})
	result := make([]string, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		trimmed := strings.TrimSpace(field)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func kindSet(kinds []Kind) map[Kind]struct{} {
	result := make(map[Kind]struct{}, len(kinds))
	for _, kind := range kinds {
		result[kind] = struct{}{}
	}
	return result
}
