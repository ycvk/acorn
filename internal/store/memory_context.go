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

func (s *Store) SaveConcern(ctx context.Context, concern core.MemoryConcern, expected int64) (core.MemoryConcern, error) {
	if strings.TrimSpace(concern.Title) == "" || strings.TrimSpace(concern.Reason) == "" || concern.SourceID == "" || concern.UpdatedAt.IsZero() {
		return core.MemoryConcern{}, errors.New("concern requires title, reason, source_id and updated_at")
	}
	if !slices.Contains([]string{"active", "waiting", "resolved", "released"}, concern.State) {
		return core.MemoryConcern{}, errors.New("concern state must be active, waiting, resolved or released")
	}
	concern.RecordIDs = uniqueMemoryStrings(concern.RecordIDs)
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		if _, err := loadMemorySource(ctx, tx, concern.SourceID, true); err != nil {
			return err
		}
		if concern.ID == "" {
			concern.ID = memoryID("concern", concern.Title, concern.SourceID)
		}
		var data string
		err := tx.QueryRowContext(ctx, `SELECT data FROM memory_concerns WHERE id=?`, concern.ID).Scan(&data)
		if err == nil {
			var previous core.MemoryConcern
			if err := json.Unmarshal([]byte(data), &previous); err != nil {
				return err
			}
			if expected != previous.Revision {
				return core.ErrMemoryConflict
			}
			concern.CreatedAt = previous.CreatedAt
			concern.Revision = previous.Revision + 1
		} else if errors.Is(err, sql.ErrNoRows) {
			if expected != 0 {
				return core.ErrMemoryConflict
			}
			concern.CreatedAt = concern.UpdatedAt
			concern.Revision = 1
		} else {
			return err
		}
		for _, id := range concern.RecordIDs {
			if _, err := loadMemoryRecord(ctx, tx, id); err != nil {
				return err
			}
		}
		encoded, err := json.Marshal(concern)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO memory_concerns(id,title,state,revision,source_id,data) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET title=excluded.title,state=excluded.state,revision=excluded.revision,source_id=excluded.source_id,data=excluded.data`, concern.ID, concern.Title, concern.State, concern.Revision, concern.SourceID, string(encoded)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO memory_concern_revisions(concern_id,revision,data) VALUES(?,?,?)`, concern.ID, concern.Revision, string(encoded)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM memory_concern_links WHERE concern_id=?`, concern.ID); err != nil {
			return err
		}
		for _, id := range concern.RecordIDs {
			if _, err = tx.ExecContext(ctx, `INSERT INTO memory_concern_links(concern_id,record_id) VALUES(?,?)`, concern.ID, id); err != nil {
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

func (s *Store) ListConcerns(ctx context.Context, activeOnly bool) ([]core.MemoryConcern, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.data FROM memory_concerns c WHERE (?=0 OR c.state IN ('active','waiting')) AND NOT EXISTS(SELECT 1 FROM memory_exclusions x WHERE x.source_id=c.source_id) AND NOT EXISTS(SELECT 1 FROM memory_concern_links l JOIN memory_records r ON r.id=l.record_id WHERE l.concern_id=c.id AND r.excluded=1) ORDER BY c.id`, activeOnly)
	if err != nil {
		return nil, err
	}
	out := []core.MemoryConcern{}
	err = scanRows(rows, func(scan func(...any) error) error {
		var data string
		if err := scan(&data); err != nil {
			return err
		}
		var c core.MemoryConcern
		if err := json.Unmarshal([]byte(data), &c); err != nil {
			return err
		}
		out = append(out, c)
		return nil
	})
	return out, err
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
			if _, err := loadMemorySource(ctx, tx, id, true); err != nil {
				return err
			}
		}
		encoded, err := json.Marshal(uniqueMemoryStrings(summary.SourceIDs))
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO thread_summaries(session_id,through_message_id,epoch,content,sources_json,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(session_id) DO UPDATE SET through_message_id=excluded.through_message_id,epoch=excluded.epoch,content=excluded.content,sources_json=excluded.sources_json,updated_at=excluded.updated_at WHERE thread_summaries.through_message_id<=excluded.through_message_id`, summary.SessionID, summary.ThroughMessageID, summary.Epoch, summary.Content, string(encoded), formatTimestamp(summary.UpdatedAt))
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
		return nil
	})
}

func (s *Store) LoadThreadSummary(ctx context.Context, sessionID string) (*core.ThreadSummary, error) {
	var out core.ThreadSummary
	var sourceJSON, updated string
	err := s.db.QueryRowContext(ctx, `SELECT session_id,through_message_id,epoch,content,sources_json,updated_at FROM thread_summaries WHERE session_id=? AND NOT EXISTS(SELECT 1 FROM json_each(sources_json) j JOIN memory_exclusions x ON x.source_id=j.value)`, sessionID).Scan(&out.SessionID, &out.ThroughMessageID, &out.Epoch, &out.Content, &sourceJSON, &updated)
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
