package memorymodule

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type RecordSelection struct {
	IncludeInactive bool
	IncludeRetired  bool
	Now             time.Time
}

func SelectRecords(records []Record, selection RecordSelection) ([]Record, error) {
	normalized := normalizeRecordSelection(selection)
	if len(records) == 0 {
		return nil, nil
	}
	byRef := make(map[string]Record, len(records))
	for _, record := range records {
		if strings.TrimSpace(record.Ref) == "" {
			return nil, fmt.Errorf("memory record ref is required")
		}
		byRef[record.Ref] = record
	}
	if err := validateRelationTargets(byRef); err != nil {
		return nil, err
	}
	if normalized.IncludeRetired {
		return cloneAndSortRecords(records), nil
	}
	if normalized.IncludeInactive {
		selected := make([]Record, 0, len(records))
		for _, record := range records {
			if record.Status == StatusRetired {
				continue
			}
			selected = append(selected, cloneRecord(record))
		}
		sortRecordsByRef(selected)
		return selected, nil
	}
	active, err := activeRecords(records, byRef, normalized.Now)
	if err != nil {
		return nil, err
	}
	return active, nil
}

func normalizeRecordSelection(selection RecordSelection) RecordSelection {
	if selection.IncludeRetired {
		selection.IncludeInactive = true
	}
	if selection.Now.IsZero() {
		selection.Now = time.Now().UTC()
	} else {
		selection.Now = selection.Now.UTC()
	}
	return selection
}

func activeRecords(records []Record, byRef map[string]Record, now time.Time) ([]Record, error) {
	selected := make([]Record, 0, len(records))
	superseded := make(map[string]struct{})
	for _, record := range records {
		if !recordIsDateActive(record, now) {
			continue
		}
		if record.Status == StatusRetired {
			continue
		}
		selected = append(selected, cloneRecord(record))
	}
	for _, record := range selected {
		for _, relation := range record.Relations {
			if relation.Type != RelationSupersedes {
				continue
			}
			target := strings.TrimSpace(relation.Target)
			if target == "" {
				return nil, fmt.Errorf("supersedes relation target is required")
			}
			if target == record.Ref {
				return nil, fmt.Errorf("record %q cannot supersede itself", record.Ref)
			}
			if _, ok := byRef[target]; !ok {
				return nil, fmt.Errorf("supersedes relation target %q not found", target)
			}
			superseded[target] = struct{}{}
		}
	}
	filtered := make([]Record, 0, len(selected))
	for _, record := range selected {
		if _, ok := superseded[record.Ref]; ok {
			continue
		}
		filtered = append(filtered, record)
	}
	sortRecordsByRef(filtered)
	return filtered, nil
}

func validateRelationTargets(byRef map[string]Record) error {
	for ref, record := range byRef {
		if strings.TrimSpace(ref) == "" {
			return fmt.Errorf("memory record ref is required")
		}
		for _, relation := range record.Relations {
			target := strings.TrimSpace(relation.Target)
			if target == "" {
				return fmt.Errorf("%s relation target is required", relation.Type)
			}
			if target == ref {
				return fmt.Errorf("record %q cannot relate to itself", ref)
			}
			if _, ok := byRef[target]; !ok {
				return fmt.Errorf("%s relation target %q not found", relation.Type, target)
			}
		}
	}
	return nil
}

func recordIsDateActive(record Record, now time.Time) bool {
	if strings.TrimSpace(record.ValidFrom) != "" {
		validFrom, err := time.Parse("2006-01-02", strings.TrimSpace(record.ValidFrom))
		if err != nil {
			return false
		}
		if now.Before(validFrom.UTC()) {
			return false
		}
	}
	if strings.TrimSpace(record.ValidUntil) != "" {
		validUntil, err := time.Parse("2006-01-02", strings.TrimSpace(record.ValidUntil))
		if err != nil {
			return false
		}
		if now.After(validUntil.UTC().Add(23*time.Hour + 59*time.Minute + 59*time.Second)) {
			return false
		}
	}
	return true
}

func cloneRecord(record Record) Record {
	clone := record
	clone.Tags = append([]string(nil), record.Tags...)
	clone.SourceRefs = append([]string(nil), record.SourceRefs...)
	clone.EvidenceRefs = append([]string(nil), record.EvidenceRefs...)
	clone.Relations = append([]RecordRelation(nil), record.Relations...)
	return clone
}

func cloneAndSortRecords(records []Record) []Record {
	cloned := make([]Record, 0, len(records))
	for _, record := range records {
		cloned = append(cloned, cloneRecord(record))
	}
	sortRecordsByRef(cloned)
	return cloned
}

func sortRecordsByRef(records []Record) {
	sort.Slice(records, func(i, j int) bool {
		return records[i].Ref < records[j].Ref
	})
}

func (s *LocalService) selectRecords(ctx context.Context, records []Record, selection RecordSelection) ([]Record, error) {
	selectedRefs, err := s.selectedRecordRefs(ctx, selection)
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(records))
	for _, record := range records {
		if _, ok := selectedRefs[record.Ref]; !ok {
			continue
		}
		out = append(out, cloneRecord(record))
	}
	sortRecordsByRef(out)
	return out, nil
}

func (s *LocalService) selectedRecordRefs(ctx context.Context, selection RecordSelection) (map[string]struct{}, error) {
	records, err := s.allRecords(ctx)
	if err != nil {
		return nil, err
	}
	selected, err := SelectRecords(records, selection)
	if err != nil {
		return nil, err
	}
	refs := make(map[string]struct{}, len(selected))
	for _, record := range selected {
		refs[record.Ref] = struct{}{}
	}
	return refs, nil
}
