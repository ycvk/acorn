package runtime

import (
	"github.com/ycvk/acorn/internal/events"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
)

func resolveRootOrchestrationMode(req runtimeapi.ExecuteRequest) events.OrchestrationMode {
	mode := events.OrchestrationMode(req.OrchestrationMode).Normalize()
	if req.OrchestrationMode != "" {
		return mode
	}
	return events.ModeDirectResponse
}
