package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// addSkillTriggers (13 to 14) adds the schedules and events of package
// triggers, registering events as memory sources.
func addSkillTriggers(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, skillTriggerTables+memoryTriggerEventTrigger)
	return err
}

func (s *Store) ListTriggers(ctx context.Context) ([]core.TriggerState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT skill, name, schedule, status, next_at, failures, last_error, last_run_at FROM skill_triggers ORDER BY skill, name`)
	if err != nil {
		return nil, fmt.Errorf("list triggers: %w", err)
	}
	var states []core.TriggerState
	if err := scanRows(rows, func(scan func(...any) error) error {
		var (
			state             core.TriggerState
			status, next, ran string
		)
		if err := scan(&state.Skill, &state.Name, &state.Schedule, &status, &next, &state.Failures, &state.LastError, &ran); err != nil {
			return err
		}
		state.Status = core.TriggerStatus(status)
		var err error
		if state.NextAt, err = parseTimestamp(fixedTimestampLayout, next, "skill_triggers.next_at"); err != nil {
			return err
		}
		state.LastRunAt, err = parseOptionalTime(ran, "skill_triggers.last_run_at")
		states = append(states, state)
		return err
	}); err != nil {
		return nil, fmt.Errorf("list triggers: %w", err)
	}
	return states, nil
}

func (s *Store) PutTrigger(ctx context.Context, state core.TriggerState) error {
	if state.Skill == "" || state.Name == "" || state.Schedule == "" || state.NextAt.IsZero() {
		return errors.New("put trigger: skill, name, schedule and next_at are required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO skill_triggers (skill, name, schedule, status, next_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(skill, name) DO UPDATE SET schedule = excluded.schedule, status = excluded.status, next_at = excluded.next_at, failures = 0, last_error = ''`,
		state.Skill, state.Name, state.Schedule, string(core.TriggerActive), formatTimestamp(state.NextAt))
	if err != nil {
		return fmt.Errorf("put trigger %s/%s: %w", state.Skill, state.Name, err)
	}
	return nil
}

func (s *Store) RemoveTrigger(ctx context.Context, skill, name string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM skill_triggers WHERE skill = ? AND name = ?`, skill, name); err != nil {
		return fmt.Errorf("remove trigger %s/%s: %w", skill, name, err)
	}
	return nil
}

func (s *Store) ClaimTrigger(ctx context.Context, skill, name string, due, next time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE skill_triggers SET next_at = ? WHERE skill = ? AND name = ? AND next_at = ?`,
		formatTimestamp(next), skill, name, formatTimestamp(due))
	if err != nil {
		return fmt.Errorf("claim trigger %s/%s: %w", skill, name, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("claim trigger %s/%s rows affected: %w", skill, name, err)
	}
	if affected != 1 {
		return fmt.Errorf("%w: %s/%s", core.ErrTriggerNotDue, skill, name)
	}
	return nil
}

func (s *Store) RecordTriggerRun(ctx context.Context, skill, name string, run core.TriggerRun) (added []core.TriggerEvent, err error) {
	if run.At.IsZero() || run.NextAt.IsZero() || run.Status == "" {
		return nil, fmt.Errorf("record run of trigger %s/%s: at, next_at and status are required", skill, name)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("record run of trigger %s/%s: %w", skill, name, err)
	}
	defer rollbackOnErr(tx, &err, "record trigger run")
	// A trigger removed during its run still keeps the events it printed.
	if _, err := tx.ExecContext(ctx, `UPDATE skill_triggers SET status = ?, next_at = ?, failures = ?, last_error = ?, last_run_at = ? WHERE skill = ? AND name = ?`,
		string(run.Status), formatTimestamp(run.NextAt), run.Failures, run.LastError, formatTimestamp(run.At), skill, name); err != nil {
		return nil, fmt.Errorf("record run of trigger %s/%s: %w", skill, name, err)
	}
	for _, event := range run.Events {
		if event.Key == "" {
			return nil, fmt.Errorf("record run of trigger %s/%s: an event needs a key", skill, name)
		}
		result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO skill_trigger_events (skill, trigger_name, event_key, text, status, seen_at) VALUES (?, ?, ?, ?, ?, ?)`,
			skill, name, event.Key, event.Text, string(core.TriggerEventPending), formatTimestamp(run.At))
		if err != nil {
			return nil, fmt.Errorf("add event %q of trigger %s/%s: %w", event.Key, skill, name, err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if affected == 0 {
			continue
		}
		event.Skill, event.Trigger, event.Status, event.SeenAt = skill, name, core.TriggerEventPending, run.At
		if event.ID, err = result.LastInsertId(); err != nil {
			return nil, err
		}
		added = append(added, event)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("record run of trigger %s/%s: %w", skill, name, err)
	}
	return added, nil
}

func (s *Store) ListTriggerEvents(ctx context.Context, status core.TriggerEventStatus, limit int) ([]core.TriggerEvent, error) {
	if limit <= 0 {
		return nil, errors.New("list trigger events: limit must be positive")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, skill, trigger_name, event_key, text, status, run_id, seen_at FROM skill_trigger_events WHERE status = ? ORDER BY skill, trigger_name, id LIMIT ?`, string(status), limit)
	if err != nil {
		return nil, fmt.Errorf("list trigger events: %w", err)
	}
	var events []core.TriggerEvent
	if err := scanRows(rows, func(scan func(...any) error) error {
		var (
			event           core.TriggerEvent
			eventStatus, at string
		)
		if err := scan(&event.ID, &event.Skill, &event.Trigger, &event.Key, &event.Text, &eventStatus, &event.RunID, &at); err != nil {
			return err
		}
		event.Status = core.TriggerEventStatus(eventStatus)
		var err error
		event.SeenAt, err = parseTimestamp(fixedTimestampLayout, at, "skill_trigger_events.seen_at")
		events = append(events, event)
		return err
	}); err != nil {
		return nil, fmt.Errorf("list trigger events: %w", err)
	}
	return events, nil
}

func (s *Store) MarkTriggerEvents(ctx context.Context, ids []int64, status core.TriggerEventStatus, runID string) (err error) {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mark trigger events: %w", err)
	}
	defer rollbackOnErr(tx, &err, "mark trigger events")
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE skill_trigger_events SET status = ?, run_id = ? WHERE id = ?`, string(status), runID, id); err != nil {
			return fmt.Errorf("mark trigger event %d: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("mark trigger events: %w", err)
	}
	return nil
}

func (s *Store) SkillThread(ctx context.Context, skill string) (string, error) {
	var thread string
	err := s.db.QueryRowContext(ctx, `SELECT thread_id FROM skill_threads WHERE skill = ?`, skill).Scan(&thread)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read thread of skill %s: %w", skill, err)
	}
	return thread, nil
}

func (s *Store) SetSkillThread(ctx context.Context, skill, threadID string) error {
	if skill == "" || threadID == "" {
		return errors.New("set skill thread: skill and thread are required")
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO skill_threads (skill, thread_id) VALUES (?, ?) ON CONFLICT(skill) DO UPDATE SET thread_id = excluded.thread_id`, skill, threadID); err != nil {
		return fmt.Errorf("set thread of skill %s: %w", skill, err)
	}
	return nil
}
