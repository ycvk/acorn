package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/core"
)

// WritePersona stores a new persona revision on top of the current one.
func (s *Store) WritePersona(ctx context.Context, w core.PersonaWrite) (core.PersonaRevision, error) {
	if strings.TrimSpace(w.Content) == "" || w.At.IsZero() {
		return core.PersonaRevision{}, errors.New("write persona: content and at are required")
	}
	switch w.Author {
	case core.PersonaAuthorOwner, core.PersonaAuthorRelease:
	default:
		return core.PersonaRevision{}, fmt.Errorf("write persona: unknown author %q", w.Author)
	}
	sum := sha256.Sum256([]byte(w.Content))
	revision := core.PersonaRevision{Content: w.Content, SHA256: hex.EncodeToString(sum[:]), Author: w.Author, DeviceID: w.DeviceID, At: w.At}
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		var current int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision), 0) FROM persona_revisions`).Scan(&current); err != nil {
			return err
		}
		if current != w.BaseRevision {
			return fmt.Errorf("%w: the persona is at revision %d, not %d", core.ErrPersonaConflict, current, w.BaseRevision)
		}
		revision.Revision = current + 1
		_, err := tx.ExecContext(ctx, `INSERT INTO persona_revisions (revision, content, content_sha256, author, device_id, at) VALUES (?, ?, ?, ?, ?, ?)`,
			revision.Revision, revision.Content, revision.SHA256, string(revision.Author), revision.DeviceID, formatTimestamp(revision.At))
		return err
	})
	if err != nil {
		return core.PersonaRevision{}, fmt.Errorf("write persona: %w", err)
	}
	return revision, nil
}

func (s *Store) Persona(ctx context.Context) (core.PersonaRevision, error) {
	revisions, err := queryPersonaRevisions(ctx, s.db, `content`, ` ORDER BY revision DESC LIMIT 1`)
	if err != nil {
		return core.PersonaRevision{}, fmt.Errorf("read persona: %w", err)
	}
	if len(revisions) == 0 {
		return core.PersonaRevision{}, fmt.Errorf("%w: no persona has been saved", core.ErrPersonaNotFound)
	}
	return revisions[0], nil
}

func (s *Store) PersonaRevisions(ctx context.Context) ([]core.PersonaRevision, error) {
	revisions, err := queryPersonaRevisions(ctx, s.db, `''`, ` ORDER BY revision DESC`)
	if err != nil {
		return nil, fmt.Errorf("list persona revisions: %w", err)
	}
	return revisions, nil
}

func (s *Store) PersonaRevision(ctx context.Context, revision int64) (core.PersonaRevision, error) {
	revisions, err := queryPersonaRevisions(ctx, s.db, `content`, ` WHERE revision = ?`, revision)
	if err != nil {
		return core.PersonaRevision{}, fmt.Errorf("read persona revision %d: %w", revision, err)
	}
	if len(revisions) == 0 {
		return core.PersonaRevision{}, fmt.Errorf("%w: revision %d", core.ErrPersonaNotFound, revision)
	}
	return revisions[0], nil
}

// queryPersonaRevisions reads revisions with content as the selected content
// expression; tail filters and orders them.
func queryPersonaRevisions(ctx context.Context, q memorySQL, content, tail string, args ...any) ([]core.PersonaRevision, error) {
	rows, err := q.QueryContext(ctx, `SELECT revision, `+content+`, content_sha256, author, device_id, at FROM persona_revisions`+tail, args...)
	if err != nil {
		return nil, err
	}
	revisions := []core.PersonaRevision{}
	err = scanRows(rows, func(scan func(...any) error) error {
		var (
			revision   core.PersonaRevision
			author, at string
		)
		if err := scan(&revision.Revision, &revision.Content, &revision.SHA256, &author, &revision.DeviceID, &at); err != nil {
			return err
		}
		revision.Author = core.PersonaAuthor(author)
		var err error
		if revision.At, err = parseTimestamp(fixedTimestampLayout, at, "persona_revisions.at"); err != nil {
			return err
		}
		revisions = append(revisions, revision)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan persona revisions: %w", err)
	}
	return revisions, nil
}
