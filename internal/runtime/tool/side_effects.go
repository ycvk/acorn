package tool

import (
	"strings"
)

// SideEffectRef records a durable side effect produced by a tool execution
// (mutation checkpoint, artifact write, operator action, etc.).
type SideEffectRef struct {
	Kind string
	Ref  string
	Path string
}

const (
	SideEffectKindOperatorAction = "operator_action"
	SideEffectKindArtifact       = "artifact"
)

// buildToolResultRef constructs a deterministic reference string for a tool
// result. This replaces the deleted store.BuildToolResultRef helper.
func buildToolResultRef(runID, callID string) string {
	return "tool_result:" + strings.TrimSpace(runID) + ":" + strings.TrimSpace(callID)
}
