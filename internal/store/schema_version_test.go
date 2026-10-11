package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

// A recorded layout is the schema a released version creates. Layouts are
// written once, when their version is new: set ACORN_RECORD_SCHEMA=1 and run
// TestOpenCreatesTheRecordedLayout.
func recordedLayoutPath(version int) string {
	return filepath.Join("testdata", "schema", fmt.Sprintf("v%d.sql", version))
}

// layoutStatements lists the CREATE statements of a database in creation
// order, leaving out SQLite's own objects and the shadow tables FTS5 creates
// for each virtual table.
func layoutStatements(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT type,name,sql FROM sqlite_master WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%'
 ORDER BY CASE type WHEN 'table' THEN 0 WHEN 'index' THEN 1 ELSE 2 END, name`)
	if err != nil {
		t.Fatal(err)
	}
	type object struct{ kind, name, sql string }
	var objects []object
	var virtual []string
	if err := scanRows(rows, func(scan func(...any) error) error {
		var o object
		if err := scan(&o.kind, &o.name, &o.sql); err != nil {
			return err
		}
		if strings.HasPrefix(strings.ToUpper(o.sql), "CREATE VIRTUAL TABLE") {
			virtual = append(virtual, o.name)
		}
		objects = append(objects, o)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, o := range objects {
		shadow := false
		for _, v := range virtual {
			shadow = shadow || strings.HasPrefix(o.name, v+"_")
		}
		if !shadow {
			out = append(out, o.sql+";")
		}
	}
	return out
}

func readLayout(t *testing.T, version int) string {
	t.Helper()
	data, err := os.ReadFile(recordedLayoutPath(version))
	if err != nil {
		t.Fatalf("recorded layout v%d: %v", version, err)
	}
	return string(data)
}

func openRaw(t *testing.T, dir string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, "acorn.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestOpenCreatesTheRecordedLayout(t *testing.T) {
	s := openTestStore(t)
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("user_version=%d err=%v, want %d", version, err, schemaVersion)
	}
	layout := strings.Join(layoutStatements(t, s.db), "\n") + "\n"
	path := recordedLayoutPath(schemaVersion)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) && os.Getenv("ACORN_RECORD_SCHEMA") == "1" {
		if err := os.WriteFile(path, []byte(layout), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if recorded := readLayout(t, schemaVersion); layout != recorded {
		t.Fatalf("the schema differs from %s; a layout change needs schemaVersion bumped, a migration from the previous version, and the new layout recorded", path)
	}
}

func TestMigrationsReachEveryRecordedLayout(t *testing.T) {
	if len(migrations) != schemaVersion-1 {
		t.Fatalf("%d migrations for schema version %d", len(migrations), schemaVersion)
	}
	want := readLayout(t, schemaVersion)
	for version := 1; version <= schemaVersion; version++ {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			dir := t.TempDir()
			raw := openRaw(t, dir)
			if _, err := raw.Exec(readLayout(t, version) + fmt.Sprintf("PRAGMA user_version = %d;", version)); err != nil {
				t.Fatalf("build v%d: %v", version, err)
			}
			if err := raw.Close(); err != nil {
				t.Fatal(err)
			}
			s, err := Open(dir)
			if err != nil {
				t.Fatalf("open v%d: %v", version, err)
			}
			t.Cleanup(func() { _ = s.Close() })
			if got := strings.Join(layoutStatements(t, s.db), "\n") + "\n"; got != want {
				t.Fatalf("v%d upgraded to a layout that differs from a new database", version)
			}
		})
	}
}

func TestOpenRefusesDatabasesItCannotRead(t *testing.T) {
	for name, setup := range map[string]string{
		"no version":    `CREATE TABLE sessions(session_id TEXT PRIMARY KEY);`,
		"newer version": fmt.Sprintf(`CREATE TABLE sessions(session_id TEXT PRIMARY KEY); PRAGMA user_version = %d;`, schemaVersion+1),
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			raw := openRaw(t, dir)
			if _, err := raw.Exec(setup); err != nil {
				t.Fatal(err)
			}
			if err := raw.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(dir); !errors.Is(err, core.ErrUnsupportedStorageSchema) {
				t.Fatalf("Open = %v, want unsupported schema", err)
			}
		})
	}
}

func TestMigrateSchemaCommitsEachStepWithItsVersion(t *testing.T) {
	raw := openRaw(t, t.TempDir())
	raw.SetMaxOpenConns(1)
	ctx := context.Background()
	if _, err := raw.Exec(`CREATE TABLE notes(id INTEGER PRIMARY KEY); PRAGMA user_version = 1;`); err != nil {
		t.Fatal(err)
	}
	var applied []int
	step := func(version int, fail bool) func(context.Context, *sql.Tx) error {
		return func(ctx context.Context, tx *sql.Tx) error {
			applied = append(applied, version)
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE notes ADD COLUMN c%d TEXT NOT NULL DEFAULT ''`, version)); err != nil {
				return err
			}
			if fail {
				return errors.New("step failed")
			}
			return nil
		}
	}
	err := migrateSchema(ctx, raw, 1, []func(context.Context, *sql.Tx) error{step(1, false), step(2, true), step(3, false)})
	if err == nil || !strings.Contains(err.Error(), "migrate schema 2 to 3") {
		t.Fatalf("migrate = %v, want the failing step named", err)
	}
	var version int
	if err := raw.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 2 {
		t.Fatalf("user_version=%d err=%v, want 2 after the first step", version, err)
	}
	var c2 int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('notes') WHERE name='c2'`).Scan(&c2); err != nil || c2 != 0 {
		t.Fatalf("failed step left column c2: %d %v", c2, err)
	}
	applied = nil
	if err := migrateSchema(ctx, raw, 2, []func(context.Context, *sql.Tx) error{step(1, false), step(2, false), step(3, false)}); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 4 || fmt.Sprint(applied) != "[2 3]" {
		t.Fatalf("resumed upgrade: version=%d applied=%v err=%v", version, applied, err)
	}
}

func TestMigrateSchemaRejectsBrokenForeignKeys(t *testing.T) {
	raw := openRaw(t, t.TempDir())
	raw.SetMaxOpenConns(1)
	if _, err := raw.Exec(`CREATE TABLE parent(id INTEGER PRIMARY KEY); CREATE TABLE child(parent_id INTEGER REFERENCES parent(id)); PRAGMA user_version = 1;`); err != nil {
		t.Fatal(err)
	}
	err := migrateSchema(context.Background(), raw, 1, []func(context.Context, *sql.Tx) error{func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO child(parent_id) VALUES(7)`)
		return err
	}})
	if err == nil || !strings.Contains(err.Error(), "broken foreign keys") {
		t.Fatalf("migrate = %v, want broken foreign keys", err)
	}
}

func TestSchemaUpgradeWaitsForOtherProcessesToClose(t *testing.T) {
	dir := t.TempDir()
	first, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.lockForUpgrade(); err == nil || !strings.Contains(err.Error(), "storage is in use") {
		t.Fatalf("upgrade lock with another store open = %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.lockForUpgrade(); err != nil {
		t.Fatal(err)
	}
	if err := first.releaseUpgradeLock(); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
}
