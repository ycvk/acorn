package memory

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
