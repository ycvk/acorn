package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func (s *Store) ForgetMemory(ctx context.Context, request core.MemoryForget) (core.MemoryForgotten, error) {
	var forgotten core.MemoryForgotten
	if err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		var err error
		forgotten, err = forgetMemory(ctx, tx, request)
		return err
	}); err != nil {
		return core.MemoryForgotten{}, err
	}
	exclusion, err := s.MemoryExclusions(ctx)
	if err != nil {
		return core.MemoryForgotten{}, err
	}
	forgotten.Exclusion = exclusion
	return forgotten, nil
}

// forgetMemory excludes the requested records and sources and everything
// derived from them in one transaction, and reports the records and sources
// this request excluded.
func forgetMemory(ctx context.Context, tx *sql.Tx, request core.MemoryForget) (core.MemoryForgotten, error) {
	var forgotten core.MemoryForgotten
	if request.Now.IsZero() || request.Reason == "" || request.RequestSourceID == "" || (len(request.RecordIDs) == 0 && len(request.SourceIDs) == 0) {
		return forgotten, errors.New("forget requires owner source, reason, now and targets")
	}
	owner, err := loadMemorySource(ctx, tx, request.RequestSourceID, sourceStored)
	if err != nil {
		return forgotten, err
	}
	if owner.Speaker != "owner" {
		return forgotten, errors.New("forget requires an owner request")
	}
	epoch, err := memoryEpoch(ctx, tx)
	if err != nil {
		return forgotten, err
	}
	epoch++
	if _, err := tx.ExecContext(ctx, `UPDATE memory_state SET epoch=?,request_run_id=? WHERE id=1`, epoch, owner.RunID); err != nil {
		return forgotten, err
	}
	var fragments []core.MemoryExcludedFragment
	for _, id := range request.SourceIDs {
		var known int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_sources WHERE id=?`, id).Scan(&known); err != nil {
			return forgotten, err
		}
		if known == 0 {
			return forgotten, fmt.Errorf("%w: source %s", core.ErrMemoryNotFound, id)
		}
		fragments = append(fragments, core.MemoryExcludedFragment{SourceID: id})
	}
	for _, id := range request.RecordIDs {
		r, err := loadMemoryRecord(ctx, tx, id)
		if err != nil {
			return forgotten, err
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
			return forgotten, err
		}
	}
	// Derived messages and summaries inherit their input source dependencies.
	// Sources derived so far read what this request excludes, so they go
	// whole. Sources derived after an earlier fragment exclusion read their
	// parent without it and stay.
	rows, err := tx.QueryContext(ctx, `WITH RECURSIVE affected(id) AS(SELECT source_id FROM memory_exclusions WHERE epoch=? UNION SELECT l.source_id FROM memory_source_links l JOIN affected a ON l.parent_id=a.id) SELECT DISTINCT l.source_id FROM memory_source_links l JOIN affected a ON l.parent_id=a.id`, epoch)
	if err != nil {
		return forgotten, err
	}
	affected, err := memoryStringRows(rows)
	if err != nil {
		return forgotten, err
	}
	for _, id := range affected {
		if slices.ContainsFunc(fragments, func(f core.MemoryExcludedFragment) bool { return f.SourceID == id }) {
			continue
		}
		if err := insertMemoryExclusion(ctx, tx, core.MemoryExcludedFragment{SourceID: id}, request, epoch); err != nil {
			return forgotten, err
		}
	}
	// A record whose current revision cites an excluded source keeps the rest
	// of its evidence when that rest still supports it; otherwise the whole
	// record goes.
	rows, err = tx.QueryContext(ctx, `SELECT DISTINCT e.record_id FROM memory_revision_evidence e JOIN memory_records cur ON cur.id=e.record_id AND cur.revision=e.revision JOIN memory_exclusions x ON e.source_id=x.source_id
	 WHERE cur.excluded=0 AND (x.quote='' OR instr(e.quote,x.quote)>0 OR instr(x.quote,e.quote)>0)`)
	if err != nil {
		return forgotten, err
	}
	cited, err := memoryStringRows(rows)
	if err != nil {
		return forgotten, err
	}
	wholly := append([]string{}, request.RecordIDs...)
	retained := map[string][]core.MemoryEvidence{}
	for _, id := range cited {
		if slices.Contains(request.RecordIDs, id) {
			continue
		}
		kept, err := retainedMemoryEvidence(ctx, tx, id)
		if err != nil {
			return forgotten, err
		}
		if slices.ContainsFunc(kept, func(e core.MemoryEvidence) bool { return e.Relation == "supports" }) {
			retained[id] = kept
		} else {
			wholly = append(wholly, id)
		}
	}
	// Exclusion cascades along derives_from only from wholly excluded records;
	// an insight loses its last unexcluded parent only this way.
	targetsJSON, err := json.Marshal(wholly)
	if err != nil {
		return forgotten, err
	}
	rows, err = tx.QueryContext(ctx, `WITH RECURSIVE affected(id) AS(SELECT value FROM json_each(?) UNION SELECT l.from_id FROM memory_links l JOIN affected a ON l.to_id=a.id WHERE l.relation='derives_from') SELECT id FROM affected`, string(targetsJSON))
	if err != nil {
		return forgotten, err
	}
	recordIDs, err := memoryStringRows(rows)
	if err != nil {
		return forgotten, err
	}
	recordIDs = uniqueMemoryStrings(recordIDs)
	// The cascade can reach records an earlier request excluded; this request
	// reports only the records it excludes.
	for _, id := range recordIDs {
		res, err := tx.ExecContext(ctx, `UPDATE memory_records SET excluded=1 WHERE id=? AND excluded=0`, id)
		if err != nil {
			return forgotten, err
		}
		changed, err := res.RowsAffected()
		if err != nil {
			return forgotten, err
		}
		if changed > 0 {
			forgotten.RecordIDs = append(forgotten.RecordIDs, id)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM memory_embeddings WHERE record_id=?`, id); err != nil {
			return forgotten, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE memory_jobs SET state='done',completed_at=?,error='owner excluded memory',lease_until='' WHERE object_id=?`, formatTimestamp(request.Now), id); err != nil {
			return forgotten, err
		}
	}
	if err := stripExcludedEvidence(ctx, tx, retained, request.Now); err != nil {
		return forgotten, err
	}
	// A current revision may have replaced its evidence. Remove embeddings
	// of excluded historical evidence even when that current record survives.
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_embeddings AS e WHERE EXISTS(SELECT 1 FROM memory_revisions v WHERE v.record_id=e.record_id AND v.revision=e.revision AND NOT (`+memoryRevisionVisibility+`))`); err != nil {
		return forgotten, err
	}
	// When source evidence is forgotten, surviving parents can support a new
	// understanding after review. Withdrawing only an inference preserves its
	// parents without scheduling that same inference again.
	if len(fragments) > 1 {
		removed, err := json.Marshal(recordIDs)
		if err != nil {
			return forgotten, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO memory_jobs(operation,object_id,version,available_at,created_at)
    SELECT DISTINCT 'consolidate',r.id,CAST(r.revision AS TEXT),?,? FROM memory_links l JOIN memory_records r ON r.id=l.to_id
    WHERE l.relation='derives_from' AND l.from_id IN(SELECT value FROM json_each(?)) AND r.excluded=0 AND r.state IN ('current','disputed')
    ON CONFLICT(operation,object_id,version,processor_version) DO UPDATE SET state='pending',token=memory_jobs.token+1,attempts=0,cursor=0,lease_until='',error='',completed_at='',available_at=excluded.available_at`, formatTimestamp(request.Now), formatTimestamp(request.Now), string(removed)); err != nil {
			return forgotten, err
		}
	}
	// Every summary covers at least one source, so a summary left without
	// sources is one that covered a source this request excludes.
	if _, err := tx.ExecContext(ctx, `DELETE FROM thread_summary_sources WHERE session_id IN(SELECT s.session_id FROM thread_summary_sources s JOIN memory_exclusions x ON x.source_id=s.source_id WHERE x.epoch=?)`, epoch); err != nil {
		return forgotten, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM thread_summaries WHERE NOT EXISTS(SELECT 1 FROM thread_summary_sources s WHERE s.session_id=thread_summaries.session_id)`); err != nil {
		return forgotten, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE memory_jobs SET state='done',completed_at=?,error='owner excluded source',lease_until='' WHERE object_id IN(SELECT source_id FROM memory_exclusions WHERE quote='')`, formatTimestamp(request.Now)); err != nil {
		return forgotten, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT DISTINCT source_id FROM memory_exclusions WHERE epoch=? ORDER BY source_id`, epoch)
	if err != nil {
		return forgotten, err
	}
	forgotten.SourceIDs, err = memoryStringRows(rows)
	return forgotten, err
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
	// The affected runs read an excluded source before its exclusion; a run
	// that reads a partly excluded source later sees it without the fragment.
	// Recall never returns an excluded record, so every reference to one was
	// taken before its exclusion.
	rows, err = s.read.QueryContext(ctx, `SELECT DISTINCT r.run_id FROM context_snapshot_refs r WHERE EXISTS(SELECT 1 FROM memory_exclusions x WHERE x.source_id=r.source_id AND x.epoch>r.epoch) OR r.record_id IN(SELECT id FROM memory_records WHERE excluded=1)`)
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

// retainedMemoryEvidence is the evidence of record id's current revision whose
// source is not excluded, under the fragment rule memoryRevisionVisibility
// applies.
func retainedMemoryEvidence(ctx context.Context, q memorySQL, id string) ([]core.MemoryEvidence, error) {
	rows, err := q.QueryContext(ctx, `SELECT e.source_id,e.quote,e.relation FROM memory_revision_evidence e JOIN memory_records r ON r.id=e.record_id AND r.revision=e.revision
	 WHERE e.record_id=? AND NOT EXISTS(SELECT 1 FROM memory_exclusions x WHERE x.source_id=e.source_id AND (x.quote='' OR instr(e.quote,x.quote)>0 OR instr(x.quote,e.quote)>0)) ORDER BY e.rowid`, id)
	if err != nil {
		return nil, err
	}
	kept := []core.MemoryEvidence{}
	err = scanRows(rows, func(scan func(...any) error) error {
		var e core.MemoryEvidence
		if err := scan(&e.SourceID, &e.Quote, &e.Relation); err != nil {
			return err
		}
		kept = append(kept, e)
		return nil
	})
	return kept, err
}

// stripExcludedEvidence writes a new revision for each surviving record with
// its excluded evidence removed, and marks the insights derived from them for
// review. A parent is rewritten before its children, so a child's root sources
// come from the parent's new revision.
func stripExcludedEvidence(ctx context.Context, q memorySQL, retained map[string][]core.MemoryEvidence, now time.Time) error {
	if len(retained) == 0 {
		return nil
	}
	seeds, err := json.Marshal(slices.Sorted(maps.Keys(retained)))
	if err != nil {
		return err
	}
	rows, err := q.QueryContext(ctx, `WITH RECURSIVE derived(id) AS(SELECT value FROM json_each(?) UNION SELECT l.from_id FROM memory_links l JOIN derived d ON l.to_id=d.id WHERE l.relation='derives_from')
	 SELECT d.id FROM derived d JOIN memory_records r ON r.id=d.id WHERE r.excluded=0`, string(seeds))
	if err != nil {
		return err
	}
	ids, err := memoryStringRows(rows)
	if err != nil {
		return err
	}
	ordered, err := memoryDerivationOrder(ctx, q, uniqueMemoryStrings(ids))
	if err != nil {
		return err
	}
	for _, id := range ordered {
		r, err := loadMemoryRecord(ctx, q, id)
		if err != nil {
			return err
		}
		reason := "supporting evidence excluded"
		if kept, ok := retained[id]; ok {
			dropped := slices.DeleteFunc(slices.Clone(r.Evidence), func(e core.MemoryEvidence) bool { return slices.Contains(kept, e) })
			r.Entities = retainedMemoryEntities(r, kept, dropped)
			r.Aliases = retainedMemoryAliases(r.Aliases, kept)
			r.Evidence = kept
			reason = "evidence excluded by owner"
		}
		derived := slices.ContainsFunc(r.Parents, func(parent string) bool { return slices.Contains(ordered, parent) })
		if derived {
			r.NeedsReview = true
		}
		if r.SourceIDs, err = memoryRecordSources(ctx, q, r); err != nil {
			return err
		}
		r.Revision++
		r.UpdatedAt = now
		if err := persistMemoryRecord(ctx, q, r, reason); err != nil {
			return err
		}
		if err := replaceMemoryMentions(ctx, q, r); err != nil {
			return err
		}
		if derived {
			if err := enqueueMemoryJob(ctx, q, "consolidate", r.ID, fmt.Sprint(r.Revision), now); err != nil {
				return err
			}
		}
	}
	return nil
}

// memoryDerivationOrder sorts ids so every record follows the records it
// derives from within the set.
func memoryDerivationOrder(ctx context.Context, q memorySQL, ids []string) ([]string, error) {
	data, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `SELECT l.from_id,l.to_id FROM memory_links l WHERE l.relation='derives_from' AND l.from_id IN(SELECT value FROM json_each(?)) AND l.to_id IN(SELECT value FROM json_each(?))`, string(data), string(data))
	if err != nil {
		return nil, err
	}
	parents := map[string][]string{}
	if err := scanRows(rows, func(scan func(...any) error) error {
		var from, to string
		if err := scan(&from, &to); err != nil {
			return err
		}
		parents[from] = append(parents[from], to)
		return nil
	}); err != nil {
		return nil, err
	}
	ordered := make([]string, 0, len(ids))
	for remaining := ids; len(remaining) > 0; {
		var next []string
		for _, id := range remaining {
			if slices.ContainsFunc(parents[id], func(parent string) bool { return !slices.Contains(ordered, parent) }) {
				next = append(next, id)
				continue
			}
			ordered = append(ordered, id)
		}
		if len(next) == len(remaining) {
			return nil, fmt.Errorf("memory derivation cycle among %v", next)
		}
		remaining = next
	}
	return ordered, nil
}

// retainedMemoryEntities drops the entities that only the dropped evidence
// names; the content or the kept evidence vouches for the rest.
func retainedMemoryEntities(r core.MemoryRecord, kept, dropped []core.MemoryEvidence) []string {
	named := func(entity string, evidence []core.MemoryEvidence) bool {
		return slices.ContainsFunc(evidence, func(e core.MemoryEvidence) bool { return strings.Contains(e.Quote, entity) })
	}
	return slices.DeleteFunc(slices.Clone(r.Entities), func(entity string) bool {
		return named(entity, dropped) && !named(entity, kept) && !strings.Contains(r.Content, entity)
	})
}

// retainedMemoryAliases keeps the aliases the kept evidence still quotes, the
// same support validateMemoryAliases demands on write.
func retainedMemoryAliases(aliases []core.MemoryEntityAlias, kept []core.MemoryEvidence) []core.MemoryEntityAlias {
	return slices.DeleteFunc(slices.Clone(aliases), func(a core.MemoryEntityAlias) bool {
		return !slices.ContainsFunc(kept, func(e core.MemoryEvidence) bool {
			return e.SourceID == a.SourceID && e.Relation == "supports" && strings.Contains(e.Quote, a.Quote)
		})
	})
}

// memoryRecordSources resolves a record's root sources: its evidence sources
// and the current root sources of its parents.
func memoryRecordSources(ctx context.Context, q memorySQL, r core.MemoryRecord) ([]string, error) {
	var ids []string
	for _, e := range r.Evidence {
		ids = append(ids, e.SourceID)
	}
	for _, parentID := range r.Parents {
		parent, err := loadMemoryRecord(ctx, q, parentID)
		if err != nil {
			return nil, err
		}
		ids = append(ids, parent.SourceIDs...)
	}
	return uniqueMemoryStrings(ids), nil
}
