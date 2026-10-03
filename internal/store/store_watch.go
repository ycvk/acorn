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

const watchColumns = `id, name, kind, target, selector, mode, interval_seconds, status, session_id,
	next_check_at, last_checked_at, last_error, failures, snapshot, created_at, updated_at`

func (s *Store) AddWatch(ctx context.Context, w core.Watch) (core.Watch, error) {
	if err := validateWatch(w); err != nil {
		return core.Watch{}, err
	}
	if w.CreatedAt.IsZero() {
		return core.Watch{}, errors.New("add watch: created_at is required")
	}
	w.UpdatedAt = w.CreatedAt
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO watches (name, kind, target, selector, mode, interval_seconds, status, session_id,
		   next_check_at, last_checked_at, last_error, failures, snapshot, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.Name, string(w.Kind), w.Target, w.Selector, string(w.Mode), int64(w.Interval/time.Second), string(w.Status), w.SessionID,
		formatTimestamp(w.NextCheckAt), formatZeroableTimestamp(w.LastCheckedAt), w.LastError, w.Failures, w.Snapshot,
		formatTimestamp(w.CreatedAt), formatTimestamp(w.UpdatedAt))
	if err != nil {
		return core.Watch{}, fmt.Errorf("add watch: %w", err)
	}
	if w.ID, err = result.LastInsertId(); err != nil {
		return core.Watch{}, fmt.Errorf("add watch id: %w", err)
	}
	return w, nil
}

func (s *Store) LoadWatch(ctx context.Context, id int64) (*core.Watch, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+watchColumns+` FROM watches WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("load watch %d: %w", id, err)
	}
	watches, err := scanWatches(rows)
	if err != nil {
		return nil, fmt.Errorf("load watch %d: %w", id, err)
	}
	if len(watches) == 0 {
		return nil, fmt.Errorf("%w: %d", core.ErrWatchNotFound, id)
	}
	return &watches[0], nil
}

func (s *Store) ListWatches(ctx context.Context) ([]core.Watch, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+watchColumns+` FROM watches ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list watches: %w", err)
	}
	return scanWatches(rows)
}

// UpdateWatch writes every mutable field of the watch.
func (s *Store) UpdateWatch(ctx context.Context, w core.Watch) error {
	if err := validateWatch(w); err != nil {
		return err
	}
	if w.UpdatedAt.IsZero() {
		return errors.New("update watch: updated_at is required")
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE watches SET name = ?, selector = ?, mode = ?, interval_seconds = ?, status = ?,
		   next_check_at = ?, last_checked_at = ?, last_error = ?, failures = ?, snapshot = ?, updated_at = ?
		 WHERE id = ?`,
		w.Name, w.Selector, string(w.Mode), int64(w.Interval/time.Second), string(w.Status),
		formatTimestamp(w.NextCheckAt), formatZeroableTimestamp(w.LastCheckedAt), w.LastError, w.Failures, w.Snapshot,
		formatTimestamp(w.UpdatedAt), w.ID)
	if err != nil {
		return fmt.Errorf("update watch %d: %w", w.ID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update watch %d rows affected: %w", w.ID, err)
	}
	if affected != 1 {
		return fmt.Errorf("%w: %d", core.ErrWatchNotFound, w.ID)
	}
	return nil
}

func (s *Store) ListDueWatches(ctx context.Context, now time.Time, limit int) ([]core.Watch, error) {
	if limit <= 0 {
		return nil, errors.New("list due watches: limit must be positive")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+watchColumns+` FROM watches WHERE status <> ? AND next_check_at <= ?
		 ORDER BY next_check_at, id LIMIT ?`,
		string(core.WatchPaused), formatTimestamp(now), limit)
	if err != nil {
		return nil, fmt.Errorf("list due watches: %w", err)
	}
	return scanWatches(rows)
}

func (s *Store) ClaimDueWatch(ctx context.Context, id int64, now time.Time, lease time.Duration) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE watches SET next_check_at = ? WHERE id = ? AND status <> ? AND next_check_at <= ?`,
		formatTimestamp(now.Add(lease)), id, string(core.WatchPaused), formatTimestamp(now))
	if err != nil {
		return fmt.Errorf("claim watch %d: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("claim watch %d rows affected: %w", id, err)
	}
	if affected != 1 {
		return fmt.Errorf("%w: %d", core.ErrWatchNotDue, id)
	}
	return nil
}

func (s *Store) AddWatchItems(ctx context.Context, items []core.WatchItem) (added []core.WatchItem, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("add watch items: %w", err)
	}
	defer rollbackOnErr(tx, &err, "add watch items")
	for _, item := range items {
		if item.WatchID == 0 || item.Key == "" || item.Status == "" || item.SeenAt.IsZero() {
			return nil, fmt.Errorf("add watch items: watch_id, key, status and seen_at are required (key %q)", item.Key)
		}
		result, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO watch_items (watch_id, item_key, title, url, summary, published_at, status, run_id, seen_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			item.WatchID, item.Key, item.Title, item.URL, item.Summary, formatZeroableTimestamp(item.PublishedAt),
			string(item.Status), item.RunID, formatTimestamp(item.SeenAt))
		if err != nil {
			return nil, fmt.Errorf("add watch item %q: %w", item.Key, err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("add watch item %q rows affected: %w", item.Key, err)
		}
		if affected == 0 {
			continue
		}
		if item.ID, err = result.LastInsertId(); err != nil {
			return nil, fmt.Errorf("add watch item %q id: %w", item.Key, err)
		}
		added = append(added, item)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("add watch items: %w", err)
	}
	return added, nil
}

func (s *Store) ListWatchItems(ctx context.Context, status core.WatchItemStatus, limit int) ([]core.WatchItem, error) {
	if limit <= 0 {
		return nil, errors.New("list watch items: limit must be positive")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, watch_id, item_key, title, url, summary, published_at, status, run_id, seen_at
		 FROM watch_items WHERE status = ? ORDER BY watch_id, id LIMIT ?`, string(status), limit)
	if err != nil {
		return nil, fmt.Errorf("list watch items: %w", err)
	}
	var items []core.WatchItem
	if err := scanRows(rows, func(scan func(...any) error) error {
		var (
			item                        core.WatchItem
			itemStatus, published, seen string
		)
		if err := scan(&item.ID, &item.WatchID, &item.Key, &item.Title, &item.URL, &item.Summary, &published, &itemStatus, &item.RunID, &seen); err != nil {
			return err
		}
		item.Status = core.WatchItemStatus(itemStatus)
		var err error
		if item.PublishedAt, err = parseOptionalTime(published, "watch_items.published_at"); err != nil {
			return err
		}
		item.SeenAt, err = parseTimestamp(fixedTimestampLayout, seen, "watch_items.seen_at")
		items = append(items, item)
		return err
	}); err != nil {
		return nil, fmt.Errorf("list watch items: %w", err)
	}
	return items, nil
}

func (s *Store) MarkWatchItems(ctx context.Context, ids []int64, status core.WatchItemStatus, runID string) (err error) {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mark watch items: %w", err)
	}
	defer rollbackOnErr(tx, &err, "mark watch items")
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE watch_items SET status = ?, run_id = ? WHERE id = ?`, string(status), runID, id); err != nil {
			return fmt.Errorf("mark watch item %d: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("mark watch items: %w", err)
	}
	return nil
}

func (s *Store) ClaimBriefing(ctx context.Context, day, threadID string, at time.Time) error {
	if day == "" || threadID == "" || at.IsZero() {
		return errors.New("claim briefing: day, thread and time are required")
	}
	result, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO briefings (day, thread_id, created_at) VALUES (?, ?, ?)`, day, threadID, formatTimestamp(at))
	if err != nil {
		return fmt.Errorf("claim briefing %s: %w", day, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("claim briefing %s rows affected: %w", day, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: %s", core.ErrBriefingTaken, day)
	}
	return nil
}

func (s *Store) SetBriefingRun(ctx context.Context, day, runID string) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE briefings SET run_id = ? WHERE day = ?`, runID, day); err != nil {
		return fmt.Errorf("set briefing run %s: %w", day, err)
	}
	return nil
}

func (s *Store) LatestBriefingThread(ctx context.Context) (string, error) {
	var thread string
	err := s.db.QueryRowContext(ctx, `SELECT thread_id FROM briefings ORDER BY day DESC LIMIT 1`).Scan(&thread)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("latest briefing thread: %w", err)
	}
	return thread, nil
}

func validateWatch(w core.Watch) error {
	switch {
	case strings.TrimSpace(w.Name) == "":
		return errors.New("watch: name is required")
	case w.Kind == "" || w.Target == "" || w.Mode == "" || w.Status == "":
		return errors.New("watch: kind, target, mode and status are required")
	case w.Interval < time.Second:
		return errors.New("watch: interval is required")
	case w.NextCheckAt.IsZero():
		return errors.New("watch: next_check_at is required")
	}
	return nil
}

func scanWatches(rows *sql.Rows) ([]core.Watch, error) {
	var watches []core.Watch
	err := scanRows(rows, func(scan func(...any) error) error {
		var (
			w                                   core.Watch
			kind, mode, status                  string
			intervalSeconds                     int64
			next, lastChecked, created, updated string
		)
		if err := scan(&w.ID, &w.Name, &kind, &w.Target, &w.Selector, &mode, &intervalSeconds, &status, &w.SessionID,
			&next, &lastChecked, &w.LastError, &w.Failures, &w.Snapshot, &created, &updated); err != nil {
			return err
		}
		w.Kind, w.Mode, w.Status = core.WatchKind(kind), core.WatchMode(mode), core.WatchStatus(status)
		w.Interval = time.Duration(intervalSeconds) * time.Second
		var err error
		if w.NextCheckAt, err = parseTimestamp(fixedTimestampLayout, next, "watches.next_check_at"); err != nil {
			return err
		}
		if w.LastCheckedAt, err = parseOptionalTime(lastChecked, "watches.last_checked_at"); err != nil {
			return err
		}
		if w.CreatedAt, err = parseTimestamp(fixedTimestampLayout, created, "watches.created_at"); err != nil {
			return err
		}
		w.UpdatedAt, err = parseTimestamp(fixedTimestampLayout, updated, "watches.updated_at")
		watches = append(watches, w)
		return err
	})
	return watches, err
}
