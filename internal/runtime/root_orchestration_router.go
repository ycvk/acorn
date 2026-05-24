package runtime

import (
	"strings"

	"github.com/ycvk/acorn/internal/orchestrationmode"
)

func resolveRootOrchestrationMode(req ExecuteRequest) orchestrationmode.Mode {
	mode := orchestrationmode.Normalize(req.OrchestrationMode)
	if req.OrchestrationMode != "" {
		return mode
	}
	if strings.TrimSpace(req.ParentRunID) != "" {
		return orchestrationmode.SingleAgent
	}
	if strings.TrimSpace(req.SkillID) != "" {
		return orchestrationmode.PlanExecute
	}
	return orchestrationmode.DirectResponse
}
