package store

import (
	"fmt"
	"os"
	"time"

	"database/sql"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/ycvk/acorn/internal/port"
)

// Compile-time assertions that *Store implements every port.*Repo interface.
var (
	_ port.SessionRepo       = (*Store)(nil)
	_ port.MessageRepo       = (*Store)(nil)
	_ port.RunRepo           = (*Store)(nil)
	_ port.EventRepo         = (*Store)(nil)
	_ port.PendingActionRepo = (*Store)(nil)
	_ port.DeviceRepo        = (*Store)(nil)
	_ port.ArtifactRepo      = (*Store)(nil)
	_ port.OAuthRepo         = (*Store)(nil)
	_ port.SummaryRepo       = (*Store)(nil)
)

type Store struct {
	db          *sql.DB
	artifactDir string
}

const fixedTimestampLayout = "2006-01-02T15:04:05.000000000Z07:00"

func formatTimestamp(value time.Time) string {
	return value.UTC().Format(fixedTimestampLayout)
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create storage dir: %w", err)
	}
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

	store := &Store{db: db, artifactDir: artifactDir}
	if err := store.configure(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func parseTimestamp(layout, value, field string) (time.Time, error) {
	t, err := time.Parse(layout, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse %s timestamp %q: %w", field, value, err)
	}
	return t, nil
}
