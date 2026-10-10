package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ycvk/acorn/internal/core"
)

// memoryRevisionKey names one revision of a record.
type memoryRevisionKey struct {
	id       string
	revision int64
}

func loadMemoryRecord(ctx context.Context, q memorySQL, id string) (core.MemoryRecord, error) {
	var record core.MemoryRecord
	var excluded bool
	var recorded, updated, validFrom, validTo string
	err := q.QueryRowContext(ctx, `SELECT id,revision,kind,content,scope,state,basis,valid_from,valid_to,needs_review,pinned,recorded_at,updated_at,excluded FROM memory_records WHERE id=?`, id).
		Scan(&record.ID, &record.Revision, &record.Kind, &record.Content, &record.Scope, &record.State, &record.Basis, &validFrom, &validTo, &record.NeedsReview, &record.Pinned, &recorded, &updated, &excluded)
	if errors.Is(err, sql.ErrNoRows) {
		return record, fmt.Errorf("%w: record %s", core.ErrMemoryNotFound, id)
	}
	if err != nil {
		return record, err
	}
	if excluded {
		return record, fmt.Errorf("%w: record %s", core.ErrMemoryExcluded, id)
	}
	if err := setMemoryTimes(&record, recorded, updated, validFrom, validTo); err != nil {
		return record, err
	}
	records := []core.MemoryRecord{record}
	if err := attachMemoryRelations(ctx, q, records); err != nil {
		return record, err
	}
	return records[0], nil
}

// loadMemoryRevisions reads the named revisions in order. RecordedAt comes from
// the record; UpdatedAt is the revision time.
func loadMemoryRevisions(ctx context.Context, q memorySQL, keys []memoryRevisionKey) ([]core.MemoryRecord, error) {
	if len(keys) == 0 {
		return []core.MemoryRecord{}, nil
	}
	pairs, err := memoryRevisionPairs(keys)
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `SELECT v.record_id,v.revision,v.kind,v.content,v.scope,v.state,v.basis,v.valid_from,v.valid_to,v.needs_review,v.pinned,r.recorded_at,v.at
 FROM json_each(?) j CROSS JOIN memory_revisions v ON v.record_id=j.value->>'$[0]' AND v.revision=j.value->>'$[1]' JOIN memory_records r ON r.id=v.record_id ORDER BY j.key`, pairs)
	if err != nil {
		return nil, err
	}
	out := make([]core.MemoryRecord, 0, len(keys))
	if err := scanRows(rows, func(scan func(...any) error) error {
		var record core.MemoryRecord
		var recorded, updated, validFrom, validTo string
		if err := scan(&record.ID, &record.Revision, &record.Kind, &record.Content, &record.Scope, &record.State, &record.Basis, &validFrom, &validTo, &record.NeedsReview, &record.Pinned, &recorded, &updated); err != nil {
			return err
		}
		if err := setMemoryTimes(&record, recorded, updated, validFrom, validTo); err != nil {
			return err
		}
		out = append(out, record)
		return nil
	}); err != nil {
		return nil, err
	}
	if len(out) != len(keys) {
		return nil, fmt.Errorf("%w: found %d of %d memory revisions", core.ErrMemoryNotFound, len(out), len(keys))
	}
	return out, attachMemoryRelations(ctx, q, out)
}

func setMemoryTimes(record *core.MemoryRecord, recorded, updated, validFrom, validTo string) error {
	var err error
	if record.RecordedAt, err = parseTimestamp(fixedTimestampLayout, recorded, "memory recorded_at"); err != nil {
		return err
	}
	if record.UpdatedAt, err = parseTimestamp(fixedTimestampLayout, updated, "memory updated_at"); err != nil {
		return err
	}
	if record.ValidFrom, err = parseOptionalTime(validFrom, "memory valid_from"); err != nil {
		return err
	}
	record.ValidTo, err = parseOptionalTime(validTo, "memory valid_to")
	return err
}

func memoryRevisionPairs(keys []memoryRevisionKey) (string, error) {
	pairs := make([][2]any, len(keys))
	for i, key := range keys {
		pairs[i] = [2]any{key.id, key.revision}
	}
	data, err := json.Marshal(pairs)
	return string(data), err
}

// attachMemoryRelations fills each revision's evidence, entities, parents,
// resolved sources and aliases, in the order they were written.
func attachMemoryRelations(ctx context.Context, q memorySQL, records []core.MemoryRecord) error {
	if len(records) == 0 {
		return nil
	}
	keys := make([]memoryRevisionKey, len(records))
	positions := make(map[memoryRevisionKey][]int, len(records))
	for i := range records {
		r := &records[i]
		r.Evidence, r.Entities, r.Parents, r.SourceIDs, r.Aliases = []core.MemoryEvidence{}, []string{}, []string{}, []string{}, nil
		keys[i] = memoryRevisionKey{r.ID, r.Revision}
		positions[keys[i]] = append(positions[keys[i]], i)
	}
	pairs, err := memoryRevisionPairs(keys)
	if err != nil {
		return err
	}
	// each scans rows whose first two columns name a revision and hands the
	// remaining columns to add for every requested copy of that revision.
	each := func(query string, values func() []any, add func(r *core.MemoryRecord)) error {
		rows, err := q.QueryContext(ctx, query, pairs)
		if err != nil {
			return err
		}
		return scanRows(rows, func(scan func(...any) error) error {
			var key memoryRevisionKey
			if err := scan(append([]any{&key.id, &key.revision}, values()...)...); err != nil {
				return err
			}
			for _, i := range positions[key] {
				add(&records[i])
			}
			return nil
		})
	}
	const from = ` FROM json_each(?) j CROSS JOIN %s t ON t.record_id=j.value->>'$[0]' AND t.revision=j.value->>'$[1]' ORDER BY t.rowid`
	var evidence core.MemoryEvidence
	if err := each(`SELECT t.record_id,t.revision,t.source_id,t.quote,t.relation`+fmt.Sprintf(from, "memory_revision_evidence"),
		func() []any { return []any{&evidence.SourceID, &evidence.Quote, &evidence.Relation} },
		func(r *core.MemoryRecord) { r.Evidence = append(r.Evidence, evidence) }); err != nil {
		return err
	}
	var value string
	for _, rel := range []struct {
		table, column string
		field         func(r *core.MemoryRecord) *[]string
	}{
		{"memory_revision_entities", "entity", func(r *core.MemoryRecord) *[]string { return &r.Entities }},
		{"memory_revision_parents", "parent_id", func(r *core.MemoryRecord) *[]string { return &r.Parents }},
		{"memory_revision_sources", "source_id", func(r *core.MemoryRecord) *[]string { return &r.SourceIDs }},
	} {
		if err := each(`SELECT t.record_id,t.revision,t.`+rel.column+fmt.Sprintf(from, rel.table),
			func() []any { return []any{&value} },
			func(r *core.MemoryRecord) { *rel.field(r) = append(*rel.field(r), value) }); err != nil {
			return err
		}
	}
	var alias core.MemoryEntityAlias
	return each(`SELECT t.record_id,t.revision,t.canonical,t.alias,t.scope,t.source_id,t.quote`+fmt.Sprintf(from, "memory_entity_aliases"),
		func() []any {
			return []any{&alias.Canonical, &alias.Alias, &alias.Scope, &alias.SourceID, &alias.Quote}
		},
		func(r *core.MemoryRecord) { r.Aliases = append(r.Aliases, alias) })
}

// persistMemoryRecord makes r the current state of its record and stores it as
// revision r.Revision with its relations.
func persistMemoryRecord(ctx context.Context, q memorySQL, r core.MemoryRecord, reason string) error {
	validFrom, validTo := formatZeroableTimestamp(r.ValidFrom), formatZeroableTimestamp(r.ValidTo)
	if _, err := q.ExecContext(ctx, `INSERT INTO memory_records(id,kind,content,scope,state,basis,revision,recorded_at,updated_at,valid_from,valid_to,needs_review,pinned)
	VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET kind=excluded.kind,content=excluded.content,scope=excluded.scope,state=excluded.state,basis=excluded.basis,revision=excluded.revision,updated_at=excluded.updated_at,valid_from=excluded.valid_from,valid_to=excluded.valid_to,needs_review=excluded.needs_review,pinned=excluded.pinned`,
		r.ID, r.Kind, r.Content, r.Scope, r.State, r.Basis, r.Revision, formatTimestamp(r.RecordedAt), formatTimestamp(r.UpdatedAt), validFrom, validTo, r.NeedsReview, r.Pinned); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `INSERT INTO memory_revisions(record_id,revision,kind,content,scope,state,basis,valid_from,valid_to,needs_review,pinned,at,reason) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Revision, r.Kind, r.Content, r.Scope, r.State, r.Basis, validFrom, validTo, r.NeedsReview, r.Pinned, formatTimestamp(r.UpdatedAt), reason); err != nil {
		return err
	}
	for _, e := range r.Evidence {
		if _, err := q.ExecContext(ctx, `INSERT INTO memory_revision_evidence(record_id,revision,source_id,quote,relation) VALUES(?,?,?,?,?)`, r.ID, r.Revision, e.SourceID, e.Quote, e.Relation); err != nil {
			return err
		}
	}
	for _, rel := range []struct {
		table, column string
		values        []string
	}{
		{"memory_revision_entities", "entity", r.Entities},
		{"memory_revision_parents", "parent_id", r.Parents},
		{"memory_revision_sources", "source_id", r.SourceIDs},
	} {
		for _, value := range rel.values {
			if _, err := q.ExecContext(ctx, `INSERT INTO `+rel.table+`(record_id,revision,`+rel.column+`) VALUES(?,?,?)`, r.ID, r.Revision, value); err != nil {
				return err
			}
		}
	}
	if err := persistMemoryAliases(ctx, q, r); err != nil {
		return err
	}
	return enqueueMemoryJob(ctx, q, "embed", r.ID, fmt.Sprint(r.Revision), r.UpdatedAt)
}
