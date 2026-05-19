package memorymodule

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

func (s *LocalService) PlanMemoryMutation(ctx context.Context, req PlanMemoryMutationRequest) (*MemoryMutationPlan, error) {
	if s == nil {
		return nil, fmt.Errorf("memory service is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	relPath, kind, err := normalizeMemoryMutationPath(req.Path)
	if err != nil {
		return rejectMemoryMutation(req.Path, "", "", "", err.Error()), nil
	}
	if strings.TrimSpace(req.Content) == "" {
		return rejectMemoryMutation(relPath, "", "", kind, "memory mutation content is required"), nil
	}
	absPath := filepath.Join(s.root, filepath.FromSlash(relPath))
	proposed, err := parseMemoryRecord(s.root, kind, absPath, req.Content)
	if err != nil {
		return rejectMemoryMutation(relPath, "", "", kind, err.Error()), nil
	}
	proposed.RootPath = s.root
	proposed.RelPath = relPath
	proposed.Ref = relPath + "#" + anchor(proposed.Title)

	existing, exists, err := s.existingMemoryMutationRecord(absPath, kind)
	if err != nil {
		return nil, err
	}
	records, err := s.allRecordsFromFS(ctx)
	if err != nil {
		return rejectMemoryMutation(relPath, proposed.Ref, existingRef(existing), kind, fmt.Sprintf("load existing memory records: %v", err)), nil
	}
	records = replaceRecordForMutation(records, existing, exists, *proposed)
	if duplicate := firstDuplicateRecordRef(records); duplicate != "" {
		return rejectMemoryMutation(relPath, proposed.Ref, existingRef(existing), kind, fmt.Sprintf("memory record ref %q is duplicated", duplicate)), nil
	}
	if _, err := SelectRecords(records, RecordSelection{IncludeRetired: true}); err != nil {
		return rejectMemoryMutation(relPath, proposed.Ref, existingRef(existing), kind, err.Error()), nil
	}

	if !exists {
		if proposed.Status == StatusRetired {
			return rejectMemoryMutation(relPath, proposed.Ref, "", kind, "cannot create a retired memory record without an existing record"), nil
		}
		if duplicate := findRecordByRef(records, proposed.Ref); duplicate != nil && duplicate.RelPath != proposed.RelPath {
			return rejectMemoryMutation(relPath, proposed.Ref, duplicate.Ref, kind, fmt.Sprintf("memory record ref %q already exists at %s", proposed.Ref, duplicate.RelPath)), nil
		}
		return &MemoryMutationPlan{
			Action: MemoryMutationCreate,
			Path:   relPath,
			Ref:    proposed.Ref,
			Kind:   kind,
			Reason: "new valid memory record",
		}, nil
	}

	if proposed.Ref != existing.Ref {
		return rejectMemoryMutation(relPath, proposed.Ref, existing.Ref, kind, fmt.Sprintf("memory replacement changes ref from %q to %q", existing.Ref, proposed.Ref)), nil
	}
	if recordsEquivalent(*existing, *proposed) {
		return &MemoryMutationPlan{
			Action:      MemoryMutationNoopDuplicate,
			Path:        relPath,
			Ref:         proposed.Ref,
			ExistingRef: existing.Ref,
			Kind:        kind,
			Reason:      "proposed memory record is equivalent to existing record",
		}, nil
	}
	action := MemoryMutationReplaceExisting
	reason := "valid replacement for existing memory record"
	if proposed.Status == StatusRetired {
		action = MemoryMutationRetireExisting
		reason = "valid retirement for existing memory record"
	}
	return &MemoryMutationPlan{
		Action:      action,
		Path:        relPath,
		Ref:         proposed.Ref,
		ExistingRef: existing.Ref,
		Kind:        kind,
		Reason:      reason,
	}, nil
}

func normalizeMemoryMutationPath(path string) (string, Kind, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", "", fmt.Errorf("memory mutation path is required")
	}
	if filepath.IsAbs(trimmed) {
		return "", "", fmt.Errorf("memory mutation path must be relative")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(trimmed)))
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", "", fmt.Errorf("memory mutation path must stay inside the memory root")
	}
	if filepath.Ext(clean) != ".md" {
		return "", "", fmt.Errorf("memory mutation path must target a markdown file")
	}
	switch {
	case strings.HasPrefix(clean, "facts/"):
		return clean, KindFact, nil
	case strings.HasPrefix(clean, "skills/"):
		return clean, KindSkill, nil
	default:
		return clean, "", fmt.Errorf("memory mutation path must be under facts/ or skills/")
	}
}

func (s *LocalService) existingMemoryMutationRecord(absPath string, kind Kind) (*Record, bool, error) {
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("stat memory mutation target %s: %w", absPath, err)
	}
	if info.IsDir() {
		return nil, false, fmt.Errorf("memory mutation target is a directory: %s", absPath)
	}
	existing, err := readMemoryRecord(s.root, kind, absPath)
	if err != nil {
		return nil, false, fmt.Errorf("read existing memory mutation target: %w", err)
	}
	return existing, true, nil
}

func replaceRecordForMutation(records []Record, existing *Record, exists bool, proposed Record) []Record {
	out := make([]Record, 0, len(records)+1)
	replaced := false
	for _, record := range records {
		if exists && record.Ref == existing.Ref {
			if !replaced {
				out = append(out, proposed)
				replaced = true
			}
			continue
		}
		out = append(out, record)
	}
	if !exists || !replaced {
		out = append(out, proposed)
	}
	return out
}

func findRecordByRef(records []Record, ref string) *Record {
	for i := range records {
		if records[i].Ref == ref {
			return &records[i]
		}
	}
	return nil
}

func firstDuplicateRecordRef(records []Record) string {
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		ref := strings.TrimSpace(record.Ref)
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			return ref
		}
		seen[ref] = struct{}{}
	}
	return ""
}

func recordsEquivalent(left Record, right Record) bool {
	return reflect.DeepEqual(canonicalComparableRecord(left), canonicalComparableRecord(right))
}

type comparableRecord struct {
	Ref          string
	Kind         Kind
	RelPath      string
	Title        string
	Status       Status
	Scope        string
	Tags         []string
	Origin       string
	TaskPattern  string
	SourceRefs   []string
	EvidenceRefs []string
	Relations    []RecordRelation
	Body         string
	Created      string
	Updated      string
	ValidFrom    string
	ValidUntil   string
	SourceRun    string
}

func canonicalComparableRecord(record Record) comparableRecord {
	return comparableRecord{
		Ref:          strings.TrimSpace(record.Ref),
		Kind:         record.Kind,
		RelPath:      strings.TrimSpace(record.RelPath),
		Title:        strings.TrimSpace(record.Title),
		Status:       record.Status,
		Scope:        strings.TrimSpace(record.Scope),
		Tags:         append([]string(nil), record.Tags...),
		Origin:       strings.TrimSpace(record.Origin),
		TaskPattern:  strings.TrimSpace(record.TaskPattern),
		SourceRefs:   append([]string(nil), record.SourceRefs...),
		EvidenceRefs: append([]string(nil), record.EvidenceRefs...),
		Relations:    append([]RecordRelation(nil), record.Relations...),
		Body:         strings.TrimSpace(record.Body),
		Created:      strings.TrimSpace(record.Created),
		Updated:      strings.TrimSpace(record.Updated),
		ValidFrom:    strings.TrimSpace(record.ValidFrom),
		ValidUntil:   strings.TrimSpace(record.ValidUntil),
		SourceRun:    strings.TrimSpace(record.SourceRun),
	}
}

func existingRef(record *Record) string {
	if record == nil {
		return ""
	}
	return record.Ref
}

func rejectMemoryMutation(path string, ref string, existingRef string, kind Kind, reason string) *MemoryMutationPlan {
	return &MemoryMutationPlan{
		Action:      MemoryMutationRejectInvalid,
		Path:        strings.TrimSpace(path),
		Ref:         strings.TrimSpace(ref),
		ExistingRef: strings.TrimSpace(existingRef),
		Kind:        kind,
		Reason:      strings.TrimSpace(reason),
	}
}
