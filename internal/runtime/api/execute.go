package api

import (
	"github.com/cloudwego/eino/adk"

	"github.com/ycvk/acorn/internal/orchestrationmode"
)

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
