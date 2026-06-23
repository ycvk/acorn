package memory

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
	for _, record := range records {
		if strings.TrimSpace(record.Ref) == "" {
			return nil, fmt.Errorf("memory record ref is required")
		}
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
	active, err := activeRecords(records, normalized.Now)
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

func activeRecords(records []Record, now time.Time) ([]Record, error) {
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

func cloneRecord(record Record) Record {
	clone := record
	clone.Tags = append([]string(nil), record.Tags...)
	clone.SourceRefs = append([]string(nil), record.SourceRefs...)
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
