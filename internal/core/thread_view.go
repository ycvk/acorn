package core

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrThreadViewNotFound = errors.New("thread view not found")
	// ErrViewActionTaken refuses an action of a view that was already sent.
	ErrViewActionTaken = errors.New("view action already sent")
)

// ThreadView is a view the agent showed the owner during a run: components
// from Acorn's view catalog and the data they bind to, drawn by the app under
// the run's reply. A later run in the thread may replace its content.
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
	// Actions are the buttons and choices the owner already sent, oldest first.
	Actions []ViewAction
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

// ThreadViewUpdate is checked content replacing a view's.
type ThreadViewUpdate struct {
	SessionID  string
	ViewID     string
	Title      string
	Components json.RawMessage
	Data       json.RawMessage
}

// ViewOption is a choice the owner picked in a view.
type ViewOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// ViewAction is the owner's answer through a view: a button they pressed or
// the options they chose, sent as their message MessageID.
type ViewAction struct {
	ViewID      string
	SessionID   string
	Key         string
	ComponentID string
	// Scope holds the List item indices the component was drawn for, outermost first.
	Scope    []int
	Name     string
	Selected []ViewOption
	// Context is the JSON object the action carried.
	Context   json.RawMessage
	MessageID int64
	CreatedAt time.Time
}

// ViewActionWrite is a resolved action to record with the owner's message.
type ViewActionWrite struct {
	ViewID      string
	SessionID   string
	Key         string
	ComponentID string
	Scope       []int
	Name        string
	// Text is the owner's message.
	Text     string
	Selected []ViewOption
	Context  json.RawMessage
}

// ViewShownData carries a view.shown or view.updated event: the view as it
// now is.
type ViewShownData struct {
	View ViewData `json:"view"`
}

// ViewData is a view as the client receives it.
type ViewData struct {
	ID         string           `json:"id"`
	Title      string           `json:"title"`
	Catalog    string           `json:"catalog"`
	Components json.RawMessage  `json:"components"`
	Data       json.RawMessage  `json:"data"`
	CreatedAt  time.Time        `json:"created_at"`
	Actions    []ViewActionData `json:"actions,omitempty"`
}

// ViewActionData is an action the owner sent, as the client receives it.
type ViewActionData struct {
	ComponentID string    `json:"component_id"`
	Scope       []int     `json:"scope"`
	Selected    []string  `json:"selected,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// ViewDataOf is the client form of view.
func ViewDataOf(view ThreadView) ViewData {
	data := ViewData{ID: view.ID, Title: view.Title, Catalog: view.Catalog, Components: view.Components, Data: view.Data, CreatedAt: view.CreatedAt}
	for _, action := range view.Actions {
		sent := ViewActionData{ComponentID: action.ComponentID, Scope: append([]int{}, action.Scope...), CreatedAt: action.CreatedAt}
		for _, option := range action.Selected {
			sent.Selected = append(sent.Selected, option.ID)
		}
		data.Actions = append(data.Actions, sent)
	}
	return data
}
