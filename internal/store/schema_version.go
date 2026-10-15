package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"gopkg.in/yaml.v3"
)

// schemaVersion is the database layout this build creates and expects. A
// layout change bumps it, appends the migration from the previous version to
// migrations, and records the new layout under testdata/schema.
const schemaVersion = 6

// migrations[i] upgrades a database from version i+1 to version i+2.
var migrations = []func(context.Context, *sql.Tx) error{
	addNotificationActionID,
	addNotificationBrowserURL,
	addSkills,
	forgetDeletedThreads,
	adoptAgentSkills,
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

// addNotificationBrowserURL (2 to 3) records the page a tap on a push opens in
// the agent's browser, rebuilding the table the same way.
func addNotificationBrowserURL(ctx context.Context, tx *sql.Tx) error {
	columns := "id, title, body, thread_id, run_id, status, send_after, error_text, created_at, sent_at, action_id"
	for _, statement := range []string{
		`ALTER TABLE notifications RENAME TO notifications_v2`,
		notificationsTable,
		`INSERT INTO notifications(` + columns + `) SELECT ` + columns + ` FROM notifications_v2`,
		`DROP TABLE notifications_v2`,
		notificationsDueIndex,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// addSkills (3 to 4) adds the skills and their revisions; the built-in skills
// are written on the next start.
func addSkills(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, skillsTables)
	return err
}

// forgetDeletedThreads (4 to 5) removes a run's search entry with the run, and
// finishes the threads deleted while deletion only removed their messages:
// their conversation sources are excluded on the owner's deletion request and
// their runs' records removed, as DeleteThread does.
func forgetDeletedThreads(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, runsFTSDeleteTrigger); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT session_id FROM runs WHERE session_id <> '' AND session_id NOT IN (SELECT session_id FROM sessions)
		UNION SELECT session_id FROM memory_sources WHERE kind IN ('message','event') AND session_id <> '' AND session_id NOT IN (SELECT session_id FROM sessions)`)
	if err != nil {
		return err
	}
	deleted, err := memoryStringRows(rows)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, sessionID := range deleted {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM memory_sources WHERE session_id = ? AND kind IN ('message','event') ORDER BY id`, sessionID)
		if err != nil {
			return err
		}
		sources, err := memoryStringRows(rows)
		if err != nil {
			return err
		}
		if len(sources) > 0 {
			request := core.MemorySource{ID: "request:deleted-" + sessionID, Kind: core.MemoryRequestSource, ObjectID: "request:deleted-" + sessionID, Version: "1", Speaker: "owner", Body: "Delete conversation " + sessionID, RecordedAt: now}
			if err := registerMemorySource(ctx, tx, request); err != nil {
				return err
			}
			if err := forgetMemory(ctx, tx, core.MemoryForget{SourceIDs: sources, RequestSourceID: request.ID, Reason: "owner deleted the conversation", Now: now}); err != nil {
				return err
			}
		}
		paths, err := purgeThread(ctx, tx, sessionID)
		if err != nil {
			return err
		}
		if len(paths) > 0 {
			return fmt.Errorf("deleted thread %s left artifacts %v, whose files an upgrade cannot remove", sessionID, paths)
		}
	}
	return nil
}

// adoptAgentSkills (5 to 6) adds skill files and staged packages, and moves
// each skill to the Agent Skills format as a new revision: its id becomes a
// name such as csv-group-count, its summary and trigger hints a description,
// and its required tools metadata. Built-in skills then get their new
// content from the next start's seed.
func adoptAgentSkills(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, skillPackageTables); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, markdown, revision, retired FROM skills ORDER BY id`)
	if err != nil {
		return err
	}
	type stored struct {
		id, markdown string
		revision     int64
		retired      bool
	}
	var skills []stored
	if err := scanRows(rows, func(scan func(...any) error) error {
		var s stored
		if err := scan(&s.id, &s.markdown, &s.revision, &s.retired); err != nil {
			return err
		}
		skills = append(skills, s)
		return nil
	}); err != nil {
		return err
	}
	now := formatTimestamp(time.Now().UTC())
	taken := map[string]string{}
	for _, s := range skills {
		name, markdown, err := agentSkillOf(s.id, s.markdown)
		if err != nil {
			return fmt.Errorf("skill %s: %w", s.id, err)
		}
		if other, ok := taken[name]; ok {
			return fmt.Errorf("skills %s and %s both become %s", other, s.id, name)
		}
		taken[name] = s.id
		sum := sha256.Sum256([]byte(markdown))
		for _, statement := range []struct {
			query string
			args  []any
		}{
			{`UPDATE skill_revisions SET id = ? WHERE id = ?`, []any{name, s.id}},
			{`UPDATE skills SET id = ?, markdown = ?, revision = ?, updated_at = ? WHERE id = ?`, []any{name, markdown, s.revision + 1, now, s.id}},
			{`INSERT INTO skill_revisions (id, revision, markdown, markdown_sha256, retired, author, run_id, at) VALUES (?, ?, ?, ?, ?, 'release', '', ?)`, []any{name, s.revision + 1, markdown, hex.EncodeToString(sum[:]), s.retired, now}},
		} {
			if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
				return err
			}
		}
	}
	return nil
}

// agentSkillOf rewrites a skill of schema version 5 in the Agent Skills
// format and returns its name.
func agentSkillOf(id, markdown string) (string, string, error) {
	front, body, ok := strings.Cut(strings.TrimPrefix(markdown, "---\n"), "\n---\n")
	if !strings.HasPrefix(markdown, "---\n") || !ok {
		return "", "", errors.New("no frontmatter")
	}
	var old struct {
		Summary      string   `yaml:"summary"`
		TriggerHints []string `yaml:"trigger_hints"`
		Requires     struct {
			Tools []string `yaml:"tools"`
		} `yaml:"requires"`
	}
	if err := yaml.Unmarshal([]byte(front), &old); err != nil {
		return "", "", fmt.Errorf("frontmatter: %w", err)
	}
	name := strings.Trim(skillNameRun.ReplaceAllString(strings.ToLower(strings.TrimPrefix(id, "skill.")), "-"), "-")
	description := strings.TrimSpace(old.Summary)
	if len(old.TriggerHints) > 0 {
		description += " Use for: " + strings.Join(old.TriggerHints, "; ") + "."
	}
	if runes := []rune(description); len(runes) > 1024 {
		description = string(runes[:1024])
	}
	meta := struct {
		Name        string            `yaml:"name"`
		Description string            `yaml:"description"`
		Metadata    map[string]string `yaml:"metadata,omitempty"`
	}{Name: name, Description: description}
	if len(old.Requires.Tools) > 0 {
		meta.Metadata = map[string]string{"acorn-requires-tools": strings.Join(old.Requires.Tools, " ")}
	}
	front2, err := yaml.Marshal(meta)
	if err != nil {
		return "", "", err
	}
	return name, "---\n" + string(front2) + "---\n" + strings.TrimLeft(body, "\n"), nil
}

// skillNameRun matches what a skill name cannot hold.
var skillNameRun = regexp.MustCompile(`[^a-z0-9]+`)

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
