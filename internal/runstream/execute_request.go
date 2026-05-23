package runstream

import (
	"github.com/cloudwego/eino/adk"

	"github.com/ycvk/acorn/internal/orchestrationmode"
)

// ExecuteRequest holds all parameters needed to start or resume a single
// execution turn. It is the canonical input DTO for the Executor.
type ExecuteRequest struct {
	RunID             string
	SessionID         string
	TurnIndex         int
	Input             string
	SkillID           string
	AllowedToolNames  []string
	Messages          []adk.Message
	OrchestrationMode orchestrationmode.Mode
	ParentRunID       string
	Depth             int
}
