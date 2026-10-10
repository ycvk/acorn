package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func memoryID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "m_" + hex.EncodeToString(sum[:16])
}

func (s *Store) CommitMemory(ctx context.Context, mutation core.MemoryMutation) ([]core.MemoryRecord, error) {
	if mutation.Now.IsZero() {
		return nil, errors.New("memory mutation requires now")
	}
	var result []core.MemoryRecord
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		epoch, err := memoryEpoch(ctx, tx)
		if err != nil {
			return err
		}
		if epoch != mutation.Epoch {
			return core.ErrMemoryExcluded
		}
		if mutation.Lease != nil {
			if err := validateMemoryLease(ctx, tx, *mutation.Lease, mutation.Now); err != nil {
				return err
			}
		}
		for _, change := range mutation.Changes {
			if change.Supersedes != "" {
				if err := supersedeMemory(ctx, tx, change, mutation.Now); err != nil {
					return err
				}
			}
			record, err := writeMemoryChange(ctx, tx, change, mutation.Now)
			if err != nil {
				return err
			}
			result = append(result, record)
			if change.Supersedes != "" {
				if err := insertMemoryLink(ctx, tx, core.MemoryLink{FromID: record.ID, ToID: change.Supersedes, Relation: "supersedes"}); err != nil {
					return err
				}
			}
		}
		for _, link := range mutation.Links {
			if err := insertMemoryLink(ctx, tx, link); err != nil {
				return err
			}
		}
		if mutation.Lease != nil {
			if mutation.More {
				if mutation.NextCursor <= mutation.Lease.Cursor {
					return errors.New("memory cursor must advance")
				}
				_, err := tx.ExecContext(ctx, `UPDATE memory_jobs SET state='pending',cursor=?,attempts=0,available_at=?,lease_until='' WHERE id=? AND token=?`, mutation.NextCursor, formatTimestamp(mutation.Now), mutation.Lease.ID, mutation.Lease.Token)
				return err
			}
			return finishMemoryJob(ctx, tx, *mutation.Lease, "", mutation.Now)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func writeMemoryChange(ctx context.Context, q memorySQL, c core.MemoryChange, now time.Time) (core.MemoryRecord, error) {
	d := c.Draft
	d.Content = strings.TrimSpace(d.Content)
	var empty core.MemoryRecord
	if d.Content == "" || c.Reason == "" {
		return empty, errors.New("memory change requires content and reason")
	}
	if d.Kind != "fact" && d.Kind != "insight" && d.Kind != "thought" {
		return empty, fmt.Errorf("memory kind %q is invalid", d.Kind)
	}
	if d.State == "" {
		d.State = "current"
		if d.Kind == "thought" {
			d.State = "open"
		}
	}
	allowed := []string{"current", "disputed", "superseded", "retracted"}
	if d.Kind == "thought" {
		allowed = []string{"open", "resolved", "released"}
	}
	if !slices.Contains(allowed, d.State) {
		return empty, fmt.Errorf("memory state %q is invalid for %s", d.State, d.Kind)
	}
	validFrom, err := parseMemoryValidity(d.ValidFrom, "memory.valid_from")
	if err != nil {
		return empty, err
	}
	validTo, err := parseMemoryValidity(d.ValidTo, "memory.valid_to")
	if err != nil {
		return empty, err
	}
	if !validTo.IsZero() && !validFrom.IsZero() && !validTo.After(validFrom) {
		return empty, errors.New("memory valid_to must follow valid_from")
	}
	if d.ID == "" {
		origins := append([]string(nil), d.Parents...)
		for _, evidence := range d.Evidence {
			origins = append(origins, evidence.SourceID)
		}
		d.ID = memoryID(d.Kind, d.Scope, d.Content, formatZeroableTimestamp(validFrom), strings.Join(uniqueMemoryStrings(origins), ","))
	}
	record := core.MemoryRecord{ID: d.ID, Kind: d.Kind, Content: d.Content, Scope: d.Scope, State: d.State, Revision: 1,
		ValidFrom: utcOrZero(validFrom), ValidTo: utcOrZero(validTo), RecordedAt: now, UpdatedAt: now, Pinned: d.Pinned,
		Entities: uniqueMemoryStrings(d.Entities), Evidence: []core.MemoryEvidence{}, Parents: uniqueMemoryStrings(d.Parents), SourceIDs: []string{}}
	existing, err := loadMemoryRecord(ctx, q, d.ID)
	if err == nil {
		if c.ExpectedRevision == 0 {
			if existing.Content != d.Content || existing.Kind != d.Kind || existing.State != d.State {
				return empty, core.ErrMemoryConflict
			}
			record = existing
		} else {
			if existing.Revision != c.ExpectedRevision {
				return empty, core.ErrMemoryConflict
			}
			record.Revision = existing.Revision
			record.RecordedAt = existing.RecordedAt
			record.UpdatedAt = existing.UpdatedAt
		}
	} else if !errors.Is(err, core.ErrMemoryNotFound) {
		return empty, err
	} else if c.ExpectedRevision != 0 {
		return empty, core.ErrMemoryConflict
	}
	before, err := json.Marshal(existing)
	if err != nil {
		return empty, err
	}
	for _, e := range d.Evidence {
		if e.Relation == "" {
			e.Relation = "supports"
		}
		if e.Relation != "supports" && e.Relation != "contradicts" {
			return empty, errors.New("evidence relation must be supports or contradicts")
		}
		source, err := loadMemorySource(ctx, q, e.SourceID, true)
		if err != nil {
			return empty, err
		}
		if strings.TrimSpace(e.Quote) == "" || !strings.Contains(source.Content, e.Quote) {
			return empty, fmt.Errorf("evidence quote is absent from source %s", e.SourceID)
		}
		if d.Kind == "fact" && !slices.Contains([]string{"owner", "tool", "external"}, source.Speaker) {
			return empty, errors.New("fact requires owner, tool or external evidence")
		}
		if !slices.Contains(record.Evidence, e) {
			record.Evidence = append(record.Evidence, e)
		}
		record.SourceIDs = append(record.SourceIDs, e.SourceID)
	}
	for _, parentID := range record.Parents {
		if parentID == record.ID {
			return empty, errors.New("memory cannot derive from itself")
		}
		parent, err := loadMemoryRecord(ctx, q, parentID)
		if err != nil {
			return empty, err
		}
		if parent.Kind == "thought" {
			return empty, errors.New("an insight requires factual support")
		}
		record.SourceIDs = append(record.SourceIDs, parent.SourceIDs...)
	}
	if d.Kind == "fact" && len(record.Evidence) == 0 {
		return empty, errors.New("fact requires source evidence")
	}
	if d.Kind == "insight" && len(record.Parents) == 0 {
		return empty, errors.New("insight requires supporting facts")
	}
	if d.Kind == "thought" && len(record.SourceIDs) == 0 {
		return empty, errors.New("thought requires an originating source")
	}
	if err := validateMemoryAliases(&record, d.Aliases); err != nil {
		return empty, err
	}
	record.SourceIDs = uniqueMemoryStrings(record.SourceIDs)
	record.Basis = "direct"
	if d.Kind != "fact" {
		record.Basis = "inferred"

	}
	after, err := json.Marshal(record)
	if err != nil {
		return empty, err
	}
	if existing.ID != "" && string(before) == string(after) {
		return record, nil
	}
	if existing.ID != "" {
		record.Revision = existing.Revision + 1
	}
	record.UpdatedAt = now
	if err := persistMemoryRecord(ctx, q, record, c.Reason); err != nil {
		return empty, err
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM memory_links WHERE from_id=? AND relation='derives_from'`, record.ID); err != nil {
		return empty, err
	}
	for _, parent := range record.Parents {
		if err := insertMemoryLink(ctx, q, core.MemoryLink{FromID: record.ID, ToID: parent, Relation: "derives_from"}); err != nil {
			return empty, err
		}
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM memory_mentions WHERE record_id=?`, record.ID); err != nil {
		return empty, err
	}
	for _, entity := range record.Entities {
		if _, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO memory_entities(name) VALUES(?)`, entity); err != nil {
			return empty, err
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO memory_mentions(record_id,entity_id) SELECT ?,id FROM memory_entities WHERE name=?`, record.ID, entity); err != nil {
			return empty, err
		}
	}
	if d.Kind == "fact" {
		if err := enqueueMemoryJob(ctx, q, "consolidate", record.ID, fmt.Sprint(record.Revision), now); err != nil {
			return empty, err
		}
	}
	return record, nil
}

func supersedeMemory(ctx context.Context, q memorySQL, c core.MemoryChange, now time.Time) error {
	old, err := loadMemoryRecord(ctx, q, c.Supersedes)
	if err != nil {
		return err
	}
	if old.Revision != c.SupersedesRevision || old.State == "superseded" {
		return core.ErrMemoryConflict
	}
	validFrom, err := parseMemoryValidity(c.Draft.ValidFrom, "correction.valid_from")
	if err != nil {
		return err
	}
	if validFrom.IsZero() {
		return errors.New("a correction requires valid_from")
	}
	old.State = "superseded"
	old.ValidTo = validFrom
	old.Revision++
	old.UpdatedAt = now
	if err := persistMemoryRecord(ctx, q, old, c.Reason); err != nil {
		return err
	}
	rows, err := q.QueryContext(ctx, `WITH RECURSIVE affected(id) AS(SELECT from_id FROM memory_links WHERE to_id=? AND relation='derives_from' UNION SELECT l.from_id FROM memory_links l JOIN affected a ON l.to_id=a.id WHERE l.relation='derives_from') SELECT a.id FROM affected a JOIN memory_records r ON r.id=a.id WHERE r.excluded=0`, old.ID)
	if err != nil {
		return err
	}
	ids, err := memoryStringRows(rows)
	if err != nil {
		return err
	}
	for _, id := range ids {
		parent, err := loadMemoryRecord(ctx, q, id)
		if err != nil {
			return err
		}
		parent.NeedsReview = true
		parent.Revision++
		parent.UpdatedAt = now
		if err := persistMemoryRecord(ctx, q, parent, "supporting fact revised"); err != nil {
			return err
		}
		if err := enqueueMemoryJob(ctx, q, "consolidate", id, fmt.Sprint(parent.Revision), now); err != nil {
			return err
		}
	}
	return nil
}

func insertMemoryLink(ctx context.Context, q memorySQL, l core.MemoryLink) error {
	if l.FromID == l.ToID {
		return errors.New("memory link cannot reference itself")
	}
	if !slices.Contains([]string{"derives_from", "supersedes", "contradicts", "related_to"}, l.Relation) {
		return errors.New("invalid memory link relation")
	}
	if l.Relation == "derives_from" || l.Relation == "supersedes" {
		var cycle int
		err := q.QueryRowContext(ctx, `WITH RECURSIVE chain(id) AS(SELECT to_id FROM memory_links WHERE from_id=? AND relation=? UNION SELECT l.to_id FROM memory_links l JOIN chain c ON l.from_id=c.id WHERE l.relation=?) SELECT COUNT(*) FROM chain WHERE id=?`, l.ToID, l.Relation, l.Relation, l.FromID).Scan(&cycle)
		if err != nil {
			return err
		}
		if cycle > 0 {
			return errors.New("memory link would create a cycle")
		}
	}
	_, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO memory_links(from_id,to_id,relation) VALUES(?,?,?)`, l.FromID, l.ToID, l.Relation)
	return err
}

// utcOrZero matches the UTC times a stored record is read back with.
func utcOrZero(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return t.UTC()
}

func uniqueMemoryStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	slices.Sort(out)
	return out
}

func parseMemoryValidity(value, field string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return parseTimestamp(time.RFC3339Nano, value, field)
}
