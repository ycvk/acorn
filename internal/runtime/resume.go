package runtime

import (
	"strings"

	"github.com/ycvk/acorn/internal/events"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
)

func resolveRootOrchestrationMode(req runtimeapi.ExecuteRequest) events.OrchestrationMode {
	mode := events.OrchestrationMode(req.OrchestrationMode).Normalize()
	if req.OrchestrationMode != "" {
		return mode
	}
	if strings.TrimSpace(req.ParentRunID) != "" {
		return events.ModeSingleAgent
	}
	if strings.TrimSpace(req.SkillID) != "" {
		return events.ModePlanExecute
	}
	return events.ModeDirectResponse
}
