package store

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/ycvk/acorn/internal/core"
)

// PreflightMemoryMigration opens the existing database in SQLite read-only
// mode. It neither creates a store nor runs schema migrations.
func PreflightMemoryMigration(ctx context.Context, dir string) (out core.MemoryMigrationReport, resultErr error) {
	path, err := filepath.Abs(filepath.Join(dir, "acorn.db"))
	if err != nil {
		return out, err
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, db.Close()) }()
	rows, err := db.QueryContext(ctx, "PRAGMA quick_check")
	if err != nil {
		return out, err
	}
	var checks []string
	if err := scanRows(rows, func(scan func(...any) error) error {
		var check string
		if err := scan(&check); err != nil {
			return err
		}
		checks = append(checks, check)
		return nil
	}); err != nil {
		return out, err
	}
	out.Integrity = strings.Join(checks, "; ")
	out.Ready = out.Integrity == "ok"
	counts := []struct {
		query  string
		target *int
	}{
		{`SELECT COUNT(*) FROM runs WHERE status='running'`, &out.ActiveRuns},
		{`SELECT COUNT(*) FROM runs WHERE status='interrupted'`, &out.InterruptedRuns},
		{`SELECT COUNT(*) FROM pending_actions WHERE status='pending'`, &out.PendingActions},
		{`SELECT COUNT(*) FROM session_messages`, &out.Messages},
		{`SELECT COUNT(*) FROM events WHERE kind IN ('tool.call.succeeded','tool.call.failed')`, &out.ToolOutcomes},
	}
	for _, count := range counts {
		if err := db.QueryRowContext(ctx, count.query).Scan(count.target); err != nil {
			return out, err
		}
	}
	var legacy bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='memory_items')`).Scan(&legacy); err != nil {
		return out, err
	}
	if legacy {
		out.Schema = "working-memory"
		out.LegacyCommitments = map[string]int{}
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_items`).Scan(&out.LegacyEntries); err != nil {
			return out, err
		}
		rows, err := db.QueryContext(ctx, `SELECT status,COUNT(*) FROM memory_items WHERE kind='commitment' GROUP BY status`)
		if err != nil {
			return out, err
		}
		if err := scanRows(rows, func(scan func(...any) error) error {
			var state string
			var n int
			if err := scan(&state, &n); err != nil {
				return err
			}
			out.LegacyCommitments[state] = n
			return nil
		}); err != nil {
			return out, err
		}
		if err := (&Store{db: db}).requireColumns("memory_items", []string{"id", "kind", "content", "status", "session_id", "source_run_id", "wake_at", "recurrence", "created_at", "updated_at"}); err != nil {
			return out, err
		}
	} else {
		out.Schema = "personal-memory"
		if err := (&Store{db: db}).validateMemorySchema(); err != nil {
			return out, err
		}
	}
	if out.ActiveRuns > 0 || out.InterruptedRuns > 0 || out.PendingActions > 0 {
		out.Ready = false
		out.Reason = "finish or explicitly cancel active and interrupted runs before migration"
	}
	if out.Integrity != "ok" {
		out.Reason = "SQLite quick_check failed"
	}
	return out, nil
}
