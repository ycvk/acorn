package core

import (
	"context"
	"encoding/json"
	"time"
)

// ThreadView is a view the agent showed the owner during a run: components
// from Acorn's view catalog and the data they bind to, drawn by the app under
// the run's reply. Its content never changes.
type ThreadView struct {
	ID        string
	SessionID string
	RunID     string
	Title     string
	// Catalog names the catalog version the components were checked against.
	Catalog string
	// Components is the JSON array of components, one of them with id root.
	Components json.RawMessage
	// Data is the JSON object the components' paths point into.
	Data      json.RawMessage
	CreatedAt time.Time
}

// ThreadViewWrite is a checked view to store.
type ThreadViewWrite struct {
	SessionID  string
	RunID      string
	Title      string
	Catalog    string
	Components json.RawMessage
	Data       json.RawMessage
}

// ThreadViews keeps the views of conversations. Deleting a thread deletes
// its views.
type ThreadViews interface {
	SaveThreadView(ctx context.Context, in ThreadViewWrite) (ThreadView, error)
	// ThreadViews lists a thread's views, oldest first.
	ThreadViews(ctx context.Context, sessionID string) ([]ThreadView, error)
}

// ViewShownData carries a view.shown event: the view a run just showed.
type ViewShownData struct {
	View ViewData `json:"view"`
}

// ViewData is a view as the client receives it.
type ViewData struct {
	ID         string          `json:"id"`
	Title      string          `json:"title"`
	Catalog    string          `json:"catalog"`
	Components json.RawMessage `json:"components"`
	Data       json.RawMessage `json:"data"`
	CreatedAt  time.Time       `json:"created_at"`
}

// ViewDataOf is the client form of view.
func ViewDataOf(view ThreadView) ViewData {
	return ViewData{ID: view.ID, Title: view.Title, Catalog: view.Catalog, Components: view.Components, Data: view.Data, CreatedAt: view.CreatedAt}
}
