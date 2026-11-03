package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
		m.CreatedAt, err = parseTimestamp(at, "memory.message.created_at")
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
		source, err := s.LoadThreadSource(ctx, core.MessageSourceID(m.ID))
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

// ThreadToolCalls reads the successful calls of tools in the succeeded or
// failed runs of the session's turns through a message, leaving out
// sub-runs. Each call pairs its
// result event, which is the call's memory source, with the arguments of the
// call that started it.
func (s *Store) ThreadToolCalls(ctx context.Context, session string, through int64, tools []string) ([]core.ToolCallRecord, error) {
	marks, names := inClause(tools)
	if session == "" || through <= 0 || len(names) == 0 {
		return nil, errors.New("thread tool calls require session, through and tools")
	}
	args := append([]any{session, session, through}, names...)
	rows, err := s.db.QueryContext(ctx, `SELECT 'event:'||r.sequence, json_extract(r.payload_json,'$.tool_name'),
 COALESCE((SELECT json_extract(c.payload_json,'$.arguments') FROM events c WHERE c.run_id=r.run_id AND c.kind='tool.call.started' AND c.sequence<r.sequence
  AND json_extract(c.payload_json,'$.tool_call_id')=json_extract(r.payload_json,'$.tool_call_id') ORDER BY c.sequence DESC LIMIT 1),''),
 CASE WHEN json_valid(json_extract(r.payload_json,'$.output')) THEN json_type(json_extract(r.payload_json,'$.output'))='object' ELSE 0 END
 FROM runs u JOIN events r ON r.run_id=u.run_id AND r.kind='tool.call.succeeded'
 JOIN memory_sources m ON m.id='event:'||r.sequence
 WHERE u.session_id=? AND u.status IN ('succeeded','failed') AND NOT EXISTS(SELECT 1 FROM sub_runs WHERE sub_runs.run_id=u.run_id)
 AND u.turn_index IN (SELECT turn_index FROM session_messages WHERE session_id=? AND id<=?)
 AND json_extract(r.payload_json,'$.tool_name') IN (`+marks+`)
 AND NOT EXISTS(SELECT 1 FROM memory_exclusions x WHERE x.source_id=m.id)
 ORDER BY r.sequence`, args...)
	if err != nil {
		return nil, err
	}
	var calls []core.ToolCallRecord
	err = scanRows(rows, func(scan func(...any) error) error {
		var call core.ToolCallRecord
		if err := scan(&call.SourceID, &call.Name, &call.Arguments, &call.ObjectResult); err != nil {
			return err
		}
		calls = append(calls, call)
		return nil
	})
	return calls, err
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
