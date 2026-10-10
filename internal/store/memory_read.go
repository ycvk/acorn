package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ycvk/acorn/internal/core"
)

// Historical revisions retain their original evidence; visibility is evaluated
// against that evidence even when the record's current revision has new sources.
const memoryRevisionVisibility = `NOT EXISTS(
 SELECT 1 FROM memory_revision_evidence ve JOIN memory_exclusions vx ON vx.source_id=ve.source_id
 WHERE ve.record_id=v.record_id AND ve.revision=v.revision AND (vx.quote='' OR instr(ve.quote,vx.quote)>0 OR instr(vx.quote,ve.quote)>0)
) AND (v.kind='fact' OR NOT EXISTS(
 SELECT 1 FROM memory_revision_sources vs JOIN memory_exclusions vx ON vx.source_id=vs.source_id WHERE vs.record_id=v.record_id AND vs.revision=v.revision
))`

// memoryRecordLinksSQL starts from the record's own links; CROSS JOIN keeps the
// planner from scanning every unexcluded record first.
const memoryRecordLinksSQL = `SELECT l.from_id,l.to_id,l.relation FROM (SELECT from_id,to_id,relation FROM memory_links WHERE from_id=? UNION SELECT from_id,to_id,relation FROM memory_links WHERE to_id=?) l CROSS JOIN memory_records a ON a.id=l.from_id CROSS JOIN memory_records b ON b.id=l.to_id WHERE a.excluded=0 AND b.excluded=0`

func (s *Store) ReadMemory(ctx context.Context, id string) (core.MemoryRead, error) {
	var out core.MemoryRead
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		var err error
		out.Record, err = loadMemoryRecord(ctx, tx, id)
		if err != nil {
			return err
		}
		out.Sources, err = resolveMemorySources(ctx, tx, out.Record.SourceIDs)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT v.record_id,v.revision FROM memory_revisions v WHERE v.record_id=? AND `+memoryRevisionVisibility+` ORDER BY v.revision`, id)
		if err != nil {
			return err
		}
		keys, err := memoryRevisionKeys(rows)
		if err != nil {
			return err
		}
		if out.Versions, err = loadMemoryRevisions(ctx, tx, keys); err != nil {
			return err
		}
		rows, err = tx.QueryContext(ctx, memoryRecordLinksSQL, id, id)
		if err != nil {
			return err
		}
		return scanRows(rows, func(scan func(...any) error) error {
			var l core.MemoryLink
			if err := scan(&l.FromID, &l.ToID, &l.Relation); err != nil {
				return err
			}
			out.Links = append(out.Links, l)
			return nil
		})
	})
	return out, err
}

func memoryRecordMatches(r core.MemoryRecord, q core.MemoryQuery) bool {
	if !q.ChangedSince.IsZero() && !r.UpdatedAt.After(q.ChangedSince) && !r.NeedsReview && r.Kind != "thought" {
		return false
	}
	if q.Kind != "" && r.Kind != q.Kind {
		return false
	}
	if q.Mode != "history" && r.State != "current" && r.State != "disputed" && r.State != "open" {
		return false
	}
	if !q.AsOf.IsZero() {
		if !r.ValidFrom.IsZero() && r.ValidFrom.After(q.AsOf) {
			return false
		}
		if !r.ValidTo.IsZero() && !r.ValidTo.After(q.AsOf) {
			return false
		}
	}
	if !q.From.IsZero() {
		at := r.ValidFrom
		if at.IsZero() {
			at = r.RecordedAt
		}
		if at.Before(q.From) {
			return false
		}
	}
	if !q.To.IsZero() {
		at := r.ValidFrom
		if at.IsZero() {
			at = r.RecordedAt
		}
		if !at.Before(q.To) {
			return false
		}
	}
	for _, entity := range q.Entities {
		if !slices.Contains(r.Entities, entity) {
			return false
		}
	}
	return true
}

func memoryQueryLimit(q core.MemoryQuery) int {
	if q.Limit <= 0 {
		return 50
	}
	return min(q.Limit, 1000)
}

func (s *Store) ListMemoryRecords(ctx context.Context, q core.MemoryQuery) ([]core.MemoryRecord, error) {
	var rows *sql.Rows
	var err error
	q, err = s.expandMemoryAliases(ctx, q)
	if err != nil {
		return nil, err
	}
	if q.KnownAt.IsZero() {
		rows, err = s.read.QueryContext(ctx, `SELECT r.id,r.revision FROM memory_records r WHERE r.excluded=0 AND (?='' OR EXISTS(SELECT 1 FROM memory_revision_sources rs JOIN memory_sources s ON s.id=rs.source_id WHERE rs.record_id=r.id AND rs.revision=r.revision AND s.session_id=?)) ORDER BY r.updated_at DESC,r.id`, q.SessionID, q.SessionID)
	} else {
		rows, err = s.read.QueryContext(ctx, `SELECT v.record_id,v.revision FROM memory_revisions v JOIN memory_records r ON r.id=v.record_id WHERE r.excluded=0 AND v.revision=(SELECT revision FROM memory_revisions WHERE record_id=v.record_id AND at<=? ORDER BY at DESC,revision DESC LIMIT 1) AND (?='' OR EXISTS(SELECT 1 FROM memory_revision_sources rs JOIN memory_sources s ON s.id=rs.source_id WHERE rs.record_id=v.record_id AND rs.revision=v.revision AND s.session_id=?)) AND `+memoryRevisionVisibility+` ORDER BY v.at DESC,v.record_id`, formatTimestamp(q.KnownAt), q.SessionID, q.SessionID)
	}
	if err != nil {
		return nil, err
	}
	keys, err := memoryRevisionKeys(rows)
	if err != nil {
		return nil, err
	}
	limit := memoryQueryLimit(q)
	out := []core.MemoryRecord{}
	for start := 0; start < len(keys) && len(out) < limit; start += memoryRecordBatch {
		records, err := loadMemoryRevisions(ctx, s.read, keys[start:min(len(keys), start+memoryRecordBatch)])
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			if memoryRecordMatches(record, q) {
				out = append(out, record)
				if len(out) >= limit {
					break
				}
			}
		}
	}
	return out, nil
}

func memoryRevisionKeys(rows *sql.Rows) ([]memoryRevisionKey, error) {
	var keys []memoryRevisionKey
	err := scanRows(rows, func(scan func(...any) error) error {
		var key memoryRevisionKey
		if err := scan(&key.id, &key.revision); err != nil {
			return err
		}
		keys = append(keys, key)
		return nil
	})
	return keys, err
}

// memorySearchTerms keeps names whole and supplies character n-grams for CJK
// questions. The vector route supplies semantic matches across different words.
func memorySearchTerms(query string) []string {
	var terms []string
	for _, word := range strings.FieldsFunc(query, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsPunct(r) }) {
		terms = append(terms, word)
		runes := []rune(word)
		hasHan := slices.ContainsFunc(runes, func(r rune) bool { return unicode.Is(unicode.Han, r) })
		if hasHan && len(runes) > 3 {
			for i := 0; i+3 <= len(runes); i++ {
				terms = append(terms, string(runes[i:i+3]))
			}
		}
	}
	terms = uniqueMemoryStrings(terms)
	if len(terms) > 32 {
		terms = terms[:32]
	}
	return terms
}

// A term matching more records than this carries little ranking signal, and
// bm25 would score and sort every match; the semantic route covers it instead.
const memoryKeywordTermMatches = 4096

// memoryTermMatches counts records matching one FTS phrase, stopping just past
// memoryKeywordTermMatches so frequent terms cost a bounded scan.
func (s *Store) memoryTermMatches(ctx context.Context, phrase string) (int, error) {
	var n int
	err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT 1 FROM memory_records_fts WHERE memory_records_fts MATCH ? LIMIT ?)`, phrase, memoryKeywordTermMatches+1).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count memory term matches: %w", err)
	}
	return n, nil
}

func (s *Store) SearchMemoryText(ctx context.Context, q core.MemoryQuery) ([]core.MemoryRecord, error) {
	var err error
	q, err = s.expandMemoryAliases(ctx, q)
	if err != nil {
		return nil, err
	}
	terms := memorySearchTerms(q.Query)
	if len(terms) == 0 {
		return []core.MemoryRecord{}, nil
	}
	var query string
	var args []any
	if q.KnownAt.IsZero() {
		var long, short, selects []string
		for _, term := range terms {
			// The trigram index cannot match terms shorter than three runes;
			// one table pass covers all of them.
			if utf8.RuneCountInString(term) < 3 {
				short = append(short, `content LIKE ? ESCAPE '\'`)
				args = append(args, "%"+escapeLike(term)+"%")
				continue
			}
			phrase := `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
			matches, err := s.memoryTermMatches(ctx, phrase)
			if err != nil {
				return nil, err
			}
			if matches > 0 && matches <= memoryKeywordTermMatches {
				long = append(long, phrase)
			}
		}
		if len(short) > 0 {
			selects = append(selects, `SELECT id,rank FROM (SELECT id,0 AS rank FROM memory_records WHERE excluded=0 AND (`+strings.Join(short, " OR ")+`) ORDER BY id LIMIT 1000)`)
		}
		if len(long) > 0 {
			selects = append(selects, `SELECT r.id,f.rank FROM (SELECT rowid,rank FROM memory_records_fts WHERE memory_records_fts MATCH ? AND rowid NOT IN (SELECT rowid FROM memory_records WHERE excluded=1) ORDER BY rank LIMIT 1000) f JOIN memory_records r ON r.rowid=f.rowid`)
			args = append(args, strings.Join(long, " OR "))
		}
		if len(selects) == 0 {
			return []core.MemoryRecord{}, nil
		}
		query = `WITH candidates AS MATERIALIZED (` + strings.Join(selects, " UNION ALL ") + `) SELECT c.id FROM candidates c JOIN memory_records r ON r.id=c.id WHERE r.excluded=0 GROUP BY c.id ORDER BY MIN(c.rank),c.id LIMIT 1000`
	} else {
		var clauses []string
		args = []any{formatTimestamp(q.KnownAt)}
		for _, term := range terms {
			clauses = append(clauses, `v.content LIKE ? ESCAPE '\'`)
			args = append(args, "%"+escapeLike(term)+"%")
		}
		query = `SELECT v.record_id FROM memory_revisions v JOIN memory_records r ON r.id=v.record_id WHERE r.excluded=0 AND v.revision=(SELECT revision FROM memory_revisions WHERE record_id=v.record_id AND at<=? ORDER BY at DESC,revision DESC LIMIT 1) AND (` + strings.Join(clauses, " OR ") + `) ORDER BY v.at DESC,v.record_id LIMIT 1000`
	}
	rows, err := s.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search memory: %w", err)
	}
	ids, err := memoryStringRows(rows)
	if err != nil {
		return nil, err
	}
	return s.MemoryRecordsByIDs(ctx, ids, q)
}

const memoryRecordBatch = 128

// loadMemoryRecordBatch reads the current revision of records in input order,
// skipping excluded ones; an unknown ID fails like loadMemoryRecord.
func loadMemoryRecordBatch(ctx context.Context, q memorySQL, ids []string) ([]core.MemoryRecord, error) {
	data, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `SELECT j.value,r.revision,r.excluded FROM json_each(?) j LEFT JOIN memory_records r ON r.id=j.value ORDER BY j.key`, string(data))
	if err != nil {
		return nil, err
	}
	var keys []memoryRevisionKey
	if err := scanRows(rows, func(scan func(...any) error) error {
		var id string
		var revision sql.NullInt64
		var excluded sql.NullBool
		if err := scan(&id, &revision, &excluded); err != nil {
			return err
		}
		if !revision.Valid {
			return fmt.Errorf("%w: record %s", core.ErrMemoryNotFound, id)
		}
		if !excluded.Bool {
			keys = append(keys, memoryRevisionKey{id, revision.Int64})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return loadMemoryRevisions(ctx, q, keys)
}

// knownMemoryRevisions replaces each record with its revision visible at
// knownAt, dropping records that had none.
func knownMemoryRevisions(ctx context.Context, q memorySQL, records []core.MemoryRecord, knownAt time.Time) ([]core.MemoryRecord, error) {
	ids := make([]string, len(records))
	for i, r := range records {
		ids[i] = r.ID
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `SELECT v.record_id,v.revision FROM json_each(?) j CROSS JOIN memory_revisions v ON v.record_id=j.value WHERE v.revision=(SELECT revision FROM memory_revisions WHERE record_id=v.record_id AND at<=? ORDER BY at DESC,revision DESC LIMIT 1) AND `+memoryRevisionVisibility+` ORDER BY j.key`, string(data), formatTimestamp(knownAt))
	if err != nil {
		return nil, err
	}
	keys, err := memoryRevisionKeys(rows)
	if err != nil {
		return nil, err
	}
	return loadMemoryRevisions(ctx, q, keys)
}

func (s *Store) MemoryRecordsByIDs(ctx context.Context, ids []string, q core.MemoryQuery) ([]core.MemoryRecord, error) {
	var err error
	q, err = s.expandMemoryAliases(ctx, q)
	if err != nil {
		return nil, err
	}
	limit := memoryQueryLimit(q)
	out := make([]core.MemoryRecord, 0, min(len(ids), limit))
	for start := 0; start < len(ids) && len(out) < limit; start += memoryRecordBatch {
		records, err := loadMemoryRecordBatch(ctx, s.read, ids[start:min(len(ids), start+memoryRecordBatch)])
		if err != nil {
			return nil, err
		}
		if !q.KnownAt.IsZero() {
			if records, err = knownMemoryRevisions(ctx, s.read, records, q.KnownAt); err != nil {
				return nil, err
			}
		}
		for _, record := range records {
			if q.SessionID != "" {
				data, err := json.Marshal(record.SourceIDs)
				if err != nil {
					return nil, err
				}
				var matches bool
				if err := s.read.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM memory_sources s JOIN json_each(?) j ON s.id=j.value WHERE s.session_id=?)`, string(data), q.SessionID).Scan(&matches); err != nil {
					return nil, err
				}
				if !matches {
					continue
				}
			}
			if memoryRecordMatches(record, q) {
				out = append(out, record)
				if len(out) >= limit {
					break
				}
			}
		}
	}
	return out, nil
}

func (s *Store) MemoryNeighbors(ctx context.Context, ids []string, limit int) ([]core.MemoryRecord, error) {
	if len(ids) == 0 {
		return []core.MemoryRecord{}, nil
	}
	var found []string
	for _, id := range ids {
		rows, err := s.read.QueryContext(ctx, `SELECT to_id FROM memory_links WHERE from_id=? UNION SELECT from_id FROM memory_links WHERE to_id=? UNION SELECT b.record_id FROM memory_mentions a JOIN memory_mentions b ON a.entity_id=b.entity_id WHERE a.record_id=? LIMIT ?`, id, id, id, limit)
		if err != nil {
			return nil, err
		}
		next, err := memoryStringRows(rows)
		if err != nil {
			return nil, err
		}
		found = append(found, next...)
	}
	return s.MemoryRecordsByIDs(ctx, uniqueMemoryStrings(found), core.MemoryQuery{Mode: "history", Limit: limit})
}
