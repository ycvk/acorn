package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// threadViewsTable is shared with the migration that adds it.
const threadViewsTable = `CREATE TABLE IF NOT EXISTS thread_views (
    view_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    title TEXT NOT NULL,
    catalog TEXT NOT NULL,
    components_json TEXT NOT NULL,
    data_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_thread_views_session ON thread_views(session_id, created_at);`

const threadViewColumns = `view_id, session_id, run_id, title, catalog, components_json, data_json, created_at`

// SaveThreadView stores a view a run showed in a thread.
func (s *Store) SaveThreadView(ctx context.Context, in core.ThreadViewWrite) (core.ThreadView, error) {
	switch {
	case strings.TrimSpace(in.RunID) == "":
		return core.ThreadView{}, errors.New("a thread view needs its run_id")
	case strings.TrimSpace(in.Title) == "" || strings.TrimSpace(in.Catalog) == "":
		return core.ThreadView{}, errors.New("a thread view needs its title and catalog")
	case len(in.Components) == 0 || len(in.Data) == 0:
		return core.ThreadView{}, errors.New("a thread view needs its components and data")
	}
	var sessions int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE session_id = ?`, in.SessionID).Scan(&sessions); err != nil {
		return core.ThreadView{}, fmt.Errorf("check thread %s: %w", in.SessionID, err)
	}
	if sessions == 0 {
		return core.ThreadView{}, fmt.Errorf("%w: %s", core.ErrSessionNotFound, in.SessionID)
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return core.ThreadView{}, fmt.Errorf("thread view id: %w", err)
	}
	view := core.ThreadView{
		ID:         "view_" + hex.EncodeToString(raw[:]),
		SessionID:  in.SessionID,
		RunID:      strings.TrimSpace(in.RunID),
		Title:      in.Title,
		Catalog:    in.Catalog,
		Components: in.Components,
		Data:       in.Data,
		CreatedAt:  time.Now().UTC(),
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO thread_views (`+threadViewColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		view.ID, view.SessionID, view.RunID, view.Title, view.Catalog, string(view.Components), string(view.Data), formatTimestamp(view.CreatedAt)); err != nil {
		return core.ThreadView{}, fmt.Errorf("save thread view: %w", err)
	}
	return view, nil
}

// ThreadViews lists a thread's views, oldest first.
func (s *Store) ThreadViews(ctx context.Context, sessionID string) ([]core.ThreadView, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+threadViewColumns+` FROM thread_views WHERE session_id = ? ORDER BY created_at, view_id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list thread views: %w", err)
	}
	views := []core.ThreadView{}
	err = scanRows(rows, func(scan func(...any) error) error {
		var view core.ThreadView
		var components, data, created string
		if err := scan(&view.ID, &view.SessionID, &view.RunID, &view.Title, &view.Catalog, &components, &data, &created); err != nil {
			return err
		}
		at, err := parseTimestamp(fixedTimestampLayout, created, "thread_views.created_at")
		if err != nil {
			return err
		}
		view.Components, view.Data, view.CreatedAt = []byte(components), []byte(data), at
		views = append(views, view)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list thread views: %w", err)
	}
	return views, nil
}

// purgeThreadViews deletes a thread's views.
func purgeThreadViews(ctx context.Context, tx *sql.Tx, sessionID string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM thread_views WHERE session_id = ?`, sessionID)
	return err
}

// addThreadViews (18 to 19) adds the views runs show in conversations.
func addThreadViews(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, threadViewsTable)
	return err
}
