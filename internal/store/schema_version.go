package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ycvk/acorn/internal/core"
)

// schemaVersion is the database layout this build creates and expects. A
// layout change bumps it, appends the migration from the previous version to
// migrations, and records the new layout under testdata/schema.
const schemaVersion = 2

// migrations[i] upgrades a database from version i+1 to version i+2.
var migrations = []func(context.Context, *sql.Tx) error{
	addNotificationActionID,
}

// addNotificationActionID (1 to 2) records which pending action a push
// announces. The table is rebuilt from notificationsTable so its stored
// definition matches a new database.
func addNotificationActionID(ctx context.Context, tx *sql.Tx) error {
	columns := "id, title, body, thread_id, run_id, status, send_after, error_text, created_at, sent_at"
	for _, statement := range []string{
		`ALTER TABLE notifications RENAME TO notifications_v1`,
		notificationsTable,
		`INSERT INTO notifications(` + columns + `) SELECT ` + columns + ` FROM notifications_v1`,
		`DROP TABLE notifications_v1`,
		notificationsDueIndex,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// openSchema creates the layout of an empty database, upgrades an older one
// and refuses one this build cannot read.
func (s *Store) openSchema(ctx context.Context) error {
	version, tables, err := schemaState(ctx, s.db)
	if err != nil {
		return err
	}
	switch {
	case tables == 0:
		if err := createSchema(ctx, s.db); err != nil {
			return err
		}
	case version == 0:
		return fmt.Errorf("%w: the database has no schema version; restore a backup made by Acorn or start from an empty storage directory", core.ErrUnsupportedStorageSchema)
	case version > schemaVersion:
		return fmt.Errorf("%w: the database is at schema version %d and this build reads up to %d; install a newer Acorn", core.ErrUnsupportedStorageSchema, version, schemaVersion)
	case version < schemaVersion:
		if err := s.lockForUpgrade(); err != nil {
			return err
		}
		if err := migrateSchema(ctx, s.db, version, migrations); err != nil {
			return err
		}
		if err := s.releaseUpgradeLock(); err != nil {
			return err
		}
	}
	return s.validateSchema()
}

func schemaState(ctx context.Context, db *sql.DB) (version, tables int, err error) {
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return 0, 0, fmt.Errorf("read schema version: %w", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`).Scan(&tables); err != nil {
		return 0, 0, fmt.Errorf("count tables: %w", err)
	}
	return version, tables, nil
}

// migrateSchema applies steps[from-1:] in order. Each step commits with its
// version bump, so an interrupted upgrade resumes at the first unfinished
// step. Foreign keys are off during the upgrade, as table rebuilds require,
// and every step must leave none broken.
func migrateSchema(ctx context.Context, db *sql.DB, from int, steps []func(context.Context, *sql.Tx) error) error {
	if from < 1 || from > len(steps)+1 {
		return fmt.Errorf("no migration path from schema version %d", from)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	for version := from; version <= len(steps); version++ {
		if err := migrateStep(ctx, db, version, steps[version-1]); err != nil {
			return fmt.Errorf("migrate schema %d to %d: %w", version, version+1, err)
		}
	}
	_, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON`)
	return err
}

func migrateStep(ctx context.Context, db *sql.DB, version int, step func(context.Context, *sql.Tx) error) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackOnErr(tx, &err, "migrate schema")
	if err := step(ctx, tx); err != nil {
		return err
	}
	broken, err := brokenForeignKeys(ctx, tx)
	if err != nil {
		return err
	}
	if broken {
		return fmt.Errorf("migration leaves broken foreign keys")
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version+1)); err != nil {
		return err
	}
	return tx.Commit()
}

func brokenForeignKeys(ctx context.Context, tx *sql.Tx) (bool, error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	broken := rows.Next()
	return broken, rows.Err()
}
