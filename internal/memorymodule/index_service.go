package memorymodule

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func (s *LocalService) BuildIndex(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("memory service is nil")
	}
	idx := newMemoryIndex()
	indexedRecords := make([]Record, 0)

	kinds := []Kind{KindFact, KindSkill, KindHistory}
	paths := []string{s.path("facts"), s.path("skills"), s.path("history")}

	for i, root := range paths {
		if _, statErr := os.Stat(root); os.IsNotExist(statErr) {
			continue
		}
		if err := s.indexWalkKind(ctx, root, kinds[i], idx, &indexedRecords); err != nil {
			return fmt.Errorf("index %s: %w", kinds[i], err)
		}
	}
	if err := idx.rebuildSkillTree(indexedRecords); err != nil {
		return err
	}

	s.mu.Lock()
	s.index = idx
	s.mu.Unlock()
	return nil
}

func (s *LocalService) indexWalkKind(ctx context.Context, root string, kind Kind, idx *MemoryIndex, indexedRecords *[]Record) error {
	return filepath.WalkDir(root, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Ext(p) != ".md" {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return fmt.Errorf("stat %s: %w", p, infoErr)
		}
		record, err := s.readRecordForIndex(kind, p)
		if err != nil {
			return fmt.Errorf("read %s record %s: %w", kind, p, err)
		}
		idx.add(record, info.Size(), info.ModTime())
		*indexedRecords = append(*indexedRecords, cloneRecord(*record))
		return nil
	})
}

func (s *LocalService) readRecordForIndex(kind Kind, p string) (*Record, error) {
	if kind == KindHistory {
		record := readHistoryRecordForIndex(s.root, p)
		if record == nil {
			return nil, fmt.Errorf("read history record %s", p)
		}
		return record, nil
	}
	return readMemoryRecord(s.root, kind, p)
}

// GetRecordByRef reads a full Record from disk using the index to find the file path.
func (s *LocalService) GetRecordByRef(ctx context.Context, ref string) (*Record, error) {
	s.mu.RLock()
	idx := s.index
	s.mu.RUnlock()

	if idx == nil {
		return s.getRecordByRefFromFS(ctx, ref)
	}

	idx.mu.RLock()
	indexed, ok := idx.byRef[ref]
	idx.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("memory record %q not found in index", ref)
	}

	if indexed.Kind == KindHistory {
		return s.findHistoryRecordByRef(ctx, ref)
	}

	record, err := readMemoryRecord(s.root, indexed.Kind, indexed.FilePath)
	if err != nil {
		return nil, fmt.Errorf("read indexed memory record %q: %w", ref, err)
	}
	return record, nil
}

func (s *LocalService) findHistoryRecordByRef(ctx context.Context, ref string) (*Record, error) {
	records, err := s.listHistory(ctx)
	if err != nil {
		return nil, fmt.Errorf("read indexed history record %q: %w", ref, err)
	}
	for _, r := range records {
		if r.Ref == ref {
			return &r, nil
		}
	}
	return nil, fmt.Errorf("memory record %q not found in history", ref)
}

// getRecordByRefFromFS performs a filesystem lookup when the index is not available.
func (s *LocalService) getRecordByRefFromFS(ctx context.Context, ref string) (*Record, error) {
	records, err := s.listHistory(ctx)
	if err != nil {
		return nil, fmt.Errorf("list history for ref lookup: %w", err)
	}
	for _, r := range records {
		if r.Ref == ref {
			return &r, nil
		}
	}

	kinds := []Kind{KindFact, KindSkill}
	paths := []string{s.path("facts"), s.path("skills")}
	for i, root := range paths {
		if _, statErr := os.Stat(root); os.IsNotExist(statErr) {
			continue
		}
		found, err := s.findRecordInDir(ctx, root, kinds[i], ref)
		if err != nil && !errors.Is(err, filepath.SkipAll) {
			return nil, fmt.Errorf("search %s for ref %q: %w", kinds[i], ref, err)
		}
		if found != nil {
			return found, nil
		}
	}

	return nil, fmt.Errorf("memory record %q not found on filesystem", ref)
}

func (s *LocalService) findRecordInDir(ctx context.Context, root string, kind Kind, ref string) (*Record, error) {
	var found *Record
	err := filepath.WalkDir(root, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Ext(p) != ".md" {
			return nil
		}
		record, readErr := readMemoryRecord(s.root, kind, p)
		if readErr != nil {
			return readErr
		}
		if record.Ref == ref {
			found = record
			return filepath.SkipAll
		}
		return nil
	})
	return found, err
}

// GetSkillTree returns the skill tree index.
func (s *LocalService) GetSkillTree() *SkillTreeIndex {
	s.mu.RLock()
	idx := s.index
	s.mu.RUnlock()

	if idx == nil {
		return nil
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.skillTree.clone()
}

func (s *LocalService) applySourceRefBoost(ctx context.Context, items *[]SearchItem, matchedRefs []string, scope string, selectedRefs map[string]struct{}) (int, error) {
	if len(matchedRefs) == 0 {
		return 0, nil
	}
	byRef := make(map[string]int, len(*items))
	for i, item := range *items {
		byRef[item.Ref] = i
	}
	seenTargets := make(map[string]struct{})
	for _, matchedRef := range matchedRefs {
		record, err := s.GetRecordByRef(ctx, matchedRef)
		if err != nil {
			return 0, fmt.Errorf("load source-ref boost record %q: %w", matchedRef, err)
		}
		if err := s.applySourceRefRecord(ctx, record, items, byRef, scope, selectedRefs, seenTargets, matchedRef); err != nil {
			return 0, err
		}
	}
	return len(seenTargets), nil
}

func (s *LocalService) applySourceRefRecord(ctx context.Context, record *Record, items *[]SearchItem, byRef map[string]int, scope string, selectedRefs map[string]struct{}, seenTargets map[string]struct{}, matchedRef string) error {
	for _, sourceRef := range record.SourceRefs {
		target, err := s.GetRecordByRef(ctx, sourceRef)
		if err != nil {
			return fmt.Errorf("resolve source-ref boost %q -> %q: %w", matchedRef, sourceRef, err)
		}
		if _, ok := selectedRefs[target.Ref]; !ok {
			continue
		}
		if !scopeMatches(scope, target.Scope) {
			continue
		}
		delta := 2.0
		if index, exists := byRef[target.Ref]; exists {
			(*items)[index].Score += delta
		} else {
			*items = append(*items, SearchItemFromRecord(*target, delta))
			byRef[target.Ref] = len(*items) - 1
		}
		seenTargets[target.Ref] = struct{}{}
	}
	return nil
}

func (s *LocalService) applyRelationBoost(ctx context.Context, items *[]SearchItem, matchedRefs []string, scope string, selectedRefs map[string]struct{}) (map[string]int, error) {
	counts := make(map[string]int)
	if len(matchedRefs) == 0 {
		return counts, nil
	}
	byRef := make(map[string]int, len(*items))
	for i, item := range *items {
		byRef[item.Ref] = i
	}
	seen := make(map[string]struct{})
	for _, matchedRef := range matchedRefs {
		record, err := s.GetRecordByRef(ctx, matchedRef)
		if err != nil {
			return nil, fmt.Errorf("load relation boost record %q: %w", matchedRef, err)
		}
		if err := s.applyRelationRecord(ctx, record, items, byRef, scope, selectedRefs, seen, counts, matchedRef); err != nil {
			return nil, err
		}
	}
	return counts, nil
}

func (s *LocalService) applyRelationRecord(ctx context.Context, record *Record, items *[]SearchItem, byRef map[string]int, scope string, selectedRefs map[string]struct{}, seen map[string]struct{}, counts map[string]int, matchedRef string) error {
	for _, relation := range record.Relations {
		stage, delta, err := relationBoost(relation.Type)
		if err != nil {
			return err
		}
		target, err := s.GetRecordByRef(ctx, relation.Target)
		if err != nil {
			return fmt.Errorf("resolve relation boost %q -> %q: %w", matchedRef, relation.Target, err)
		}
		if _, ok := selectedRefs[target.Ref]; !ok {
			continue
		}
		if !scopeMatches(scope, target.Scope) {
			continue
		}
		key := stage + "\x00" + matchedRef + "\x00" + target.Ref
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if index, exists := byRef[target.Ref]; exists {
			(*items)[index].Score += delta
		} else {
			*items = append(*items, SearchItemFromRecord(*target, delta))
			byRef[target.Ref] = len(*items) - 1
		}
		counts[stage]++
	}
	return nil
}

func relationBoost(relationType RelationType) (string, float64, error) {
	switch relationType {
	case RelationSupports:
		return searchStageRelationSupports, 1.5, nil
	case RelationDerivedFrom:
		return searchStageRelationDerived, 1.25, nil
	case RelationSupersedes:
		return searchStageRelationSupersede, 0.75, nil
	case RelationContradicts:
		return searchStageRelationContradict, 0.5, nil
	default:
		return "", 0, fmt.Errorf("unsupported relation type %q", relationType)
	}
}

func readHistoryRecordForIndex(root string, p string) *Record {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	info, err := os.Stat(p)
	if err != nil {
		return nil
	}
	record, err := historyRecordFromFile(root, p, data, info.ModTime())
	if err != nil {
		return nil
	}
	return record
}
