package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// addConcernThreads (15 to 16) records the conversation each concern was
// created in: the conversation of its first revision's source, or of that
// source's run. A concern whose conversation is gone is no longer listed.
func addConcernThreads(ctx context.Context, tx *sql.Tx) error {
	columns := "id, title, state, reason, source_id, revision, review_at, created_at, updated_at"
	for _, statement := range []string{
		`CREATE TEMP TABLE memory_concerns_v15 AS SELECT ` + columns + ` FROM memory_concerns`,
		`DROP TABLE memory_concerns`,
		memoryConcernsTable,
		`INSERT INTO memory_concerns(` + columns + `, thread_id) SELECT ` + columns + `,
		 COALESCE((SELECT COALESCE(NULLIF(s.session_id, ''), (SELECT r.session_id FROM runs r WHERE r.run_id = s.run_id), '')
		  FROM memory_concern_revisions v JOIN memory_sources s ON s.id = v.source_id WHERE v.concern_id = c.id AND v.revision = 1), '')
		 FROM memory_concerns_v15 c`,
		`DROP TABLE memory_concerns_v15`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// addConcernOutcomes (16 to 17) adds the outcome, what a concern hands the
// owner when done, to each concern and revision; existing ones have none.
func addConcernOutcomes(ctx context.Context, tx *sql.Tx) error {
	concerns := "id, title, state, reason, source_id, revision, review_at, created_at, updated_at, thread_id"
	revisions := "concern_id, revision, title, state, reason, source_id, review_at, at"
	for _, statement := range []string{
		`CREATE TEMP TABLE memory_concerns_v16 AS SELECT ` + concerns + ` FROM memory_concerns`,
		`CREATE TEMP TABLE memory_concern_revisions_v16 AS SELECT ` + revisions + ` FROM memory_concern_revisions`,
		`DROP TABLE memory_concern_revisions`,
		`DROP TABLE memory_concerns`,
		memoryConcernsTable,
		memoryConcernRevisionsTable,
		`INSERT INTO memory_concerns(` + concerns + `) SELECT ` + concerns + ` FROM memory_concerns_v16`,
		`INSERT INTO memory_concern_revisions(` + revisions + `) SELECT ` + revisions + ` FROM memory_concern_revisions_v16`,
		`DROP TABLE memory_concerns_v16`,
		`DROP TABLE memory_concern_revisions_v16`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// SaveConcern writes a concern's next revision. A new concern belongs to
// concern.ThreadID, which must exist; a revision keeps the stored thread and
// fails with core.ErrMemoryExcluded once that conversation was deleted. A
// revision without an outcome keeps the stored one. Resolving needs the
// owner's confirmation or a tool result as source, and only the owner's
// confirmation for a concern with an outcome.
func (s *Store) SaveConcern(ctx context.Context, concern core.MemoryConcern, expected int64) (core.MemoryConcern, error) {
	if strings.TrimSpace(concern.Title) == "" || strings.TrimSpace(concern.Reason) == "" || concern.SourceID == "" || concern.UpdatedAt.IsZero() {
		return core.MemoryConcern{}, errors.New("concern requires title, reason, source_id and updated_at")
	}
	if !slices.Contains([]string{"active", "waiting", "resolved", "released"}, concern.State) {
		return core.MemoryConcern{}, errors.New("concern state must be active, waiting, resolved or released")
	}
	concern.RecordIDs = uniqueMemoryStrings(concern.RecordIDs)
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		source, err := loadMemorySource(ctx, tx, concern.SourceID, sourceMemory)
		if err != nil {
			return err
		}
		if concern.ID == "" {
			concern.ID = memoryID("concern", concern.Title, concern.SourceID)
		}
		var revision int64
		var created, thread, outcome string
		err = tx.QueryRowContext(ctx, `SELECT revision,created_at,thread_id,outcome FROM memory_concerns WHERE id=?`, concern.ID).Scan(&revision, &created, &thread, &outcome)
		switch {
		case err == nil:
			if expected != revision {
				return core.ErrMemoryConflict
			}
			if concern.CreatedAt, err = parseTimestamp(fixedTimestampLayout, created, "concern created_at"); err != nil {
				return err
			}
			concern.Revision = revision + 1
			concern.ThreadID = thread
			if concern.Outcome == "" {
				concern.Outcome = outcome
			}
		case errors.Is(err, sql.ErrNoRows):
			if expected != 0 {
				return core.ErrMemoryConflict
			}
			if concern.ThreadID == "" {
				return errors.New("new concern requires thread_id")
			}
			concern.CreatedAt = concern.UpdatedAt
			concern.Revision = 1
		default:
			return err
		}
		var open int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE session_id=?`, concern.ThreadID).Scan(&open); err != nil {
			return err
		}
		switch {
		case open == 0 && concern.Revision == 1:
			return fmt.Errorf("%w: concern thread_id %s", core.ErrSessionNotFound, concern.ThreadID)
		case open == 0:
			return fmt.Errorf("%w: concern %s belongs to a deleted conversation", core.ErrMemoryExcluded, concern.ID)
		}
		if concern.State == "resolved" {
			switch {
			case concern.Outcome != "" && source.Speaker != "owner":
				return errors.New("a concern with an outcome is resolved only by the owner's confirmation; cite the owner's message")
			case source.Speaker != "owner" && source.Speaker != "tool":
				return errors.New("resolving a concern requires owner confirmation or execution evidence")
			}
		}
		for _, id := range concern.RecordIDs {
			if _, err := loadMemoryRecord(ctx, tx, id); err != nil {
				return err
			}
		}
		reviewAt, updatedAt := formatZeroableTimestamp(concern.ReviewAt), formatTimestamp(concern.UpdatedAt)
		if _, err = tx.ExecContext(ctx, `INSERT INTO memory_concerns(id,title,state,reason,source_id,revision,review_at,created_at,updated_at,thread_id,outcome) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET title=excluded.title,state=excluded.state,reason=excluded.reason,source_id=excluded.source_id,revision=excluded.revision,review_at=excluded.review_at,updated_at=excluded.updated_at,outcome=excluded.outcome`, concern.ID, concern.Title, concern.State, concern.Reason, concern.SourceID, concern.Revision, reviewAt, formatTimestamp(concern.CreatedAt), updatedAt, concern.ThreadID, concern.Outcome); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO memory_concern_revisions(concern_id,revision,title,state,reason,source_id,review_at,at,outcome) VALUES(?,?,?,?,?,?,?,?,?)`, concern.ID, concern.Revision, concern.Title, concern.State, concern.Reason, concern.SourceID, reviewAt, updatedAt, concern.Outcome); err != nil {
			return err
		}
		for _, id := range concern.RecordIDs {
			if _, err = tx.ExecContext(ctx, `INSERT INTO memory_concern_records(concern_id,revision,record_id) VALUES(?,?,?)`, concern.ID, concern.Revision, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return core.MemoryConcern{}, fmt.Errorf("save concern: %w", err)
	}
	return concern, nil
}

// ListConcerns leaves out concerns whose source or linked records were
// forgotten and those created in a conversation the owner deleted.
func (s *Store) ListConcerns(ctx context.Context, activeOnly bool) ([]core.MemoryConcern, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.title,c.state,c.reason,c.source_id,c.revision,c.review_at,c.created_at,c.updated_at,c.thread_id,c.outcome,
 (SELECT json_group_array(l.record_id) FROM (SELECT record_id FROM memory_concern_records WHERE concern_id=c.id AND revision=c.revision ORDER BY rowid) l)
 FROM memory_concerns c WHERE (?=0 OR c.state IN ('active','waiting'))
 AND EXISTS(SELECT 1 FROM sessions t WHERE t.session_id=c.thread_id)
 AND NOT EXISTS(SELECT 1 FROM memory_exclusions x WHERE x.source_id=c.source_id)
 AND NOT EXISTS(SELECT 1 FROM memory_concern_records l JOIN memory_records r ON r.id=l.record_id WHERE l.concern_id=c.id AND l.revision=c.revision AND r.excluded=1)
 ORDER BY c.id`, activeOnly)
	if err != nil {
		return nil, err
	}
	out := []core.MemoryConcern{}
	err = scanRows(rows, func(scan func(...any) error) error {
		var c core.MemoryConcern
		var reviewAt, created, updated, records string
		if err := scan(&c.ID, &c.Title, &c.State, &c.Reason, &c.SourceID, &c.Revision, &reviewAt, &created, &updated, &c.ThreadID, &c.Outcome, &records); err != nil {
			return err
		}
		var err error
		if c.ReviewAt, err = parseOptionalTime(reviewAt, "concern review_at"); err != nil {
			return err
		}
		if c.CreatedAt, err = parseTimestamp(fixedTimestampLayout, created, "concern created_at"); err != nil {
			return err
		}
		if c.UpdatedAt, err = parseTimestamp(fixedTimestampLayout, updated, "concern updated_at"); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(records), &c.RecordIDs); err != nil {
			return err
		}
		out = append(out, c)
		return nil
	})
	return out, err
}

// DeletedThreadConcernIDs lists the concerns created in conversations the
// owner deleted.
func (s *Store) DeletedThreadConcernIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM memory_concerns c WHERE NOT EXISTS(SELECT 1 FROM sessions t WHERE t.session_id=c.thread_id) ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list concerns of deleted conversations: %w", err)
	}
	return memoryStringRows(rows)
}

func (s *Store) SaveThreadSummary(ctx context.Context, summary core.ThreadSummary) error {
	if summary.SessionID == "" || summary.Content == "" || summary.ThroughMessageID <= 0 || len(summary.SourceIDs) == 0 || summary.UpdatedAt.IsZero() {
		return errors.New("thread summary requires session, content, through_message_id, sources and updated_at")
	}
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		epoch, err := memoryEpoch(ctx, tx)
		if err != nil {
			return err
		}
		if epoch != summary.Epoch {
			return core.ErrMemoryExcluded
		}
		for _, id := range summary.SourceIDs {
			if _, err := loadMemorySource(ctx, tx, id, sourceMemory); err != nil {
				return err
			}
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO thread_summaries(session_id,through_message_id,epoch,content,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(session_id) DO UPDATE SET through_message_id=excluded.through_message_id,epoch=excluded.epoch,content=excluded.content,updated_at=excluded.updated_at WHERE thread_summaries.through_message_id<=excluded.through_message_id`, summary.SessionID, summary.ThroughMessageID, summary.Epoch, summary.Content, formatTimestamp(summary.UpdatedAt))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return core.ErrMemoryConflict
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM thread_summary_sources WHERE session_id=?`, summary.SessionID); err != nil {
			return err
		}
		for _, id := range uniqueMemoryStrings(summary.SourceIDs) {
			if _, err := tx.ExecContext(ctx, `INSERT INTO thread_summary_sources(session_id,source_id) VALUES(?,?)`, summary.SessionID, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// LoadThreadSummary reads a summary that no exclusion reaches: a summary read
// its sources without the fragments excluded before it was written.
func (s *Store) LoadThreadSummary(ctx context.Context, sessionID string) (*core.ThreadSummary, error) {
	var out core.ThreadSummary
	var sourceJSON, updated string
	err := s.db.QueryRowContext(ctx, `SELECT t.session_id,t.through_message_id,t.epoch,t.content,
 (SELECT json_group_array(s.source_id) FROM (SELECT source_id FROM thread_summary_sources WHERE session_id=t.session_id ORDER BY rowid) s),t.updated_at
 FROM thread_summaries t WHERE t.session_id=? AND NOT EXISTS(SELECT 1 FROM thread_summary_sources s JOIN memory_exclusions x ON x.source_id=s.source_id WHERE s.session_id=t.session_id AND (x.quote='' OR x.epoch>t.epoch))`, sessionID).Scan(&out.SessionID, &out.ThroughMessageID, &out.Epoch, &out.Content, &sourceJSON, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(sourceJSON), &out.SourceIDs); err != nil {
		return nil, err
	}
	if out.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return nil, err
	}
	return &out, nil
}
