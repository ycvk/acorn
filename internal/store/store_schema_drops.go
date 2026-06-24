package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func (s *Store) dropDecisionTables() (err error) {
	const version = "v2_drop_decision_tables"
	if migrationApplied(s.db, version) {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin decision table drop: %w", err)
	}
	defer rollbackOnErr(tx, &err, "decision table drop")

	if _, err := tx.Exec(`DROP TABLE IF EXISTS run_decisions`); err != nil {
		return fmt.Errorf("drop run_decisions table: %w", err)
	}
	if err := dropDecisionSnapshotColumns(tx); err != nil {
		return err
	}
	return commitDropMigration(tx, version, "decision table drop")
}

func (s *Store) dropRefactoredTables() (err error) {
	const version = "v3_drop_refactored_tables"
	if migrationApplied(s.db, version) {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin refactored table drop: %w", err)
	}
	defer rollbackOnErr(tx, &err, "refactored table drop")
	statements := []string{
		`DROP TABLE IF EXISTS plan_steps`,
		`DROP TABLE IF EXISTS context_boundaries`,
		`DROP TABLE IF EXISTS tool_results`,
		`DROP TABLE IF EXISTS conversation_segments`,
		`DROP TABLE IF EXISTS run_archives`,
		`DROP TABLE IF EXISTS working_checkpoints`,
		`DROP TABLE IF EXISTS provider_usages`,
		`DROP TABLE IF EXISTS run_context_snapshots`,
		`DROP TABLE IF EXISTS checkpoints`,
	}
	if err := execDropStatements(tx, statements, "refactored table"); err != nil {
		return err
	}
	return commitDropMigration(tx, version, "refactored table drop")
}

// dropDecisionSnapshotColumns drops the legacy decision_* columns from
// run_context_snapshots, swallowing the benign "no such column"/"no such
// table" errors that fire when a column was already dropped or the table no
// longer exists (post-refactor fresh databases never create it).
func dropDecisionSnapshotColumns(tx *sql.Tx) error {
	for _, col := range []string{"decision_profile_hash", "decision_action", "decision_skill_id"} {
		if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE run_context_snapshots DROP COLUMN %s", col)); err != nil {
			msg := err.Error()
			if strings.Contains(msg, "no such column") || strings.Contains(msg, "no such table") {
				continue
			}
			return fmt.Errorf("drop run_context_snapshots column %s: %w", col, err)
		}
	}
	return nil
}

func (s *Store) dropLegacyMemoryTables() (err error) {
	const version = "v2_drop_legacy_memory_tables"
	if migrationApplied(s.db, version) {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin legacy memory table drop: %w", err)
	}
	defer rollbackOnErr(tx, &err, "legacy memory table drop")

	statements := []string{
		`DROP TABLE IF EXISTS knowledge_search_idx`,
		`DROP TABLE IF EXISTS knowledge_search_docs`,
		`DROP TABLE IF EXISTS skill_patch_history`,
		`DROP TABLE IF EXISTS knowledge_facts`,
		`DROP TABLE IF EXISTS memory_evidence`,
		`DROP TABLE IF EXISTS episodic_memories`,
		`DROP TABLE IF EXISTS core_memory_blocks`,
	}
	if err := execDropStatements(tx, statements, "legacy memory table"); err != nil {
		return err
	}
	return commitDropMigration(tx, version, "legacy memory table drop")
}

func (s *Store) dropRemovedRuntimeTables() (err error) {
	const version = "v2_drop_removed_runtime_tables"
	if migrationApplied(s.db, version) {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin removed runtime table drop: %w", err)
	}
	defer rollbackOnErr(tx, &err, "removed runtime table drop")

	if _, err := tx.Exec(`DROP TABLE IF EXISTS run_snapshots`); err != nil {
		return fmt.Errorf("drop removed runtime table: %w", err)
	}
	return commitDropMigration(tx, version, "removed runtime table drop")
}

func (s *Store) dropRemovedTerminalSessionTables() (err error) {
	const version = "v2_drop_removed_terminal_session_tables"
	if migrationApplied(s.db, version) {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin removed terminal session table drop: %w", err)
	}
	defer rollbackOnErr(tx, &err, "removed terminal session table drop")

	statements := []string{
		`DROP TABLE IF EXISTS terminal_session_logs`,
		`DROP TABLE IF EXISTS terminal_sessions`,
	}
	if err := execDropStatements(tx, statements, "removed terminal session table"); err != nil {
		return err
	}
	return commitDropMigration(tx, version, "removed terminal session table drop")
}

func (s *Store) dropRemovedCodeintelTables() (err error) {
	const version = "v2_drop_removed_codeintel_tables"
	if migrationApplied(s.db, version) {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin removed codeintel table drop: %w", err)
	}
	defer rollbackOnErr(tx, &err, "removed codeintel table drop")

	statements := []string{
		`DROP TRIGGER IF EXISTS codeintel_search_docs_ai`,
		`DROP TRIGGER IF EXISTS codeintel_search_docs_ad`,
		`DROP TRIGGER IF EXISTS codeintel_search_docs_au`,
		`DROP TABLE IF EXISTS codeintel_search_idx`,
		`DROP TABLE IF EXISTS codeintel_search_docs`,
		`DROP TABLE IF EXISTS codeintel_edges`,
		`DROP TABLE IF EXISTS codeintel_symbols`,
		`DROP TABLE IF EXISTS codeintel_files`,
		`DROP TABLE IF EXISTS codeintel_index_meta`,
	}
	if err := execDropStatements(tx, statements, "removed codeintel table"); err != nil {
		return err
	}
	return commitDropMigration(tx, version, "removed codeintel table drop")
}

func (s *Store) dropRemovedConversationFTS() (err error) {
	const version = "v2_drop_removed_conversation_fts"
	if migrationApplied(s.db, version) {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin removed conversation fts drop: %w", err)
	}
	defer rollbackOnErr(tx, &err, "removed conversation fts drop")

	statements := []string{
		`DROP TRIGGER IF EXISTS conv_seg_ai`,
		`DROP TRIGGER IF EXISTS conv_seg_ad`,
		`DROP TRIGGER IF EXISTS conv_seg_au`,
		`DROP TABLE IF EXISTS conversation_segments_idx`,
	}
	if err := execDropStatements(tx, statements, "removed conversation fts object"); err != nil {
		return err
	}
	return commitDropMigration(tx, version, "removed conversation fts drop")
}

// dropArchitecturalRefactorTables drops the tables retired by the Phase 3
// schema redesign: owner_profile (single-user system) and session_summaries
// (compact boundary not persisted). This migration is idempotent.
func (s *Store) dropArchitecturalRefactorTables() (err error) {
	const version = "v4_drop_architectural_refactor_tables"
	if migrationApplied(s.db, version) {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin architectural refactor table drop: %w", err)
	}
	defer rollbackOnErr(tx, &err, "architectural refactor table drop")
	statements := []string{
		`DROP TABLE IF EXISTS owner_profile`,
		`DROP TABLE IF EXISTS session_summaries`,
	}
	if err := execDropStatements(tx, statements, "architectural refactor table"); err != nil {
		return err
	}
	return commitDropMigration(tx, version, "architectural refactor table drop")
}

// migrationApplied reports whether a schema migration version has already been
// recorded in schema_migrations.
func migrationApplied(db *sql.DB, version string) bool {
	var count int
	row := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version)
	if err := row.Scan(&count); err == nil && count > 0 {
		return true
	}
	return false
}

// execDropStatements runs each DROP statement in a slice within the given tx.
func execDropStatements(tx *sql.Tx, statements []string, label string) error {
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("drop %s: %w", label, err)
		}
	}
	return nil
}

// commitDropMigration records the migration version and commits the tx.
func commitDropMigration(tx *sql.Tx, version, label string) error {
	if _, err := tx.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))", version); err != nil {
		return fmt.Errorf("record %s migration: %w", label, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", label, err)
	}
	return nil
}

// rollbackOnErr is deferred from each drop migration to roll back the tx if the
// surrounding function returned an error, swallowing the benign ErrTxDone that
// fires after a successful Commit.
func rollbackOnErr(tx *sql.Tx, err *error, label string) {
	rollbackErr := tx.Rollback()
	if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
		*err = errors.Join(*err, fmt.Errorf("%s rollback: %w", label, rollbackErr))
	}
}
