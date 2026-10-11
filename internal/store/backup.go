package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

// SnapshotDatabase writes a consistent copy of the database in storageDir to
// dest. It reads through a read-only connection, so a running Acorn keeps
// writing meanwhile.
func SnapshotDatabase(ctx context.Context, storageDir, dest string) error {
	source := filepath.Join(storageDir, "acorn.db")
	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("database to back up: %w", err)
	}
	dsn := url.URL{Scheme: "file", Path: source, RawQuery: url.Values{"mode": {"ro"}, "_pragma": {"busy_timeout(5000)"}}.Encode()}
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return fmt.Errorf("open database to back up: %w", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, dest); err != nil {
		return fmt.Errorf("snapshot database: %w", err)
	}
	return nil
}

// VerifyDatabase checks that a database copy is intact and at the schema
// version this build reads.
func VerifyDatabase(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("database copy: %w", err)
	}
	dsn := url.URL{Scheme: "file", Path: path, RawQuery: url.Values{"mode": {"ro"}}.Encode()}
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return fmt.Errorf("open database copy: %w", err)
	}
	defer db.Close()
	var result string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result); err != nil {
		return fmt.Errorf("check database copy: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("database copy %s fails its integrity check: %s", path, result)
	}
	var version int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read database copy version: %w", err)
	}
	if version != schemaVersion {
		return fmt.Errorf("database copy %s is at schema version %d; this build reads %d", path, version, schemaVersion)
	}
	return nil
}

// RestoreStorage replaces the database in storageDir with a verified copy
// and then runs restoreFiles, all under the exclusive storage lock, so no
// Acorn process may have the storage open.
func RestoreStorage(ctx context.Context, storageDir, copyPath string, restoreFiles func() error) error {
	if err := VerifyDatabase(ctx, copyPath); err != nil {
		return err
	}
	lock, err := lockStorage(storageDir, true)
	if err != nil {
		return fmt.Errorf("restore needs Acorn stopped: %w", err)
	}
	defer lock.Close()
	target := filepath.Join(storageDir, "acorn.db")
	staged := target + ".restore"
	if err := copyFile(copyPath, staged); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(target + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", target+suffix, err)
		}
	}
	if err := os.Rename(staged, target); err != nil {
		return fmt.Errorf("install restored database: %w", err)
	}
	return restoreFiles()
}

func copyFile(from, to string) (err error) {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
	}()
	if _, err := out.ReadFrom(in); err != nil {
		return fmt.Errorf("copy %s: %w", from, err)
	}
	return out.Sync()
}
