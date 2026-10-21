package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
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

// threadViewActionsTable is shared with the migration that adds it. The
// primary key lets each action of a view be sent once.
const threadViewActionsTable = `CREATE TABLE IF NOT EXISTS thread_view_actions (
    view_id TEXT NOT NULL,
    action_key TEXT NOT NULL,
    session_id TEXT NOT NULL,
    component_id TEXT NOT NULL,
    scope_json TEXT NOT NULL,
    name TEXT NOT NULL,
    selected_json TEXT NOT NULL,
    context_json TEXT NOT NULL,
    message_id INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (view_id, action_key)
);
CREATE INDEX IF NOT EXISTS idx_thread_view_actions_session ON thread_view_actions(session_id, created_at);`

const threadViewColumns = `view_id, session_id, run_id, title, catalog, components_json, data_json, created_at`

const threadViewActionColumns = `view_id, action_key, session_id, component_id, scope_json, name, selected_json, context_json, message_id, created_at`

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

// UpdateThreadView replaces the title, components and data of a view in the
// thread. The actions the owner already sent stay.
func (s *Store) UpdateThreadView(ctx context.Context, in core.ThreadViewUpdate) (core.ThreadView, error) {
	if strings.TrimSpace(in.Title) == "" || len(in.Components) == 0 || len(in.Data) == 0 {
		return core.ThreadView{}, errors.New("a thread view update needs a title, components and data")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE thread_views SET title = ?, components_json = ?, data_json = ? WHERE view_id = ? AND session_id = ?`,
		in.Title, string(in.Components), string(in.Data), in.ViewID, in.SessionID)
	if err != nil {
		return core.ThreadView{}, fmt.Errorf("update thread view %s: %w", in.ViewID, err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return core.ThreadView{}, fmt.Errorf("update thread view %s: %w", in.ViewID, err)
	}
	if updated == 0 {
		return core.ThreadView{}, fmt.Errorf("%w: %s in thread %s", core.ErrThreadViewNotFound, in.ViewID, in.SessionID)
	}
	return s.ThreadView(ctx, in.SessionID, in.ViewID)
}

// ThreadView loads a view of the thread with the actions sent from it.
func (s *Store) ThreadView(ctx context.Context, sessionID, viewID string) (core.ThreadView, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+threadViewColumns+` FROM thread_views WHERE view_id = ? AND session_id = ?`, viewID, sessionID)
	view, err := scanThreadView(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return core.ThreadView{}, fmt.Errorf("%w: %s in thread %s", core.ErrThreadViewNotFound, viewID, sessionID)
	}
	if err != nil {
		return core.ThreadView{}, fmt.Errorf("load thread view %s: %w", viewID, err)
	}
	actions, err := s.ViewActions(ctx, sessionID)
	if err != nil {
		return core.ThreadView{}, err
	}
	for _, action := range actions {
		if action.ViewID == view.ID {
			view.Actions = append(view.Actions, action)
		}
	}
	return view, nil
}

// ThreadViews lists a thread's views, oldest first, each with the actions
// sent from it.
func (s *Store) ThreadViews(ctx context.Context, sessionID string) ([]core.ThreadView, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+threadViewColumns+` FROM thread_views WHERE session_id = ? ORDER BY created_at, view_id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list thread views: %w", err)
	}
	views := []core.ThreadView{}
	err = scanRows(rows, func(scan func(...any) error) error {
		view, err := scanThreadView(scan)
		if err != nil {
			return err
		}
		views = append(views, view)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list thread views: %w", err)
	}
	actions, err := s.ViewActions(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	byView := map[string][]core.ViewAction{}
	for _, action := range actions {
		byView[action.ViewID] = append(byView[action.ViewID], action)
	}
	for i := range views {
		views[i].Actions = byView[views[i].ID]
	}
	return views, nil
}

func scanThreadView(scan func(...any) error) (core.ThreadView, error) {
	var view core.ThreadView
	var components, data, created string
	if err := scan(&view.ID, &view.SessionID, &view.RunID, &view.Title, &view.Catalog, &components, &data, &created); err != nil {
		return core.ThreadView{}, err
	}
	at, err := parseTimestamp(fixedTimestampLayout, created, "thread_views.created_at")
	if err != nil {
		return core.ThreadView{}, err
	}
	view.Components, view.Data, view.CreatedAt = []byte(components), []byte(data), at
	return view, nil
}

// SendViewAction records an action of a view together with the owner's
// message saying its text, in one transaction. An action already sent fails
// with ErrViewActionTaken and adds no message.
func (s *Store) SendViewAction(ctx context.Context, in core.ViewActionWrite) (core.SessionMessageRecord, core.ViewAction, error) {
	switch {
	case strings.TrimSpace(in.Text) == "":
		return core.SessionMessageRecord{}, core.ViewAction{}, errors.New("a view action needs the text of the owner's message")
	case in.Key == "" || in.ComponentID == "" || in.Name == "":
		return core.SessionMessageRecord{}, core.ViewAction{}, errors.New("a view action needs its key, component and name")
	}
	scope, err := json.Marshal(append([]int{}, in.Scope...))
	if err != nil {
		return core.SessionMessageRecord{}, core.ViewAction{}, fmt.Errorf("view action scope: %w", err)
	}
	selected, err := json.Marshal(append([]core.ViewOption{}, in.Selected...))
	if err != nil {
		return core.SessionMessageRecord{}, core.ViewAction{}, fmt.Errorf("view action options: %w", err)
	}
	carried := in.Context
	if len(carried) == 0 {
		carried = json.RawMessage(`{}`)
	}
	now := time.Now().UTC()
	message := core.SessionMessageRecord{SessionID: in.SessionID, Role: "user", Content: strings.TrimSpace(in.Text), CreatedAt: now}
	err = s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		var views int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM thread_views WHERE view_id = ? AND session_id = ?`, in.ViewID, in.SessionID).Scan(&views); err != nil {
			return err
		}
		if views == 0 {
			return fmt.Errorf("%w: %s in thread %s", core.ErrThreadViewNotFound, in.ViewID, in.SessionID)
		}
		var sent int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM thread_view_actions WHERE view_id = ? AND action_key = ?`, in.ViewID, in.Key).Scan(&sent); err != nil {
			return err
		}
		if sent > 0 {
			return fmt.Errorf("%w: %s of %s", core.ErrViewActionTaken, in.Key, in.ViewID)
		}
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(turn_index), 0) + 1 FROM session_messages WHERE session_id = ?`, in.SessionID).Scan(&message.TurnIndex); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO session_messages(session_id, turn_index, role, content, run_id, created_at) VALUES(?, ?, ?, ?, '', ?)`,
			message.SessionID, message.TurnIndex, message.Role, message.Content, formatTimestamp(now))
		if err != nil {
			return err
		}
		if message.ID, err = result.LastInsertId(); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO thread_view_actions (`+threadViewActionColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			in.ViewID, in.Key, in.SessionID, in.ComponentID, string(scope), in.Name, string(selected), string(carried), message.ID, formatTimestamp(now)); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE sessions SET updated_at = ? WHERE session_id = ?`, formatTimestamp(now), in.SessionID)
		return err
	})
	if err != nil {
		return core.SessionMessageRecord{}, core.ViewAction{}, fmt.Errorf("send view action %s: %w", in.Key, err)
	}
	action := core.ViewAction{
		ViewID: in.ViewID, SessionID: in.SessionID, Key: in.Key, ComponentID: in.ComponentID, Scope: append([]int{}, in.Scope...),
		Name: in.Name, Selected: append([]core.ViewOption{}, in.Selected...), Context: carried, MessageID: message.ID, CreatedAt: now,
	}
	return message, action, nil
}

// ViewActions lists the actions sent from a thread's views, oldest first.
func (s *Store) ViewActions(ctx context.Context, sessionID string) ([]core.ViewAction, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+threadViewActionColumns+` FROM thread_view_actions WHERE session_id = ? ORDER BY created_at, message_id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list view actions: %w", err)
	}
	var actions []core.ViewAction
	err = scanRows(rows, func(scan func(...any) error) error {
		var action core.ViewAction
		var scope, selected, carried, created string
		if err := scan(&action.ViewID, &action.Key, &action.SessionID, &action.ComponentID, &scope, &action.Name, &selected, &carried, &action.MessageID, &created); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(scope), &action.Scope); err != nil {
			return fmt.Errorf("thread_view_actions.scope_json: %w", err)
		}
		if err := json.Unmarshal([]byte(selected), &action.Selected); err != nil {
			return fmt.Errorf("thread_view_actions.selected_json: %w", err)
		}
		at, err := parseTimestamp(fixedTimestampLayout, created, "thread_view_actions.created_at")
		if err != nil {
			return err
		}
		action.Context, action.CreatedAt = []byte(carried), at
		actions = append(actions, action)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list view actions: %w", err)
	}
	return actions, nil
}

// purgeThreadViews deletes a thread's views and the actions sent from them.
func purgeThreadViews(ctx context.Context, tx *sql.Tx, sessionID string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM thread_view_actions WHERE session_id = ?`, sessionID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM thread_views WHERE session_id = ?`, sessionID)
	return err
}

// addThreadViews (18 to 19) adds the views runs show in conversations.
func addThreadViews(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, threadViewsTable)
	return err
}

// addViewActions (19 to 20) adds the buttons and choices the owner sends
// from views.
func addViewActions(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, threadViewActionsTable)
	return err
}
