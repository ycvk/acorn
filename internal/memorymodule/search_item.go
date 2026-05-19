package memorymodule

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
