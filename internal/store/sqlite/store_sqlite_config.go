package sqlite

import (
	"fmt"
	"strings"
)

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
