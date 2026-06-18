// Package sessionview owns the pure UI projection of session messages.
//
// It converts persisted runtime artifacts (events, tool results, plans,
// pending actions, run records) into the message-part shapes consumed by the
// mobile/openapi remote client. The package is free of persistence concerns:
// callers load the underlying records and feed them in, and sessionview returns
// the projected content. The JSON tags on the wire types below are part of the
// remote client contract and must not change.
package sessionview

// MessagePart is one renderable fragment of a session message. Its JSON shape
// is the remote client wire contract.
type MessagePart struct {
	Kind             string           `json:"kind"`
	Text             string           `json:"text,omitempty"`
	Reasoning        string           `json:"reasoning,omitempty"`
	Status           string           `json:"status,omitempty"`
	Title            string           `json:"title,omitempty"`
	Summary          string           `json:"summary,omitempty"`
	Changed          []string         `json:"changed,omitempty"`
	Verified         []string         `json:"verified,omitempty"`
	Risks            []string         `json:"risks,omitempty"`
	Items            []DisclosureItem `json:"items,omitempty"`
	DetailRunID      string           `json:"detail_run_id,omitempty"`
	RunID            string           `json:"run_id,omitempty"`
	Label            string           `json:"label,omitempty"`
	DecisionID       string           `json:"decision_id,omitempty"`
	Question         string           `json:"question,omitempty"`
	SelectedOptionID string           `json:"selected_option_id,omitempty"`
	Answer           string           `json:"answer,omitempty"`
	Options          []DecisionOption `json:"options,omitempty"`
	Action           *MessageAction   `json:"action,omitempty"`
}

// DisclosureItem describes a single disclosure entry (memory or skill usage).
type DisclosureItem struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Detail  string `json:"detail,omitempty"`
	Tone    string `json:"tone"`
	SkillID string `json:"skill_id,omitempty"`
}

// DecisionOption is one selectable answer for a decision message.
type DecisionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// MessageAction is an actionable affordance attached to a message part.
type MessageAction struct {
	Kind  string `json:"kind"`
	RunID string `json:"run_id"`
	Label string `json:"label"`
}
