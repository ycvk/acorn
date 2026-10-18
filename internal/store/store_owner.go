package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

var _ core.OwnerStore = (*Store)(nil)

// addOwner (14 to 15) adds the table that holds what Acorn calls the owner.
func addOwner(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, ownerTable)
	return err
}

// SetOwnerName saves what Acorn calls the owner; an empty name leaves the
// owner unnamed.
func (s *Store) SetOwnerName(ctx context.Context, name string, at time.Time) (core.OwnerName, error) {
	if at.IsZero() {
		return core.OwnerName{}, errors.New("set owner name: at is required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO owner (id, name, updated_at) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, updated_at = excluded.updated_at`, name, formatTimestamp(at))
	if err != nil {
		return core.OwnerName{}, fmt.Errorf("set owner name: %w", err)
	}
	return core.OwnerName{Name: name, UpdatedAt: at}, nil
}

// OwnerName reads what Acorn calls the owner; the zero value when no name was
// ever saved.
func (s *Store) OwnerName(ctx context.Context) (core.OwnerName, error) {
	var (
		owner core.OwnerName
		at    string
	)
	err := s.db.QueryRowContext(ctx, `SELECT name, updated_at FROM owner WHERE id = 1`).Scan(&owner.Name, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return core.OwnerName{}, nil
	}
	if err != nil {
		return core.OwnerName{}, fmt.Errorf("read owner name: %w", err)
	}
	if owner.UpdatedAt, err = parseTimestamp(fixedTimestampLayout, at, "owner.updated_at"); err != nil {
		return core.OwnerName{}, err
	}
	return owner, nil
}
