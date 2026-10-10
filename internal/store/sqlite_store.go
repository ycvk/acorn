package store

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	"database/sql"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/ycvk/acorn/internal/core"
)

// Compile-time assertions that *Store implements the core capability interfaces.
var (
	_ core.SessionStore    = (*Store)(nil)
	_ core.IdentityStore   = (*Store)(nil)
	_ core.ArtifactStore   = (*Store)(nil)
	_ core.MemoryStore     = (*Store)(nil)
	_ core.CommitmentStore = (*Store)(nil)
	_ core.ActivityStore   = (*Store)(nil)
)

type Store struct {
	lock         *os.File
	sourceReader core.MemorySourceReader
	db           *sql.DB
	// read serves memory recall and candidate reads. WAL lets these readers run
	// beside the single serialized writer connection in db.
	read        *sql.DB
	artifactDir string
	artifactSvc *ArtifactService
}

const fixedTimestampLayout = "2006-01-02T15:04:05.000000000Z07:00"

func formatTimestamp(value time.Time) string {
	return value.UTC().Format(fixedTimestampLayout)
}

type OpenOptions struct {
	SourceReader core.MemorySourceReader
	Exclusive    bool
}

func Open(dir string, options ...OpenOptions) (*Store, error) {
	var sourceReader core.MemorySourceReader
	var exclusive bool
	if len(options) > 1 {
		return nil, fmt.Errorf("store.Open accepts one options value")
	}
	if len(options) == 1 {
		sourceReader = options[0].SourceReader
		exclusive = options[0].Exclusive
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create storage dir: %w", err)
	}
	lock, err := lockStorage(dir, exclusive)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			_ = lock.Close()
		}
	}()
	db, err := sql.Open("sqlite", filepath.Join(dir, "acorn.db"))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	artifactDir := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create artifact dir: %w", err)
	}
	store := &Store{lock: lock, db: db, artifactDir: artifactDir, sourceReader: sourceReader}
	artifactSvc, err := NewArtifactService(artifactDir, store)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create artifact service: %w", err)
	}
	store.artifactSvc = artifactSvc
	if err := store.configure(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	read, err := openReadPool(filepath.Join(dir, "acorn.db"))
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	store.read = read
	success = true
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return errors.Join(s.read.Close(), s.db.Close(), s.lock.Close())
}

const readConnections = 4

func openReadPool(path string) (*sql.DB, error) {
	dsn := url.URL{Scheme: "file", Path: path, RawQuery: url.Values{"_pragma": {"busy_timeout(5000)", "query_only(1)"}}.Encode()}
	read, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, fmt.Errorf("open sqlite read pool: %w", err)
	}
	read.SetMaxOpenConns(readConnections)
	read.SetMaxIdleConns(readConnections)
	if err := read.PingContext(context.Background()); err != nil {
		_ = read.Close()
		return nil, fmt.Errorf("open sqlite read pool: %w", err)
	}
	return read, nil
}

func parseTimestamp(layout, value, field string) (time.Time, error) {
	t, err := time.Parse(layout, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse %s timestamp %q: %w", field, value, err)
	}
	return t, nil
}
