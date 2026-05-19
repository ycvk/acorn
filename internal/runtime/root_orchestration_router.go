package runtime

import (
	"strings"

	"github.com/ycvk/acorn/internal/orchestration"
)

func resolveRootOrchestrationMode(req ExecuteRequest) orchestration.OrchestrationMode {
	mode := orchestration.NormalizeOrchestrationMode(req.OrchestrationMode)
	if req.OrchestrationMode != "" {
		return mode
	}
	if strings.TrimSpace(req.ParentRunID) != "" {
		return orchestration.OrchestrationModeSingleAgent
	}
	if strings.TrimSpace(req.SkillID) != "" {
		return orchestration.OrchestrationModePlanExecute
	}
	return orchestration.OrchestrationModeDirectResponse
}
