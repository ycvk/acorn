package memorymodule

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// IndexedRecord holds metadata for a memory record without the full body.
// This is kept in memory for fast O(1) record lookups and skill tree projections.
type IndexedRecord struct {
	Ref         string
	Kind        Kind
	Title       string
	Status      Status
	Scope       string
	Tags        []string
	TaskPattern string
	SourceRun   string
	SourceRefs  []string
	RelPath     string
	FilePath    string
	BodySize    int64
	BodySnippet string
	ModTime     time.Time
}

// MemoryIndex maintains an in-memory index of all memory records.
// It is rebuilt on startup and incrementally updated on mutations.
type MemoryIndex struct {
	mu        sync.RWMutex
	byRef     map[string]*IndexedRecord
	byKind    map[Kind][]*IndexedRecord
	byTag     map[string][]*IndexedRecord
	byScope   map[string][]*IndexedRecord
	modified  map[string]time.Time
	skillTree *SkillTreeIndex
}

// SkillTreeIndex provides a two-level hierarchical index of skills.
type SkillTreeIndex struct {
	Categories map[string]*SkillCategory
}

// SkillCategory groups skills by domain.
type SkillCategory struct {
	Name        string
	Description string
	UsageCount  int
	Skills      map[string]*SkillTreeEntry
}

// SkillTreeEntry represents a skill in the tree without its full body.
type SkillTreeEntry struct {
	Ref      string
	Title    string
	Tags     []string
	Status   Status
	RelPath  string
	FilePath string
	BodySize int64
}

func newMemoryIndex() *MemoryIndex {
	return &MemoryIndex{
		byRef:    make(map[string]*IndexedRecord),
		byKind:   make(map[Kind][]*IndexedRecord),
		byTag:    make(map[string][]*IndexedRecord),
		byScope:  make(map[string][]*IndexedRecord),
		modified: make(map[string]time.Time),
		skillTree: &SkillTreeIndex{
			Categories: make(map[string]*SkillCategory),
		},
	}
}

// BuildIndex rebuilds the full index from the filesystem.
// Call once during service initialization.
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
			info, infoErr := entry.Info()
			if infoErr != nil {
				return fmt.Errorf("stat %s: %w", p, infoErr)
			}
			var record *Record
			if kinds[i] == KindHistory {
				record = readHistoryRecordForIndex(s.root, p)
				if record == nil {
					return fmt.Errorf("read history record %s", p)
				}
			} else {
				var readErr error
				record, readErr = readMemoryRecord(s.root, kinds[i], p)
				if readErr != nil {
					return fmt.Errorf("read %s record %s: %w", kinds[i], p, readErr)
				}
			}
			idx.add(record, info.Size(), info.ModTime())
			indexedRecords = append(indexedRecords, cloneRecord(*record))
			return nil
		})
		if err != nil {
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

	record, err := readMemoryRecord(s.root, indexed.Kind, indexed.FilePath)
	if err != nil {
		return nil, fmt.Errorf("read indexed memory record %q: %w", ref, err)
	}
	return record, nil
}

// getRecordByRefFromFS performs a filesystem lookup when the index is not available.
func (s *LocalService) getRecordByRefFromFS(ctx context.Context, ref string) (*Record, error) {
	// Check history first
	records, err := s.listHistory(ctx)
	if err != nil {
		return nil, fmt.Errorf("list history for ref lookup: %w", err)
	}
	for _, r := range records {
		if r.Ref == ref {
			return &r, nil
		}
	}

	// Search facts and skills directories
	kinds := []Kind{KindFact, KindSkill}
	paths := []string{s.path("facts"), s.path("skills")}
	for i, root := range paths {
		if _, statErr := os.Stat(root); os.IsNotExist(statErr) {
			continue
		}
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
			record, readErr := readMemoryRecord(s.root, kinds[i], p)
			if readErr != nil {
				return readErr
			}
			if record.Ref == ref {
				found = record
				return filepath.SkipAll
			}
			return nil
		})
		if err != nil && !errors.Is(err, filepath.SkipAll) {
			return nil, fmt.Errorf("search %s for ref %q: %w", kinds[i], ref, err)
		}
		if found != nil {
			return found, nil
		}
	}

	return nil, fmt.Errorf("memory record %q not found on filesystem", ref)
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

// --- internal index operations ---

func (idx *MemoryIndex) add(record *Record, bodySize int64, modTime time.Time) {
	rel := record.RelPath
	if record.RootPath != "" && record.Ref != "" {
		computed, err := filepath.Rel(record.RootPath, record.Ref)
		if err == nil && computed != "" {
			rel = computed
		}
	}
	snippetText := snippet(record.Body)

	indexed := &IndexedRecord{
		Ref:         record.Ref,
		Kind:        record.Kind,
		Title:       record.Title,
		Status:      record.Status,
		Scope:       record.Scope,
		Tags:        append([]string(nil), record.Tags...),
		TaskPattern: record.TaskPattern,
		SourceRun:   record.SourceRun,
		SourceRefs:  append([]string(nil), record.SourceRefs...),
		RelPath:     rel,
		FilePath:    filepath.Join(record.RootPath, rel),
		BodySize:    bodySize,
		BodySnippet: snippetText,
		ModTime:     modTime,
	}

	idx.byRef[record.Ref] = indexed
	idx.byKind[record.Kind] = append(idx.byKind[record.Kind], indexed)
	for _, tag := range record.Tags {
		t := strings.ToLower(strings.TrimSpace(tag))
		if t != "" {
			idx.byTag[t] = append(idx.byTag[t], indexed)
		}
	}
	sc := strings.ToLower(strings.TrimSpace(record.Scope))
	if sc != "" {
		idx.byScope[sc] = append(idx.byScope[sc], indexed)
	}
	idx.modified[record.Ref] = modTime

}

func (idx *MemoryIndex) rebuildSkillTree(records []Record) error {
	idx.skillTree = &SkillTreeIndex{Categories: make(map[string]*SkillCategory)}
	selected, err := SelectRecords(records, RecordSelection{})
	if err != nil {
		return fmt.Errorf("build memory skill tree: %w", err)
	}
	for _, record := range selected {
		if record.Kind != KindSkill {
			continue
		}
		idx.addToSkillTree(&record, int64(len(record.Body)))
	}
	return nil
}

func (idx *MemoryIndex) addToSkillTree(record *Record, bodySize int64) {
	if record.Status == "retired" {
		return
	}
	category := deriveSkillCategory(record)

	cat, ok := idx.skillTree.Categories[category]
	if !ok {
		cat = &SkillCategory{
			Name:        category,
			Description: "",
			UsageCount:  0,
			Skills:      make(map[string]*SkillTreeEntry),
		}
		idx.skillTree.Categories[category] = cat
	}

	slug := strings.ToLower(strings.TrimSpace(record.Title))
	if slug == "" {
		slug = record.Ref
	}

	cat.Skills[slug] = &SkillTreeEntry{
		Ref:      record.Ref,
		Title:    record.Title,
		Tags:     append([]string(nil), record.Tags...),
		Status:   record.Status,
		RelPath:  record.RelPath,
		FilePath: filepath.Join(record.RootPath, record.RelPath),
		BodySize: bodySize,
	}
}

func deriveSkillCategory(record *Record) string {
	if record.TaskPattern != "" {
		fields := strings.FieldsFunc(strings.ToLower(record.TaskPattern), func(r rune) bool {
			return r == ',' || r == ' ' || r == '-'
		})
		for _, f := range fields {
			trimmed := strings.TrimSpace(f)
			if trimmed != "" {
				return trimmed
			}
		}
	}
	if len(record.Tags) > 0 {
		cat := strings.ToLower(strings.TrimSpace(record.Tags[0]))
		if cat != "" {
			return cat
		}
	}
	return "general"
}

func (st *SkillTreeIndex) clone() *SkillTreeIndex {
	if st == nil {
		return nil
	}
	clone := &SkillTreeIndex{
		Categories: make(map[string]*SkillCategory, len(st.Categories)),
	}
	for name, cat := range st.Categories {
		catClone := &SkillCategory{
			Name:        cat.Name,
			Description: cat.Description,
			UsageCount:  cat.UsageCount,
			Skills:      make(map[string]*SkillTreeEntry, len(cat.Skills)),
		}
		for slug, entry := range cat.Skills {
			catClone.Skills[slug] = &SkillTreeEntry{
				Ref:      entry.Ref,
				Title:    entry.Title,
				Tags:     append([]string(nil), entry.Tags...),
				Status:   entry.Status,
				RelPath:  entry.RelPath,
				FilePath: entry.FilePath,
				BodySize: entry.BodySize,
			}
		}
		clone.Categories[name] = catClone
	}
	return clone
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
		for _, sourceRef := range record.SourceRefs {
			target, err := s.GetRecordByRef(ctx, sourceRef)
			if err != nil {
				return 0, fmt.Errorf("resolve source-ref boost %q -> %q: %w", matchedRef, sourceRef, err)
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
	}
	return len(seenTargets), nil
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
		for _, relation := range record.Relations {
			stage, delta, err := relationBoost(relation.Type)
			if err != nil {
				return nil, err
			}
			target, err := s.GetRecordByRef(ctx, relation.Target)
			if err != nil {
				return nil, fmt.Errorf("resolve relation boost %q -> %q: %w", matchedRef, relation.Target, err)
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
	}
	return counts, nil
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
