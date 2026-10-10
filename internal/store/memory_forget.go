package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/ycvk/acorn/internal/core"
)

func (s *Store) ForgetMemory(ctx context.Context, request core.MemoryForget) (core.MemoryExclusion, error) {
	if request.Now.IsZero() || request.Reason == "" || request.RequestSourceID == "" || (len(request.RecordIDs) == 0 && len(request.SourceIDs) == 0) {
		return core.MemoryExclusion{}, errors.New("forget requires owner source, reason, now and targets")
	}
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		owner, err := loadMemorySource(ctx, tx, request.RequestSourceID, false)
		if err != nil {
			return err
		}
		if owner.Speaker != "owner" {
			return errors.New("forget requires an owner request")
		}
		epoch, err := memoryEpoch(ctx, tx)
		if err != nil {
			return err
		}
		epoch++
		if _, err := tx.ExecContext(ctx, `UPDATE memory_state SET epoch=?,request_run_id=? WHERE id=1`, epoch, owner.RunID); err != nil {
			return err
		}
		var fragments []core.MemoryExcludedFragment
		for _, id := range request.SourceIDs {
			if _, err := loadMemorySource(ctx, tx, id, false); err != nil {
				return err
			}
			fragments = append(fragments, core.MemoryExcludedFragment{SourceID: id})
		}
		for _, id := range request.RecordIDs {
			r, err := loadMemoryRecord(ctx, tx, id)
			if err != nil {
				return err
			}
			if r.Kind == "fact" {
				for _, e := range r.Evidence {
					fragments = append(fragments, core.MemoryExcludedFragment{SourceID: e.SourceID, Quote: e.Quote})
				}
			}
		}
		fragments = append(fragments, core.MemoryExcludedFragment{SourceID: request.RequestSourceID})
		for _, f := range fragments {
			if err := insertMemoryExclusion(ctx, tx, f, request, epoch); err != nil {
				return err
			}
		}
		// Derived messages and summaries inherit their input source dependencies.
		rows, err := tx.QueryContext(ctx, `WITH RECURSIVE affected(id) AS(SELECT source_id FROM memory_exclusions UNION SELECT l.source_id FROM memory_source_links l JOIN affected a ON l.parent_id=a.id) SELECT DISTINCT l.source_id FROM memory_source_links l JOIN affected a ON l.parent_id=a.id`)
		if err != nil {
			return err
		}
		affected, err := memoryStringRows(rows)
		if err != nil {
			return err
		}
		for _, id := range affected {
			if slices.ContainsFunc(fragments, func(f core.MemoryExcludedFragment) bool { return f.SourceID == id }) {
				continue
			}
			if err := insertMemoryExclusion(ctx, tx, core.MemoryExcludedFragment{SourceID: id}, request, epoch); err != nil {
				return err
			}
		}
		targetsJSON, err := json.Marshal(request.RecordIDs)
		if err != nil {
			return err
		}
		rows, err = tx.QueryContext(ctx, `WITH RECURSIVE affected(id) AS(
		 SELECT value FROM json_each(?) WHERE value IS NOT NULL UNION
		 SELECT DISTINCT e.record_id FROM memory_evidence e JOIN memory_exclusions x ON e.source_id=x.source_id
		 WHERE x.quote='' OR instr(e.quote,x.quote)>0 OR instr(x.quote,e.quote)>0
		 UNION SELECT l.from_id FROM memory_links l JOIN affected a ON l.to_id=a.id WHERE l.relation='derives_from'
		 ) SELECT id FROM affected`, string(targetsJSON))
		if err != nil {
			return err
		}
		recordIDs, err := memoryStringRows(rows)
		if err != nil {
			return err
		}
		recordIDs = uniqueMemoryStrings(append(recordIDs, request.RecordIDs...))
		for _, id := range recordIDs {
			if _, err := tx.ExecContext(ctx, `UPDATE memory_records SET excluded=1 WHERE id=?`, id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM memory_embeddings WHERE record_id=?`, id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE memory_jobs SET state='done',completed_at=?,error='owner excluded memory',lease_until='' WHERE object_id=?`, formatTimestamp(request.Now), id); err != nil {
				return err
			}
		}
		// A current revision may have replaced its evidence. Remove embeddings
		// of excluded historical evidence even when that current record survives.
		if _, err := tx.ExecContext(ctx, `DELETE FROM memory_embeddings AS e WHERE EXISTS(SELECT 1 FROM memory_revisions v WHERE v.record_id=e.record_id AND v.revision=e.revision AND NOT (`+memoryRevisionVisibility+`))`); err != nil {
			return err
		}
		// When source evidence is forgotten, surviving parents can support a new
		// understanding after review. Withdrawing only an inference preserves its
		// parents without scheduling that same inference again.
		if len(fragments) > 1 {
			removed, err := json.Marshal(recordIDs)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO memory_jobs(operation,object_id,version,available_at,created_at)
    SELECT DISTINCT 'consolidate',r.id,CAST(r.revision AS TEXT),?,? FROM memory_links l JOIN memory_records r ON r.id=l.to_id
    WHERE l.relation='derives_from' AND l.from_id IN(SELECT value FROM json_each(?)) AND r.excluded=0 AND r.state IN ('current','disputed')
    ON CONFLICT(operation,object_id,version,processor_version) DO UPDATE SET state='pending',token=memory_jobs.token+1,attempts=0,cursor=0,lease_until='',error='',completed_at='',available_at=excluded.available_at`, formatTimestamp(request.Now), formatTimestamp(request.Now), string(removed)); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM thread_summaries WHERE EXISTS(SELECT 1 FROM json_each(sources_json) j JOIN memory_exclusions x ON x.source_id=j.value)`); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE memory_jobs SET state='done',completed_at=?,error='owner excluded source',lease_until='' WHERE object_id IN(SELECT source_id FROM memory_exclusions WHERE quote='')`, formatTimestamp(request.Now))
		return err
	})
	if err != nil {
		return core.MemoryExclusion{}, err
	}
	return s.MemoryExclusions(ctx)
}

func insertMemoryExclusion(ctx context.Context, q memorySQL, f core.MemoryExcludedFragment, r core.MemoryForget, epoch int64) error {
	_, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO memory_exclusions(epoch,source_id,quote,request_source_id,reason,created_at) VALUES(?,?,?,?,?,?)`, epoch, f.SourceID, f.Quote, r.RequestSourceID, r.Reason, formatTimestamp(r.Now))
	return err
}

func (s *Store) MemoryExclusions(ctx context.Context) (core.MemoryExclusion, error) {
	var out core.MemoryExclusion
	var err error
	err = s.read.QueryRowContext(ctx, `SELECT epoch,request_run_id FROM memory_state WHERE id=1`).Scan(&out.Epoch, &out.RequestRunID)
	if err != nil {
		return out, err
	}
	rows, err := s.read.QueryContext(ctx, `SELECT source_id,quote FROM memory_exclusions ORDER BY id`)
	if err != nil {
		return out, err
	}
	if err := scanRows(rows, func(scan func(...any) error) error {
		var f core.MemoryExcludedFragment
		if err := scan(&f.SourceID, &f.Quote); err != nil {
			return err
		}
		out.Fragments = append(out.Fragments, f)
		out.SourceIDs = append(out.SourceIDs, f.SourceID)
		if f.Quote != "" {
			out.Quotes = append(out.Quotes, f.Quote)
		}
		return nil
	}); err != nil {
		return out, err
	}
	rows, err = s.read.QueryContext(ctx, `SELECT id FROM memory_records WHERE excluded=1`)
	if err != nil {
		return out, err
	}
	out.RecordIDs, err = memoryStringRows(rows)
	if err != nil {
		return out, err
	}
	rows, err = s.read.QueryContext(ctx, `SELECT DISTINCT r.run_id FROM context_snapshot_refs r WHERE r.source_id IN(SELECT source_id FROM memory_exclusions) OR r.record_id IN(SELECT id FROM memory_records WHERE excluded=1)`)
	if err != nil {
		return out, err
	}
	out.RunIDs, err = memoryStringRows(rows)
	out.SourceIDs = uniqueMemoryStrings(out.SourceIDs)
	out.Quotes = uniqueMemoryStrings(out.Quotes)
	return out, err
}

func (s *Store) SaveMemorySnapshotRefs(ctx context.Context, hash, runID string, sources, records []string, epoch int64) error {
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		current, err := memoryEpoch(ctx, tx)
		if err != nil {
			return err
		}
		if current != epoch {
			return core.ErrMemoryExcluded
		}
		for _, id := range uniqueMemoryStrings(sources) {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO context_snapshot_refs(hash,run_id,source_id,epoch) VALUES(?,?,?,?)`, hash, runID, id, epoch); err != nil {
				return err
			}
		}
		for _, id := range uniqueMemoryStrings(records) {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO context_snapshot_refs(hash,run_id,record_id,epoch) VALUES(?,?,?,?)`, hash, runID, id, epoch); err != nil {
				return err
			}
		}
		return nil
	})
}
