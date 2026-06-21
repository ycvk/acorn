package sqlite

import (
	"fmt"
	"strings"
)

// addColumnIfNotExists adds a column to a table idempotently, recording the
// migration version in schema_migrations so it is not re-run. A column that
// already exists (e.g. V2→V1→V2) is recorded as a successful no-op.
func (s *Store) addColumnIfNotExists(table, column, definition, versionKey string) error {
	// Check schema_migrations for idempotency
	if migrationApplied(s.db, versionKey) {
		return nil
	}

	_, err := s.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	if err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("add column %s.%s: %w", table, column, err)
		}
		// Column already exists (e.g., V2→V1→V2 scenario), record and continue
		if _, insertErr := s.db.Exec("INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))", versionKey); insertErr != nil {
			return fmt.Errorf("record duplicate column migration %s: %w", versionKey, insertErr)
		}
		return nil
	}
	_, insertErr := s.db.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))", versionKey)
	return insertErr
}
