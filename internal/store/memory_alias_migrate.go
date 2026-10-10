package store

import (
	"context"
	"database/sql"
)

func (s *Store) migrateMemoryAliases() error {
	const version = "v8_memory_entity_aliases"
	if migrationApplied(s.db, version) {
		return nil
	}
	ctx := context.Background()
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, memoryAliasSchema); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,applied_at) VALUES(?,datetime('now'))`, version)
		return err
	})
}
