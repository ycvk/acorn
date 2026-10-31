package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// createSchema lays out an empty database at schemaVersion in one
// transaction.
func createSchema(ctx context.Context, db *sql.DB) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackOnErr(tx, &err, "create sqlite schema")
	for _, step := range []struct{ name, ddl string }{
		{"tables", storeBootstrapTables},
		{"memory tables", memorySchema},
		{"memory indexes", memoryQueryIndexes},
		{"memory triggers", memorySourceTriggers},
		{"indexes", storeBootstrapIndexes},
	} {
		if _, err := tx.ExecContext(ctx, step.ddl); err != nil {
			return fmt.Errorf("create sqlite schema (%s): %w", step.name, err)
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}
	return tx.Commit()
}

func (s *Store) configure() error {
	if _, err := s.db.Exec(`PRAGMA busy_timeout = 5000;`); err != nil {
		return fmt.Errorf("configure sqlite pragma %q: %w", `PRAGMA busy_timeout = 5000;`, err)
	}
	mode, err := s.journalMode()
	if err != nil {
		return err
	}
	if !strings.EqualFold(mode, "wal") {
		row := s.db.QueryRow(`PRAGMA journal_mode = WAL;`)
		var applied string
		if err := row.Scan(&applied); err != nil {
			return fmt.Errorf("configure sqlite pragma %q: %w", `PRAGMA journal_mode = WAL;`, err)
		}
		if !strings.EqualFold(strings.TrimSpace(applied), "wal") {
			return fmt.Errorf("configure sqlite pragma %q: applied mode %q", `PRAGMA journal_mode = WAL;`, applied)
		}
	}
	if _, err := s.db.Exec(`PRAGMA synchronous = NORMAL;`); err != nil {
		return fmt.Errorf("configure sqlite pragma %q: %w", `PRAGMA synchronous = NORMAL;`, err)
	}
	if _, err := s.db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		return fmt.Errorf("configure sqlite pragma %q: %w", `PRAGMA foreign_keys = ON;`, err)
	}
	return nil
}

func (s *Store) journalMode() (string, error) {
	row := s.db.QueryRow(`PRAGMA journal_mode;`)
	var mode string
	if err := row.Scan(&mode); err != nil {
		return "", fmt.Errorf("query sqlite pragma %q: %w", `PRAGMA journal_mode;`, err)
	}
	return strings.TrimSpace(mode), nil
}

// rollbackOnErr rolls back a transaction if the surrounding function returned
// an error, swallowing the benign ErrTxDone that fires after a successful Commit.
func rollbackOnErr(tx *sql.Tx, err *error, label string) {
	rollbackErr := tx.Rollback()
	if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
		*err = errors.Join(*err, fmt.Errorf("%s rollback: %w", label, rollbackErr))
	}
}
