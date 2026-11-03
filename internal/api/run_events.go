package api

import (
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// RunEvent is the client-visible live event envelope used by /v1 run detail and SSE.
type RunEvent struct {
	EventID string    `json:"event_id"`
	RunID   string    `json:"run_id"`
	Seq     int64     `json:"seq"`
	TS      time.Time `json:"ts"`
	Type    string    `json:"type"`
	Data    any       `json:"data"`
}

// RunEventBatch is a live event page plus the persisted cursor scanned to build it.
type RunEventBatch struct {
	Events    []RunEvent
	CursorSeq int64
}

// RunEventDetail aggregates client-visible live events for a run detail view.
type RunEventDetail struct {
	Events []RunEvent
}

// --- Event payload data types ---

type RunStartedData struct {
	Input string `json:"input,omitempty"`
}

type AssistantDeltaData struct {
	AssistantDelta map[string]any `json:"assistant_delta"`
}

// RunCompletedData and RunFailedData name, as NextRunID, the run that
// started when the run ended on owner messages that waited for it.
type RunCompletedData struct {
	Message   map[string]any `json:"message,omitempty"`
	NextRunID string         `json:"next_run_id,omitempty"`
}

type RunFailedData struct {
	Error     string `json:"error,omitempty"`
	NextRunID string `json:"next_run_id,omitempty"`
}

// RunInputData is an owner message the run took while it ran: QueuedID is
// the message as it waited, MessageID as it now stands in the thread.
type RunInputData struct {
	QueuedID  string `json:"queued_id"`
	MessageID string `json:"message_id"`
	Text      string `json:"text"`
}

type RunInterruptedData struct {
	Interrupt map[string]any `json:"interrupt,omitempty"`
}

type RunResumeRequestedData struct {
	Targets map[string]any `json:"targets,omitempty"`
}

type ElicitationData struct {
	ActionID        string `json:"action_id"`
	Message         string `json:"message,omitempty"`
	RequestedSchema any    `json:"requested_schema,omitempty"`
}

type OperatorQuestionData struct {
	ActionID         string                     `json:"action_id"`
	Question         string                     `json:"question,omitempty"`
	Options          []core.PendingActionOption `json:"options,omitempty"`
	AllowFreeform    bool                       `json:"allow_freeform,omitempty"`
	Decision         string                     `json:"decision,omitempty"`
	SelectedOptionID string                     `json:"selected_option_id,omitempty"`
	Answer           string                     `json:"answer,omitempty"`
}

// ToolCallData carries tool.call.started, tool.call.succeeded and
// tool.call.failed: which tool, a one-line summary of what it was asked to
// do, and the error of a failed call. Tool output stays on the server.
type ToolCallData struct {
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
	Summary    string `json:"summary,omitempty"`
	Error      string `json:"error,omitempty"`
}

// SubRunData carries sub_run.updated and lists a reply's delegated tasks:
// the latest attempt at a task the run handed a sub-agent, where it stands
// and, once it reported, what it handed back, clipped.
type SubRunData struct {
	Task      int    `json:"task"`
	Attempt   int    `json:"attempt"`
	State     string `json:"state"`
	Objective string `json:"objective"`
	Summary   string `json:"summary,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ToolApprovalData carries a tool_approval.pending (tool name and arguments)
// or tool_approval.decided (decision) event.
type ToolApprovalData struct {
	ActionID  string `json:"action_id"`
	ToolName  string `json:"tool_name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Decision  string `json:"decision,omitempty"`
}
