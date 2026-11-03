package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// goalsTable is shared with the migration that adds it. A conversation has
// at most one active goal.
const goalsTable = `CREATE TABLE IF NOT EXISTS goals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    source_run_id TEXT NOT NULL DEFAULT '',
    objective TEXT NOT NULL,
    max_runs INTEGER NOT NULL,
    runs INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL,
    result TEXT NOT NULL DEFAULT '',
    announced INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_goals_active ON goals(session_id) WHERE state = 'active';`

const goalColumns = `id,session_id,source_run_id,objective,max_runs,runs,state,result,announced,created_at,updated_at`

// StartGoal implements core.GoalStore.
func (s *Store) StartGoal(ctx context.Context, g core.Goal) (core.Goal, error) {
	switch {
	case strings.TrimSpace(g.SessionID) == "" || strings.TrimSpace(g.Objective) == "":
		return core.Goal{}, errors.New("goal requires session_id and objective")
	case g.MaxRuns < 1 || g.CreatedAt.IsZero():
		return core.Goal{}, errors.New("goal requires a positive max_runs and created_at")
	}
	g.Runs, g.State, g.Result, g.Announced, g.UpdatedAt = 0, core.GoalActive, "", false, g.CreatedAt
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		var exists, active int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE session_id = ?`, g.SessionID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return fmt.Errorf("%w: %s", core.ErrSessionNotFound, g.SessionID)
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM goals WHERE session_id = ? AND state = ?`, g.SessionID, core.GoalActive).Scan(&active); err != nil {
			return err
		}
		if active > 0 {
			return core.ErrGoalActive
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO goals(session_id,source_run_id,objective,max_runs,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
			g.SessionID, g.SourceRunID, g.Objective, g.MaxRuns, g.State, formatTimestamp(g.CreatedAt), formatTimestamp(g.UpdatedAt))
		if err != nil {
			return err
		}
		g.ID, err = result.LastInsertId()
		return err
	})
	if err != nil {
		return core.Goal{}, fmt.Errorf("start goal: %w", err)
	}
	return g, nil
}

// ActiveGoal implements core.GoalStore.
func (s *Store) ActiveGoal(ctx context.Context, sessionID string) (core.Goal, error) {
	g, err := scanGoal(s.db.QueryRowContext(ctx, `SELECT `+goalColumns+` FROM goals WHERE session_id = ? AND state = ?`, sessionID, core.GoalActive))
	if errors.Is(err, sql.ErrNoRows) {
		return core.Goal{}, core.ErrGoalNotFound
	}
	return g, err
}

// ActiveGoals implements core.GoalStore.
func (s *Store) ActiveGoals(ctx context.Context) ([]core.Goal, error) {
	return s.queryGoals(ctx, `SELECT `+goalColumns+` FROM goals WHERE state = ? ORDER BY id`, core.GoalActive)
}

// UnannouncedGoals implements core.GoalStore.
func (s *Store) UnannouncedGoals(ctx context.Context) ([]core.Goal, error) {
	return s.queryGoals(ctx, `SELECT `+goalColumns+` FROM goals WHERE state IN (?, ?) AND announced = 0 ORDER BY id`, core.GoalDone, core.GoalBlocked)
}

// EndGoal implements core.GoalStore.
func (s *Store) EndGoal(ctx context.Context, id int64, state, result string, now time.Time) (core.Goal, error) {
	if state != core.GoalDone && state != core.GoalBlocked && state != core.GoalStopped {
		return core.Goal{}, fmt.Errorf("goal can end done, blocked or stopped, got %q", state)
	}
	if strings.TrimSpace(result) == "" {
		return core.Goal{}, errors.New("ending a goal requires a result")
	}
	var g core.Goal
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		changed, err := tx.ExecContext(ctx, `UPDATE goals SET state = ?, result = ?, announced = ?, updated_at = ? WHERE id = ? AND state = ?`,
			state, result, state == core.GoalStopped, formatTimestamp(now), id, core.GoalActive)
		if err := goalChanged(changed, err, core.ErrGoalNotFound); err != nil {
			return err
		}
		g, err = scanGoal(tx.QueryRowContext(ctx, `SELECT `+goalColumns+` FROM goals WHERE id = ?`, id))
		return err
	})
	if err != nil {
		return core.Goal{}, fmt.Errorf("end goal #%d: %w", id, err)
	}
	return g, nil
}

// CountGoalRun implements core.GoalStore.
func (s *Store) CountGoalRun(ctx context.Context, id int64, runs int, now time.Time) error {
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		changed, err := tx.ExecContext(ctx, `UPDATE goals SET runs = runs + 1, updated_at = ? WHERE id = ? AND state = ? AND runs = ? AND runs < max_runs`,
			formatTimestamp(now), id, core.GoalActive, runs)
		return goalChanged(changed, err, core.ErrGoalChanged)
	})
}

// MarkGoalAnnounced implements core.GoalStore.
func (s *Store) MarkGoalAnnounced(ctx context.Context, id int64, now time.Time) error {
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		changed, err := tx.ExecContext(ctx, `UPDATE goals SET announced = 1, updated_at = ? WHERE id = ? AND state IN (?, ?) AND announced = 0`,
			formatTimestamp(now), id, core.GoalDone, core.GoalBlocked)
		return goalChanged(changed, err, core.ErrGoalChanged)
	})
}

func goalChanged(result sql.Result, err, unchanged error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return unchanged
	}
	return nil
}

func (s *Store) queryGoals(ctx context.Context, query string, args ...any) ([]core.Goal, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list goals: %w", err)
	}
	out := []core.Goal{}
	err = scanRows(rows, func(scan func(...any) error) error {
		g, err := scanGoal(scannerFunc(scan))
		if err == nil {
			out = append(out, g)
		}
		return err
	})
	return out, err
}

func scanGoal(row interface{ Scan(...any) error }) (core.Goal, error) {
	var g core.Goal
	var created, updated string
	if err := row.Scan(&g.ID, &g.SessionID, &g.SourceRunID, &g.Objective, &g.MaxRuns, &g.Runs, &g.State, &g.Result, &g.Announced, &created, &updated); err != nil {
		return g, err
	}
	var err error
	if g.CreatedAt, err = parseTimestamp(created, "goals.created_at"); err != nil {
		return g, err
	}
	g.UpdatedAt, err = parseTimestamp(updated, "goals.updated_at")
	return g, err
}
