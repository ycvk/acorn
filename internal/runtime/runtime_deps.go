package runtime

import (
	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/model"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/workingstate"
	"github.com/ycvk/acorn/internal/workspace"
)

type RuntimeDeps struct {
	Config            *config.Config
	Store             RunnerFactoryStore
	Loader            *skills.Loader
	DecisionProfiles  *decision.ProfileService
	CheckpointService *workingstate.Service
	SessionSummarySvc *model.SessionSummaryService
	MemoryModule      memorymodule.Service
	ContextPlane      contextplane.ContextPlane
	Orchestration     orchestrationPlane
	MCPPendingActions mcpprovider.PendingActionStore
	Workspace         *workspace.Workspace
	ArtifactService   *store.ArtifactService
	ExtraLocalTools   []einotool.BaseTool
	Handlers          []adk.ChatModelAgentMiddleware
}

func (d RuntimeDeps) CloneForWorkspace(ws *workspace.Workspace) RuntimeDeps {
	clone := d
	clone.Workspace = ws
	clone.DecisionProfiles = decision.NewProfileService(ws.Root())
	return clone
}
