package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// MemoryMessages pages canonical thread text while applying source exclusions.
// The cursor advances across excluded messages, so older pages remain reachable.
func (s *Store) MemoryMessages(ctx context.Context, session string, after, through int64, descending bool, limit int) ([]core.SessionMessageRecord, int64, error) {
	if session == "" || limit < 1 || limit > 256 {
		return nil, 0, errors.New("memory history requires session and limit 1..256")
	}
	order := "ASC"
	if descending {
		order = "DESC"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,turn_index,role,run_id,created_at FROM session_messages WHERE session_id=? AND id>? AND (?=0 OR id<=?) ORDER BY id `+order+` LIMIT ?`, session, after, through, through, limit)
	if err != nil {
		return nil, 0, err
	}
	var items []core.SessionMessageRecord
	err = scanRows(rows, func(scan func(...any) error) error {
		var m core.SessionMessageRecord
		var at string
		m.SessionID = session
		if err := scan(&m.ID, &m.TurnIndex, &m.Role, &m.RunID, &at); err != nil {
			return err
		}
		m.CreatedAt, err = parseTimestamp("2006-01-02T15:04:05.999999999Z07:00", at, "memory.message.created_at")
		items = append(items, m)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]core.SessionMessageRecord, 0, len(items))
	var cursor int64
	for _, m := range items {
		cursor = m.ID
		source, err := s.LoadMemorySource(ctx, "message:"+strconv.FormatInt(m.ID, 10))
		if errors.Is(err, core.ErrMemoryExcluded) {
			continue
		}
		if err != nil {
			return nil, 0, fmt.Errorf("history %d: %w", m.ID, err)
		}
		m.Content = source.Content
		out = append(out, m)
	}
	return out, cursor, nil
}

// SaveMemoryCheckpoint fences checkpoint persistence with the same transaction
// that reads the exclusion epoch, including a forget concurrent with a pause.
func (s *Store) SaveMemoryCheckpoint(ctx context.Context, id string, data []byte, epoch int64, now time.Time) error {
	if now.IsZero() {
		return errors.New("memory checkpoint requires now")
	}
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		current, err := memoryEpoch(ctx, tx)
		if err != nil {
			return err
		}
		if current != epoch {
			return core.ErrMemoryExcluded
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO agent_checkpoints(checkpoint_id,data,updated_at) VALUES(?,?,?) ON CONFLICT(checkpoint_id) DO UPDATE SET data=excluded.data,updated_at=excluded.updated_at`, id, data, formatTimestamp(now))
		return err
	})
}
