package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ycvk/acorn/internal/core"
)

// Historical revisions retain their original evidence; visibility is evaluated
// against that evidence even when the record's current revision has new sources.
const memoryRevisionVisibility = `NOT EXISTS(
 SELECT 1 FROM json_each(v.data,'$.evidence') e JOIN memory_exclusions x ON x.source_id=json_extract(e.value,'$.source_id')
 WHERE x.quote='' OR instr(json_extract(e.value,'$.quote'),x.quote)>0 OR instr(x.quote,json_extract(e.value,'$.quote'))>0
) AND (json_extract(v.data,'$.kind')='fact' OR NOT EXISTS(
 SELECT 1 FROM json_each(v.data,'$.source_ids') source JOIN memory_exclusions x ON x.source_id=source.value
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
		rows, err := tx.QueryContext(ctx, `SELECT v.data FROM memory_revisions v WHERE v.record_id=? AND `+memoryRevisionVisibility+` ORDER BY v.revision`, id)
		if err != nil {
			return err
		}
		if err := scanRows(rows, func(scan func(...any) error) error {
			var data string
			if err := scan(&data); err != nil {
				return err
			}
			var r core.MemoryRecord
			if err := json.Unmarshal([]byte(data), &r); err != nil {
				return err
			}
			out.Versions = append(out.Versions, r)
			return nil
		}); err != nil {
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
		rows, err = s.read.QueryContext(ctx, `SELECT data FROM memory_records WHERE excluded=0 AND (?='' OR EXISTS(SELECT 1 FROM memory_sources s JOIN json_each(memory_records.data,'$.source_ids') j ON s.id=j.value WHERE s.session_id=?)) ORDER BY updated_at DESC,id`, q.SessionID, q.SessionID)
	} else {
		rows, err = s.read.QueryContext(ctx, `SELECT v.data FROM memory_revisions v JOIN memory_records r ON r.id=v.record_id WHERE r.excluded=0 AND v.revision=(SELECT revision FROM memory_revisions WHERE record_id=v.record_id AND at<=? ORDER BY at DESC,revision DESC LIMIT 1) AND (?='' OR EXISTS(SELECT 1 FROM memory_sources s JOIN json_each(v.data,'$.source_ids') j ON s.id=j.value WHERE s.session_id=?)) AND `+memoryRevisionVisibility+` ORDER BY v.at DESC,v.record_id`, formatTimestamp(q.KnownAt), q.SessionID, q.SessionID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []core.MemoryRecord{}
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var record core.MemoryRecord
		if err := json.Unmarshal([]byte(data), &record); err != nil {
			return nil, err
		}
		if memoryRecordMatches(record, q) {
			out = append(out, record)
			if len(out) >= memoryQueryLimit(q) {
				break
			}
		}
	}
	return out, rows.Err()
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
		var long, selects []string
		for _, term := range terms {
			if utf8.RuneCountInString(term) < 3 {
				selects = append(selects, `SELECT id,rank FROM (SELECT id,0 AS rank FROM memory_records WHERE excluded=0 AND content LIKE ? ESCAPE '\' ORDER BY id LIMIT 1000)`)
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
			clauses = append(clauses, `json_extract(v.data,'$.content') LIKE ? ESCAPE '\'`)
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

// loadMemoryRecordBatch reads records in input order, skipping excluded ones;
// an unknown ID fails like loadMemoryRecord.
func loadMemoryRecordBatch(ctx context.Context, q memorySQL, ids []string) ([]core.MemoryRecord, error) {
	data, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `SELECT j.value,r.data,r.excluded FROM json_each(?) j LEFT JOIN memory_records r ON r.id=j.value ORDER BY j.key`, string(data))
	if err != nil {
		return nil, err
	}
	out := make([]core.MemoryRecord, 0, len(ids))
	err = scanRows(rows, func(scan func(...any) error) error {
		var id string
		var body sql.NullString
		var excluded sql.NullBool
		if err := scan(&id, &body, &excluded); err != nil {
			return err
		}
		if !body.Valid {
			return fmt.Errorf("%w: record %s", core.ErrMemoryNotFound, id)
		}
		if excluded.Bool {
			return nil
		}
		var record core.MemoryRecord
		if err := json.Unmarshal([]byte(body.String), &record); err != nil {
			return fmt.Errorf("read memory %s: %w", id, err)
		}
		out = append(out, record)
		return nil
	})
	return out, err
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
		for _, record := range records {
			if !q.KnownAt.IsZero() {
				var data string
				err := s.read.QueryRowContext(ctx, `SELECT v.data FROM memory_revisions v WHERE v.record_id=? AND v.revision=(SELECT revision FROM memory_revisions WHERE record_id=v.record_id AND at<=? ORDER BY at DESC,revision DESC LIMIT 1) AND `+memoryRevisionVisibility, record.ID, formatTimestamp(q.KnownAt)).Scan(&data)
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				if err != nil {
					return nil, err
				}
				if err := json.Unmarshal([]byte(data), &record); err != nil {
					return nil, err
				}
			}
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
