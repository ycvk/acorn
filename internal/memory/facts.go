package memory

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
)

func (s *LocalService) ListFacts(ctx context.Context, selection RecordSelection) ([]Record, error) {
	if s == nil {
		return nil, fmt.Errorf("memory service is nil")
	}
	records, err := s.listRecordsByKindFromIndex(ctx, KindFact)
	if err != nil {
		return nil, err
	}
	return s.selectRecords(ctx, records, selection)
}

func (s *LocalService) ListSkills(ctx context.Context, selection RecordSelection) ([]Record, error) {
	if s == nil {
		return nil, fmt.Errorf("memory service is nil")
	}
	records, err := s.listRecordsByKindFromIndex(ctx, KindSkill)
	if err != nil {
		return nil, err
	}
	return s.selectRecords(ctx, records, selection)
}

func (s *LocalService) listRecordsByKindFromIndex(ctx context.Context, kind Kind) ([]Record, error) {
	s.mu.RLock()
	idx := s.index
	s.mu.RUnlock()

	if idx == nil {
		return s.scanKindFromFS(ctx, kind, s.path(string(kind)+"s"))
	}

	idx.mu.RLock()
	indexed := idx.byKind[kind]
	idx.mu.RUnlock()

	if len(indexed) == 0 {
		return s.scanKindFromFS(ctx, kind, s.path(string(kind)+"s"))
	}

	records := make([]Record, 0, len(indexed))
	for _, rec := range indexed {
		record, err := readMemoryRecord(s.root, kind, rec.FilePath)
		if err != nil {
			return nil, fmt.Errorf("read indexed %s record %q from %q: %w", kind, rec.Ref, rec.FilePath, err)
		}
		records = append(records, *record)
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].Ref < records[j].Ref
	})
	return records, nil
}

func (s *LocalService) scanKindFromFS(ctx context.Context, kind Kind, root string) ([]Record, error) {
	records := make([]Record, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		record, err := readMemoryRecord(s.root, kind, path)
		if err != nil {
			return err
		}
		records = append(records, *record)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s memory files: %w", kind, err)
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].Ref < records[j].Ref
	})
	return records, nil
}
