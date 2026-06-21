package memorymodule

import (
	"fmt"
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
