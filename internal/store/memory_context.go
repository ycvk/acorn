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

// addWatchedQuestions (39 to 40) adds the mark of a question watched for the
// owner to each concern; existing concerns watch none.
func addWatchedQuestions(ctx context.Context, tx *sql.Tx) error {
	concerns := "id, title, state, reason, source_id, revision, review_at, created_at, updated_at, thread_id, outcome"
	revisions := "concern_id, revision, title, state, reason, source_id, review_at, at, outcome"
	for _, statement := range []string{
		`CREATE TEMP TABLE memory_concerns_v39 AS SELECT ` + concerns + ` FROM memory_concerns`,
		`CREATE TEMP TABLE memory_concern_revisions_v39 AS SELECT ` + revisions + ` FROM memory_concern_revisions`,
		`DROP TABLE memory_concern_revisions`,
		`DROP TABLE memory_concerns`,
		memoryConcernsTable,
		memoryConcernRevisionsTable,
		`INSERT INTO memory_concerns(` + concerns + `) SELECT ` + concerns + ` FROM memory_concerns_v39`,
		`INSERT INTO memory_concern_revisions(` + revisions + `) SELECT ` + revisions + ` FROM memory_concern_revisions_v39`,
		`DROP TABLE memory_concerns_v39`,
		`DROP TABLE memory_concern_revisions_v39`,
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
// revision without a title, outcome or record links keeps the stored ones,
// and whether it watches a question stays as it was created.
// Resolving needs the owner's confirmation or a tool result as source, and
// only the owner's confirmation for a concern with an outcome. A watched
// question is opened under checkNewQuestion, and resolving one schedules
// telling the owner in the same transaction.
func (s *Store) SaveConcern(ctx context.Context, concern core.MemoryConcern, expected int64) (core.MemoryConcern, error) {
	if strings.TrimSpace(concern.Reason) == "" || concern.SourceID == "" || concern.UpdatedAt.IsZero() {
		return core.MemoryConcern{}, errors.New("concern requires reason, source_id and updated_at")
	}
	if concern.ID == "" && strings.TrimSpace(concern.Title) == "" {
		return core.MemoryConcern{}, errors.New("new concern requires title")
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
		var title, created, thread, outcome, state string
		var question bool
		err = tx.QueryRowContext(ctx, `SELECT title,revision,created_at,thread_id,outcome,question,state FROM memory_concerns WHERE id=?`, concern.ID).Scan(&title, &revision, &created, &thread, &outcome, &question, &state)
		switch {
		case err == nil:
			if expected != revision {
				return core.ErrMemoryConflict
			}
			concern.Question = question
			if concern.CreatedAt, err = parseTimestamp(created, "concern created_at"); err != nil {
				return err
			}
			concern.Revision = revision + 1
			concern.ThreadID = thread
			if strings.TrimSpace(concern.Title) == "" {
				concern.Title = title
			}
			if concern.Outcome == "" {
				concern.Outcome = outcome
			}
			if len(concern.RecordIDs) == 0 {
				rows, err := tx.QueryContext(ctx, `SELECT record_id FROM memory_concern_records WHERE concern_id=? AND revision=? ORDER BY rowid`, concern.ID, revision)
				if err != nil {
					return err
				}
				if concern.RecordIDs, err = memoryStringRows(rows); err != nil {
					return err
				}
			}
		case errors.Is(err, sql.ErrNoRows):
			if expected != 0 {
				return core.ErrMemoryConflict
			}
			if strings.TrimSpace(concern.Title) == "" {
				return errors.New("new concern requires title")
			}
			if concern.ThreadID == "" {
				return errors.New("new concern requires thread_id")
			}
			if concern.Question {
				if err := checkNewQuestion(ctx, tx, concern, source); err != nil {
					return err
				}
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
		if _, err = tx.ExecContext(ctx, `INSERT INTO memory_concerns(id,title,state,reason,source_id,revision,review_at,created_at,updated_at,thread_id,outcome,question) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET title=excluded.title,state=excluded.state,reason=excluded.reason,source_id=excluded.source_id,revision=excluded.revision,review_at=excluded.review_at,updated_at=excluded.updated_at,outcome=excluded.outcome`, concern.ID, concern.Title, concern.State, concern.Reason, concern.SourceID, concern.Revision, reviewAt, formatTimestamp(concern.CreatedAt), updatedAt, concern.ThreadID, concern.Outcome, concern.Question); err != nil {
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
		if concern.Question && concern.State == "resolved" && state != "resolved" {
			return scheduleQuestionAnswer(ctx, tx, concern, source)
		}
		return nil
	})
	if err != nil {
		return core.MemoryConcern{}, fmt.Errorf("save concern: %w", err)
	}
	return concern, nil
}

// checkNewQuestion refuses a watched question that is not opened from the
// owner's own message, that also has an outcome, or that would be one more
// than core.MaxWatchedQuestions.
func checkNewQuestion(ctx context.Context, tx *sql.Tx, concern core.MemoryConcern, source core.MemorySource) error {
	switch {
	case source.Kind != "message" || source.Speaker != "owner":
		return errors.New("a watched question is opened from the owner's own message; cite that message as source_id")
	case concern.Outcome != "":
		return errors.New("a concern watches a question or has an outcome, not both")
	}
	var watched int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_concerns c WHERE c.question=1 AND c.state='active' AND `+visibleConcern).Scan(&watched); err != nil {
		return err
	}
	if watched >= core.MaxWatchedQuestions {
		return fmt.Errorf("%d questions are already watched, the most at once; this one is not watched, so do not tell the owner it is", watched)
	}
	return nil
}

// scheduleQuestionAnswer makes the commitment that tells the owner a watched
// question has a new answer, in the conversation they asked in. A run in
// that conversation is telling them already and needs none.
func scheduleQuestionAnswer(ctx context.Context, tx *sql.Tx, concern core.MemoryConcern, source core.MemorySource) error {
	session := source.SessionID
	if session == "" {
		if err := tx.QueryRowContext(ctx, `SELECT session_id FROM runs WHERE run_id=?`, source.RunID).Scan(&session); err != nil {
			return fmt.Errorf("conversation of run %s: %w", source.RunID, err)
		}
	}
	if session == concern.ThreadID {
		return nil
	}
	at := formatTimestamp(concern.UpdatedAt)
	_, err := tx.ExecContext(ctx, `INSERT INTO commitments(content,state,session_id,source_run_id,concern_id,wake_at,recurrence,created_at,updated_at) VALUES(?,'scheduled',?,?,?,?,'',?,?)`, concern.Title, concern.ThreadID, source.RunID, concern.ID, at, at, at)
	return err
}

// ReleaseStaleQuestions releases the watched questions asked before
// askedBefore that still have no new answer, each with a revision that keeps
// its source, and returns them as released.
func (s *Store) ReleaseStaleQuestions(ctx context.Context, askedBefore, now time.Time) ([]core.MemoryConcern, error) {
	concerns, err := s.ListConcerns(ctx, true)
	if err != nil {
		return nil, err
	}
	released := []core.MemoryConcern{}
	for _, c := range concerns {
		if !c.Question || c.State != "active" || !c.CreatedAt.Before(askedBefore) {
			continue
		}
		next := core.MemoryConcern{ID: c.ID, State: "released", Reason: fmt.Sprintf("watched since %s without a new answer", c.CreatedAt.UTC().Format("2006-01-02")), SourceID: c.SourceID, UpdatedAt: now}
		saved, err := s.SaveConcern(ctx, next, c.Revision)
		if err != nil {
			return nil, fmt.Errorf("release watched question %s: %w", c.ID, err)
		}
		released = append(released, saved)
	}
	return released, nil
}

// visibleConcern holds for a concern c whose conversation still exists, whose
// source is not excluded and whose current records are not excluded.
const visibleConcern = `EXISTS(SELECT 1 FROM sessions t WHERE t.session_id=c.thread_id)
 AND NOT EXISTS(SELECT 1 FROM memory_exclusions x WHERE x.source_id=c.source_id)
 AND NOT EXISTS(SELECT 1 FROM memory_concern_records l JOIN memory_records r ON r.id=l.record_id WHERE l.concern_id=c.id AND l.revision=c.revision AND r.excluded=1)`

// ListConcerns leaves out concerns whose source or linked records were
// forgotten and those created in a conversation the owner deleted.
func (s *Store) ListConcerns(ctx context.Context, activeOnly bool) ([]core.MemoryConcern, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.title,c.state,c.reason,c.source_id,c.revision,c.review_at,c.created_at,c.updated_at,c.thread_id,c.outcome,c.question,
 (SELECT json_group_array(l.record_id) FROM (SELECT record_id FROM memory_concern_records WHERE concern_id=c.id AND revision=c.revision ORDER BY rowid) l)
 FROM memory_concerns c WHERE (?=0 OR c.state IN ('active','waiting')) AND `+visibleConcern+`
 ORDER BY c.id`, activeOnly)
	if err != nil {
		return nil, err
	}
	out := []core.MemoryConcern{}
	err = scanRows(rows, func(scan func(...any) error) error {
		var c core.MemoryConcern
		var reviewAt, created, updated, records string
		if err := scan(&c.ID, &c.Title, &c.State, &c.Reason, &c.SourceID, &c.Revision, &reviewAt, &created, &updated, &c.ThreadID, &c.Outcome, &c.Question, &records); err != nil {
			return err
		}
		var err error
		if c.ReviewAt, err = parseOptionalTime(reviewAt, "concern review_at"); err != nil {
			return err
		}
		if c.CreatedAt, err = parseTimestamp(created, "concern created_at"); err != nil {
			return err
		}
		if c.UpdatedAt, err = parseTimestamp(updated, "concern updated_at"); err != nil {
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
	if out.UpdatedAt, err = parseTimestamp(updated, "thread_summaries.updated_at"); err != nil {
		return nil, err
	}
	return &out, nil
}
