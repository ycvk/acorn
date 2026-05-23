package runtime

import (
	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/orchestration"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/runtimehistory"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/workingstate"
	"github.com/ycvk/acorn/internal/workspace"
)

// RunnerFactoryOptions holds the optional dependencies for creating a RunnerFactory.
type RunnerFactoryOptions struct {
	Loader                 *skills.Loader
	DecisionProfileService *decision.ProfileService
	ExtraLocalTools        []einotool.BaseTool
	Workspace              *workspace.Workspace
	Handlers               []adk.ChatModelAgentMiddleware
	CheckpointService      *workingstate.Service
	SessionSummaryService  *runtimehistory.SessionSummaryService
	MemoryModule           memorymodule.Service
	ContextPlane           contextplane.ContextPlane
	MCPPendingActionStore  mcpprovider.PendingActionStore
}

// RunnerBuildRequest holds the parameters for building a new run.
type RunnerBuildRequest struct {
	SessionID         string
	RunID             string
	Input             string
	SkillID           string
	AllowedToolNames  []string
	Sink              StreamSink
	ExcludedToolNames []string
	InstructionSuffix string
	OrchestrationMode orchestration.OrchestrationMode
	ParentRunID       string
}

// ActiveRunner represents a fully built and ready-to-execute run.
type ActiveRunner struct {
	Mcp              *mcpprovider.Manager
	Runner           *adk.Runner
	SelectedSkill    *SelectedSkill
	Instruction      string
	ChatModel        einomodel.BaseChatModel
	Factory          *RunnerFactory
	ContextResult    *contextplane.AssembleResult
	ContextSession   contextplane.ContextSession
	RunID            string
	CompressionState any
	ToolCatalog      *tooling.Catalog
	CloseRunTools    func() error
}
