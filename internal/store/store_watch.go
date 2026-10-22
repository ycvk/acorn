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

// watchesTable is shared with the migrations that rebuild the watches table.
// A news watch names its section; other watches have none.
const watchesTable = `CREATE TABLE IF NOT EXISTS watches (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    target TEXT NOT NULL,
    selector TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL,
    section TEXT REFERENCES news_sections(name) ON UPDATE CASCADE,
    issue INTEGER NOT NULL DEFAULT 0,
    trial INTEGER NOT NULL DEFAULT 0,
    interval_seconds INTEGER NOT NULL,
    status TEXT NOT NULL,
    session_id TEXT NOT NULL DEFAULT '',
    next_check_at TEXT NOT NULL,
    last_checked_at TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    failures INTEGER NOT NULL DEFAULT 0,
    snapshot TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);`

const newsSectionsTable = `CREATE TABLE IF NOT EXISTS news_sections (
    name TEXT PRIMARY KEY,
    position INTEGER NOT NULL,
    preference TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);`

// addNewsSections (21 to 22) adds news sections, the section and issue flag
// of each watch, and stops registering items of news watches as memory
// sources. The watches table is rebuilt from watchesTable so its stored
// definition matches a new database.
func addNewsSections(ctx context.Context, tx *sql.Tx) error {
	columns := "id, name, kind, target, selector, mode, interval_seconds, status, session_id, next_check_at, last_checked_at, last_error, failures, snapshot, created_at, updated_at"
	for _, statement := range []string{
		newsSectionsTable,
		`CREATE TEMP TABLE watches_v21 AS SELECT ` + columns + ` FROM watches`,
		`DROP TABLE watches`,
		watchesTable,
		`INSERT INTO watches(` + columns + `) SELECT ` + columns + ` FROM watches_v21`,
		`DROP TABLE watches_v21`,
		`CREATE INDEX IF NOT EXISTS idx_watches_due ON watches(status, next_check_at)`,
		`DROP TRIGGER memory_watch_ai`,
		memoryWatchTrigger,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// addNewsTrials (23 to 24) adds the trial flag of news watches. The watches
// table is rebuilt from watchesTable so its stored definition matches a new
// database.
func addNewsTrials(ctx context.Context, tx *sql.Tx) error {
	columns := "id, name, kind, target, selector, mode, section, issue, interval_seconds, status, session_id, next_check_at, last_checked_at, last_error, failures, snapshot, created_at, updated_at"
	for _, statement := range []string{
		`CREATE TEMP TABLE watches_v23 AS SELECT ` + columns + ` FROM watches`,
		`DROP TABLE watches`,
		watchesTable,
		`INSERT INTO watches(` + columns + `) SELECT ` + columns + ` FROM watches_v23`,
		`DROP TABLE watches_v23`,
		`CREATE INDEX IF NOT EXISTS idx_watches_due ON watches(status, next_check_at)`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

const watchColumns = `id, name, kind, target, selector, mode, section, issue, trial, interval_seconds, status, session_id,
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
		`INSERT INTO watches (name, kind, target, selector, mode, section, issue, trial, interval_seconds, status, session_id,
		   next_check_at, last_checked_at, last_error, failures, snapshot, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.Name, string(w.Kind), w.Target, w.Selector, string(w.Mode), nullableSection(w.Section), w.Issue, w.Trial, int64(w.Interval/time.Second), string(w.Status), w.SessionID,
		formatTimestamp(w.NextCheckAt), formatZeroableTimestamp(w.LastCheckedAt), w.LastError, w.Failures, w.Snapshot,
		formatTimestamp(w.CreatedAt), formatTimestamp(w.UpdatedAt))
	if err != nil {
		return core.Watch{}, fmt.Errorf("add watch: %w", sectionError(err, w.Section))
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
		`UPDATE watches SET name = ?, selector = ?, mode = ?, section = ?, issue = ?, trial = ?, interval_seconds = ?, status = ?,
		   next_check_at = ?, last_checked_at = ?, last_error = ?, failures = ?, snapshot = ?, updated_at = ?
		 WHERE id = ?`,
		w.Name, w.Selector, string(w.Mode), nullableSection(w.Section), w.Issue, w.Trial, int64(w.Interval/time.Second), string(w.Status),
		formatTimestamp(w.NextCheckAt), formatZeroableTimestamp(w.LastCheckedAt), w.LastError, w.Failures, w.Snapshot,
		formatTimestamp(w.UpdatedAt), w.ID)
	if err != nil {
		return fmt.Errorf("update watch %d: %w", w.ID, sectionError(err, w.Section))
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
		`SELECT `+watchColumns+` FROM watches WHERE status <> ? AND kind <> ? AND next_check_at <= ?
		 ORDER BY next_check_at, id LIMIT ?`,
		string(core.WatchPaused), string(core.WatchTrigger), formatTimestamp(now), limit)
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

func (s *Store) RecordWatchCheck(ctx context.Context, from core.Watch, check core.WatchCheck) (added []core.WatchItem, err error) {
	if check.At.IsZero() || check.NextCheckAt.IsZero() || check.Status == "" {
		return nil, fmt.Errorf("record check of watch %d: at, next_check_at and status are required", from.ID)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("record check of watch %d: %w", from.ID, err)
	}
	defer rollbackOnErr(tx, &err, "record watch check")
	result, err := tx.ExecContext(ctx,
		`UPDATE watches SET status = ?, next_check_at = ?, last_checked_at = ?, last_error = ?, failures = ?, snapshot = ?, updated_at = ?
		 WHERE id = ? AND status = ? AND failures = ? AND last_checked_at = ?`,
		string(check.Status), formatTimestamp(check.NextCheckAt), formatTimestamp(check.At), check.LastError, check.Failures, check.Snapshot,
		formatTimestamp(check.At), from.ID, string(from.Status), from.Failures, formatZeroableTimestamp(from.LastCheckedAt))
	if err != nil {
		return nil, fmt.Errorf("record check of watch %d: %w", from.ID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("record check of watch %d rows affected: %w", from.ID, err)
	}
	if affected != 1 {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM watches WHERE id = ?)`, from.ID).Scan(&exists); err != nil {
			return nil, fmt.Errorf("record check of watch %d: %w", from.ID, err)
		}
		if !exists {
			return nil, fmt.Errorf("%w: %d", core.ErrWatchNotFound, from.ID)
		}
		return nil, fmt.Errorf("%w: %d", core.ErrWatchChanged, from.ID)
	}
	for _, item := range check.Items {
		if item.WatchID != from.ID || item.Key == "" || item.Status == "" || item.SeenAt.IsZero() {
			return nil, fmt.Errorf("record check of watch %d: items need its watch_id, a key, status and seen_at (key %q)", from.ID, item.Key)
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
		return nil, fmt.Errorf("record check of watch %d: %w", from.ID, err)
	}
	return added, nil
}

func (s *Store) ListWatchItems(ctx context.Context, status core.WatchItemStatus, limit int) ([]core.WatchItem, error) {
	if limit <= 0 {
		return nil, errors.New("list watch items: limit must be positive")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+watchItemColumns+` FROM watch_items WHERE status = ? ORDER BY watch_id, id LIMIT ?`, string(status), limit)
	if err != nil {
		return nil, fmt.Errorf("list watch items: %w", err)
	}
	items, err := scanWatchItems(rows)
	if err != nil {
		return nil, fmt.Errorf("list watch items: %w", err)
	}
	return items, nil
}

func scanWatchItems(rows *sql.Rows) ([]core.WatchItem, error) {
	var items []core.WatchItem
	err := scanRows(rows, func(scan func(...any) error) error {
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
	})
	return items, err
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
	case w.Mode == core.WatchModeNews && strings.TrimSpace(w.Section) == "":
		return errors.New("watch: a news watch needs a section")
	case w.Mode != core.WatchModeNews && (w.Section != "" || w.Issue || w.Trial):
		return fmt.Errorf("watch: only news watches have a section, issues or a trial, not mode %q", w.Mode)
	case w.Kind == core.WatchTrigger && w.Mode != core.WatchModeNews:
		return fmt.Errorf("watch: a trigger source is a news watch, not mode %q", w.Mode)
	}
	return nil
}

func nullableSection(section string) any {
	if section == "" {
		return nil
	}
	return section
}

// sectionError names a section that does not exist, which the foreign key
// reports only as a constraint failure.
func sectionError(err error, section string) error {
	if section != "" && strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
		return fmt.Errorf("%w: %q", core.ErrNewsSectionNotFound, section)
	}
	return err
}

func scanWatches(rows *sql.Rows) ([]core.Watch, error) {
	var watches []core.Watch
	err := scanRows(rows, func(scan func(...any) error) error {
		var (
			w                                   core.Watch
			kind, mode, status                  string
			section                             sql.NullString
			intervalSeconds                     int64
			next, lastChecked, created, updated string
		)
		if err := scan(&w.ID, &w.Name, &kind, &w.Target, &w.Selector, &mode, &section, &w.Issue, &w.Trial, &intervalSeconds, &status, &w.SessionID,
			&next, &lastChecked, &w.LastError, &w.Failures, &w.Snapshot, &created, &updated); err != nil {
			return err
		}
		w.Kind, w.Mode, w.Status, w.Section = core.WatchKind(kind), core.WatchMode(mode), core.WatchStatus(status), section.String
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
