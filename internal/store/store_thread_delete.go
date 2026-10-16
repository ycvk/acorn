package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ycvk/acorn/internal/core"
)

// ThreadMemorySourceIDs lists the memory sources a thread's conversation
// produced: its messages and tool results.
func (s *Store) ThreadMemorySourceIDs(ctx context.Context, sessionID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM memory_sources WHERE session_id = ? AND kind IN ('message','event') ORDER BY id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list thread memory sources: %w", err)
	}
	return memoryStringRows(rows)
}

// DeleteThread removes a thread with what its runs left behind: messages,
// files, runs, events, pending actions, checkpoints, notifications, artifacts
// and the context snapshots no other run used. The thread's memory sources stay,
// so the exclusions that forget them keep holding.
func (s *Store) DeleteThread(ctx context.Context, sessionID string) error {
	var paths, files []string
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		var exists, running int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE session_id = ?`, sessionID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return fmt.Errorf("%w: %s", core.ErrSessionNotFound, sessionID)
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs WHERE session_id = ? AND status = ?`, sessionID, string(core.RunStatusRunning)).Scan(&running); err != nil {
			return err
		}
		if running > 0 {
			return fmt.Errorf("%w: %s", core.ErrThreadBusy, sessionID)
		}
		var err error
		if paths, err = purgeThread(ctx, tx, sessionID); err != nil {
			return err
		}
		files, err = purgeThreadFiles(ctx, tx, sessionID)
		return err
	})
	if err != nil {
		return fmt.Errorf("delete thread %s: %w", sessionID, err)
	}
	for _, relative := range paths {
		path, err := s.artifactSvc.pathFor(relative)
		if err != nil {
			return fmt.Errorf("delete thread %s: %w", sessionID, err)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("delete thread %s artifact: %w", sessionID, err)
		}
	}
	for _, id := range files {
		if err := os.Remove(filepath.Join(s.filesDir, id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("delete thread %s file: %w", sessionID, err)
		}
	}
	return nil
}

// purgeThread deletes a thread's rows and returns the relative paths of its
// artifact files.
func purgeThread(ctx context.Context, tx *sql.Tx, sessionID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT run_id FROM runs WHERE session_id = ?`, sessionID)
	if err != nil {
		return nil, err
	}
	runIDs, err := memoryStringRows(rows)
	if err != nil {
		return nil, err
	}
	runs, err := json.Marshal(append([]string{}, runIDs...))
	if err != nil {
		return nil, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT relative_path FROM artifacts WHERE session_id = ? OR run_id IN (SELECT value FROM json_each(?))`, sessionID, string(runs))
	if err != nil {
		return nil, err
	}
	paths, err := memoryStringRows(rows)
	if err != nil {
		return nil, err
	}
	for _, step := range []struct {
		query string
		args  []any
	}{
		// A snapshot leaves with the thread when no other run used it.
		{`DELETE FROM context_snapshots WHERE hash IN (
			SELECT hash FROM context_snapshot_refs WHERE run_id IN (SELECT value FROM json_each(?1))
			UNION SELECT json_extract(payload_json, '$.hash') FROM events WHERE kind = 'presence.snapshot' AND run_id IN (SELECT value FROM json_each(?1)))
		  AND hash NOT IN (SELECT hash FROM context_snapshot_refs WHERE run_id NOT IN (SELECT value FROM json_each(?1)))
		  AND hash NOT IN (SELECT json_extract(payload_json, '$.hash') FROM events WHERE kind = 'presence.snapshot' AND run_id NOT IN (SELECT value FROM json_each(?1)) AND json_extract(payload_json, '$.hash') IS NOT NULL)`, []any{string(runs)}},
		{`DELETE FROM context_snapshot_refs WHERE run_id IN (SELECT value FROM json_each(?))`, []any{string(runs)}},
		{`DELETE FROM events WHERE run_id IN (SELECT value FROM json_each(?))`, []any{string(runs)}},
		{`DELETE FROM pending_actions WHERE run_id IN (SELECT value FROM json_each(?))`, []any{string(runs)}},
		{`DELETE FROM agent_checkpoints WHERE checkpoint_id IN (SELECT value FROM json_each(?))`, []any{string(runs)}},
		{`DELETE FROM artifacts WHERE session_id = ? OR run_id IN (SELECT value FROM json_each(?))`, []any{sessionID, string(runs)}},
		{`DELETE FROM notifications WHERE thread_id = ? OR run_id IN (SELECT value FROM json_each(?))`, []any{sessionID, string(runs)}},
		{`DELETE FROM thread_summary_sources WHERE session_id = ?`, []any{sessionID}},
		{`DELETE FROM thread_summaries WHERE session_id = ?`, []any{sessionID}},
		{`DELETE FROM runs WHERE session_id = ?`, []any{sessionID}},
		{`DELETE FROM session_messages WHERE session_id = ?`, []any{sessionID}},
		{`DELETE FROM sessions WHERE session_id = ?`, []any{sessionID}},
	} {
		if _, err := tx.ExecContext(ctx, step.query, step.args...); err != nil {
			return nil, err
		}
	}
	return paths, nil
}
