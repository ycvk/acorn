package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// queuedMessagesTable is shared with the migration that adds it. A row is an
// owner message sent while a run of its conversation was going; it becomes a
// conversation message when a run takes it, so the conversation keeps the
// order in which the model saw its messages. file_ids is a JSON array of the
// unsent owner files the message sends.
const queuedMessagesTable = `CREATE TABLE IF NOT EXISTS queued_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    content TEXT NOT NULL,
    file_ids TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_queued_messages_session ON queued_messages(session_id, id);`

// addQueuedMessages (30 to 31) adds the owner messages that wait for the run
// of their conversation.
func addQueuedMessages(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, queuedMessagesTable)
	return err
}

// SendOwnerMessage implements core.SessionStore.
func (s *Store) SendOwnerMessage(ctx context.Context, message core.OwnerMessage) (core.SentOwnerMessage, error) {
	content := strings.TrimSpace(message.Content)
	switch {
	case strings.TrimSpace(message.SessionID) == "" || strings.TrimSpace(message.RunID) == "":
		return core.SentOwnerMessage{}, errors.New("send owner message: SessionID and RunID are required")
	case content == "" && len(message.FileIDs) == 0:
		return core.SentOwnerMessage{}, errors.New("send owner message: content or files are required")
	}
	queued := core.QueuedMessage{SessionID: message.SessionID, Content: content, FileIDs: append([]string{}, message.FileIDs...), CreatedAt: time.Now().UTC()}
	var sent core.SentOwnerMessage
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE session_id = ?`, message.SessionID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return fmt.Errorf("%w: %s", core.ErrSessionNotFound, message.SessionID)
		}
		active, err := activeRunOf(ctx, tx, message.SessionID)
		if err != nil {
			return err
		}
		if active == "" {
			start, err := startQueuedRun(ctx, tx, s.process, message.SessionID, message.RunID, &queued)
			sent = core.SentOwnerMessage{RunID: message.RunID, Started: start}
			return err
		}
		if err := queueMessage(ctx, tx, &queued); err != nil {
			return err
		}
		sent = core.SentOwnerMessage{RunID: active, Queued: &queued}
		return nil
	})
	if err != nil {
		return core.SentOwnerMessage{}, fmt.Errorf("send owner message to %s: %w", message.SessionID, err)
	}
	return sent, nil
}

// LatestOwnerMessageAt returns when the owner last wrote in the conversation:
// the time of its newest message of theirs, or the zero time when they never
// wrote there. A message that still waits for a run does not count.
func (s *Store) LatestOwnerMessageAt(ctx context.Context, sessionID string) (time.Time, error) {
	var at sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(created_at) FROM session_messages WHERE session_id = ? AND role = 'user'`, sessionID).Scan(&at); err != nil {
		return time.Time{}, fmt.Errorf("latest owner message of %s: %w", sessionID, err)
	}
	if !at.Valid {
		return time.Time{}, nil
	}
	return parseTimestamp(at.String, "session_messages.created_at")
}

// TakeQueuedMessages implements core.SessionStore.
func (s *Store) TakeQueuedMessages(ctx context.Context, runID string) ([]core.SessionMessageRecord, error) {
	var taken []core.SessionMessageRecord
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		var sessionID, status string
		var turn int
		err := tx.QueryRowContext(ctx, `SELECT session_id, turn_index, status FROM runs WHERE run_id = ?`, runID).Scan(&sessionID, &turn, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", core.ErrRunNotFound, runID)
		}
		if err != nil || status != string(core.RunStatusRunning) || sessionID == "" {
			return err
		}
		queued, err := queuedMessagesIn(ctx, tx, sessionID)
		if err != nil {
			return err
		}
		for _, message := range queued {
			// Files reach the workstation before a run starts, so a message
			// that sends them waits for the next run, and so do the ones after it.
			if len(message.FileIDs) > 0 {
				break
			}
			record, err := takeQueuedMessage(ctx, tx, message, runID, turn)
			if err != nil {
				return err
			}
			if _, err := appendEvent(ctx, tx, runID, "run.input", map[string]any{
				"queued_id":  strconv.FormatInt(message.ID, 10),
				"message_id": strconv.FormatInt(record.ID, 10),
				"text":       record.Content,
			}); err != nil {
				return err
			}
			taken = append(taken, record)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("take queued messages for run %s: %w", runID, err)
	}
	return taken, nil
}

// StartQueuedRun implements core.SessionStore.
func (s *Store) StartQueuedRun(ctx context.Context, sessionID, runID string) (*core.RunStart, error) {
	var start *core.RunStart
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		active, err := activeRunOf(ctx, tx, sessionID)
		if err != nil || active != "" {
			return err
		}
		start, err = startQueuedRun(ctx, tx, s.process, sessionID, runID, nil)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("start queued run in %s: %w", sessionID, err)
	}
	return start, nil
}

// ListQueuedMessages implements core.SessionStore.
func (s *Store) ListQueuedMessages(ctx context.Context, sessionID string) ([]core.QueuedMessage, error) {
	messages, err := queuedMessagesIn(ctx, s.db, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list queued messages of %s: %w", sessionID, err)
	}
	return messages, nil
}

// ListQueuedSessions implements core.SessionStore.
func (s *Store) ListQueuedSessions(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT session_id FROM queued_messages ORDER BY session_id`)
	if err != nil {
		return nil, fmt.Errorf("list queued sessions: %w", err)
	}
	return memoryStringRows(rows)
}

// activeRunOf names the newest run of the conversation that runs or waits
// for the owner, else "". Sub-runs belong to the run that delegated them.
func activeRunOf(ctx context.Context, tx *sql.Tx, sessionID string) (string, error) {
	var runID string
	err := tx.QueryRowContext(ctx, `SELECT run_id FROM runs r WHERE session_id = ? AND status IN (?, ?) AND `+notSubRun+` ORDER BY created_at DESC, run_id DESC LIMIT 1`,
		sessionID, string(core.RunStatusRunning), string(core.RunStatusInterrupted)).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return runID, err
}

// queueMessage stores message as waiting in its conversation and sets its id.
// Its files must be unsent owner files of the conversation that no other
// queued message sends.
func queueMessage(ctx context.Context, tx *sql.Tx, message *core.QueuedMessage) error {
	files, err := json.Marshal(message.FileIDs)
	if err != nil {
		return err
	}
	if len(message.FileIDs) > 0 {
		var unsent, taken int
		if err := tx.QueryRowContext(ctx, `SELECT
			(SELECT COUNT(*) FROM thread_files WHERE session_id = ?1 AND origin = 'owner' AND message_id = 0 AND file_id IN (SELECT value FROM json_each(?2))),
			(SELECT COUNT(*) FROM queued_messages q, json_each(q.file_ids) f WHERE q.session_id = ?1 AND f.value IN (SELECT value FROM json_each(?2)))`,
			message.SessionID, string(files)).Scan(&unsent, &taken); err != nil {
			return err
		}
		if unsent != len(message.FileIDs) || taken > 0 {
			return fmt.Errorf("%w: a queued message sends unsent owner files of thread %s that no other queued message sends", core.ErrInvalidThreadFile, message.SessionID)
		}
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO queued_messages(session_id, content, file_ids, created_at) VALUES(?, ?, ?, ?)`,
		message.SessionID, message.Content, string(files), formatTimestamp(message.CreatedAt))
	if err != nil {
		return err
	}
	if message.ID, err = result.LastInsertId(); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE sessions SET updated_at = ? WHERE session_id = ?`, formatTimestamp(message.CreatedAt), message.SessionID)
	return err
}

// startQueuedRun creates runID in the conversation as its next turn, on the
// messages queued there followed by extra, and returns nil when there are
// none. The messages become the turn's input in the order they were sent.
func startQueuedRun(ctx context.Context, tx *sql.Tx, process, sessionID, runID string, extra *core.QueuedMessage) (*core.RunStart, error) {
	queued, err := queuedMessagesIn(ctx, tx, sessionID)
	if err != nil {
		return nil, err
	}
	if extra != nil {
		queued = append(queued, *extra)
	}
	if len(queued) == 0 {
		return nil, nil
	}
	start := &core.RunStart{RunID: runID, SessionID: sessionID}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(turn_index), 0) + 1 FROM session_messages WHERE session_id = ?`, sessionID).Scan(&start.TurnIndex); err != nil {
		return nil, err
	}
	var texts []string
	for _, message := range queued {
		record, err := takeQueuedMessage(ctx, tx, message, runID, start.TurnIndex)
		if err != nil {
			return nil, err
		}
		start.Messages = append(start.Messages, record)
		if record.Content != "" {
			texts = append(texts, record.Content)
		}
	}
	start.Input = strings.Join(texts, "\n\n")
	if _, err := tx.ExecContext(ctx, `INSERT INTO runs(run_id, session_id, turn_index, status, input_text, output_text, error_text, created_at, finished_at) VALUES(?, ?, ?, ?, ?, '', '', ?, '')`,
		runID, sessionID, start.TurnIndex, string(core.RunStatusRunning), start.Input, formatTimestamp(time.Now())); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO run_processes(run_id, process) VALUES(?, ?)`, runID, process); err != nil {
		return nil, err
	}
	return start, nil
}

// takeQueuedMessage writes message into its conversation as an input of runID
// in turn, sends its files with it and drops it from the queue. A message
// that never waited has no id.
func takeQueuedMessage(ctx context.Context, tx *sql.Tx, message core.QueuedMessage, runID string, turn int) (core.SessionMessageRecord, error) {
	record := core.SessionMessageRecord{SessionID: message.SessionID, TurnIndex: turn, Role: "user", Content: message.Content, RunID: runID, CreatedAt: message.CreatedAt}
	result, err := tx.ExecContext(ctx, `INSERT INTO session_messages(session_id, turn_index, role, content, run_id, created_at) VALUES(?, ?, 'user', ?, ?, ?)`,
		record.SessionID, turn, record.Content, runID, formatTimestamp(record.CreatedAt))
	if err != nil {
		return record, err
	}
	if record.ID, err = result.LastInsertId(); err != nil {
		return record, err
	}
	if len(message.FileIDs) > 0 {
		if err := attachThreadFiles(ctx, tx, message.SessionID, record.ID, message.FileIDs); err != nil {
			return record, err
		}
	}
	if message.ID > 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM queued_messages WHERE id = ?`, message.ID); err != nil {
			return record, err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE sessions SET updated_at = ? WHERE session_id = ?`, formatTimestamp(time.Now()), message.SessionID)
	return record, err
}

func queuedMessagesIn(ctx context.Context, q memorySQL, sessionID string) ([]core.QueuedMessage, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, session_id, content, file_ids, created_at FROM queued_messages WHERE session_id = ? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	messages := []core.QueuedMessage{}
	err = scanRows(rows, func(scan func(...any) error) error {
		var message core.QueuedMessage
		var files, created string
		if err := scan(&message.ID, &message.SessionID, &message.Content, &files, &created); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(files), &message.FileIDs); err != nil {
			return fmt.Errorf("queued message %d files: %w", message.ID, err)
		}
		var err error
		if message.CreatedAt, err = parseTimestamp(created, "queued_message.created_at"); err != nil {
			return err
		}
		messages = append(messages, message)
		return nil
	})
	return messages, err
}
