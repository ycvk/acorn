package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Existing vectors keep their generation and revision. Historical gaps become
// durable embedding jobs in the same transaction as the index key change.
func (s *Store) migrateMemoryRevisionVectors() error {
	const version = "v7_memory_revision_vectors"
	if migrationApplied(s.db, version) {
		return nil
	}
	ctx := context.Background()
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		var revisionKey int
		if err := tx.QueryRowContext(ctx, `SELECT pk FROM pragma_table_info('memory_embeddings') WHERE name='revision'`).Scan(&revisionKey); err != nil {
			return err
		}
		if revisionKey == 0 {
			statements := []string{
				`ALTER TABLE memory_vector_sketches RENAME TO memory_sketch_layout`,
				`ALTER TABLE memory_embeddings RENAME TO memory_vector_layout`,
				memoryVectorSchema,
				`INSERT INTO memory_embeddings SELECT record_id,revision,generation,dimensions,vector FROM memory_vector_layout`,
				`INSERT INTO memory_vector_sketches SELECT generation,record_id,revision,dimensions,scale,codes FROM memory_sketch_layout`,
				`DROP TABLE memory_sketch_layout`,
				`DROP TABLE memory_vector_layout`,
			}
			for _, statement := range statements {
				if _, err := tx.ExecContext(ctx, statement); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO memory_jobs(operation,object_id,version,available_at,created_at)
 SELECT 'embed',v.record_id,CAST(v.revision AS TEXT),v.at,v.at FROM memory_revisions v JOIN memory_records r ON r.id=v.record_id
 WHERE r.excluded=0 AND `+memoryRevisionVisibility+` AND NOT EXISTS(SELECT 1 FROM memory_embeddings e JOIN memory_index_state i ON e.generation=i.generation WHERE e.record_id=v.record_id AND e.revision=v.revision)
 ON CONFLICT(operation,object_id,version,processor_version) DO UPDATE SET state='pending',token=memory_jobs.token+1,attempts=0,lease_until='',error='',completed_at='',available_at=excluded.available_at`); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,applied_at) VALUES(?,datetime('now'))`, version)
		return err
	})
	if err != nil {
		return fmt.Errorf("migrate memory revision vectors: %w", err)
	}
	return nil
}
