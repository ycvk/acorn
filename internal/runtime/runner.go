package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	mcpprovider "github.com/ycvk/acorn/internal/mcp"
	"github.com/ycvk/acorn/internal/presence"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tools"
)

type RunnerFactory struct {
	deps RuntimeDeps

	runChatModelBuilder func(context.Context, RunnerBuildRequest) (einomodel.BaseChatModel, error)
	mcpCache            *mcpManagerCache
	toolRegistry        core.ToolRegistry
}

func NewRunnerFactory(cfg *config.Config, store RuntimeStore, opts RunnerFactoryOptions) (*RunnerFactory, error) {
	deps, err := buildRuntimeDeps(cfg, store, opts)
	if err != nil {
		return nil, fmt.Errorf("build runtime deps: %w", err)
	}
	return assembleRunnerFactory(deps), nil
}

func (f *RunnerFactory) New(ctx context.Context, req RunnerBuildRequest) (*ActiveRunner, error) {
	return f.buildRun(ctx, req)
}

// BuildCapabilitySpecs returns the non-MCP tool specs a run can see: the
// registry's native tools plus the per-run toolset (web, memory, skill). MCP
// tools are reported per provider by the capability snapshot.
func (f *RunnerFactory) BuildCapabilitySpecs(ctx context.Context) ([]core.ToolSpec, error) {
	toolset, err := buildToolset(ctx, f.deps, "")
	if err != nil {
		return nil, err
	}
	var specs []core.ToolSpec
	for _, spec := range f.deps.ToolRegistry.Specs() {
		if spec.Kind != core.ToolKindMCP {
			specs = append(specs, spec)
		}
	}
	specs = append(specs, toolset.Catalog().Specs()...)
	for i := range specs {
		specs[i].Tool = nil
		specs[i].Factory = nil
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	if err := toolset.Close(); err != nil {
		return nil, fmt.Errorf("close capability toolset: %w", err)
	}
	return specs, nil
}

func (f *RunnerFactory) Config() *config.Config {
	return f.deps.Config
}

func (f *RunnerFactory) NewChatModel(ctx context.Context) (einomodel.BaseChatModel, error) {
	return newChatModel(ctx, f.deps.Config)
}

func (f *RunnerFactory) buildRunChatModel(ctx context.Context, req RunnerBuildRequest) (einomodel.BaseChatModel, error) {
	if f.runChatModelBuilder != nil {
		return f.runChatModelBuilder(ctx, req)
	}
	return newChatModel(ctx, f.deps.Config)
}

func (r *ActiveRunner) Close() error {
	var closeErr error
	if r.CloseRunTools != nil {
		closeErr = r.CloseRunTools()
		r.CloseRunTools = nil
	}
	return closeErr
}

// Close releases the cached MCP manager.
func (f *RunnerFactory) Close() error {
	return closeMCPCache(f.mcpCache)
}

type localToolset struct {
	specs   []core.ToolSpec
	closers []io.Closer
}

func (f *RunnerFactory) buildRunCapabilityAssembly(ctx context.Context, req RunnerBuildRequest) (*capabilityAssembly, error) {
	if f == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	mcpManager, err := bootstrapRunMCP(ctx, f.deps, f.mcpCache, req)
	if err != nil {
		return nil, err
	}
	capabilities, err := buildRunCapabilities(ctx, f.deps, req.SessionID, req.RunID, mcpManager)
	if err != nil {
		return nil, err
	}
	return &capabilityAssembly{mcpManager: mcpManager, capabilities: capabilities}, nil
}

type capabilityAssembly struct {
	mcpManager   *mcpprovider.Manager
	capabilities *runCapabilities
}

func buildRuntimeDeps(cfg *config.Config, store RuntimeStore, opts RunnerFactoryOptions) (RuntimeDeps, error) {
	if cfg == nil {
		return RuntimeDeps{}, errors.New("config is required")
	}
	if store == nil {
		return RuntimeDeps{}, errors.New("store is required")
	}
	artifactService := opts.ArtifactService
	if opts.ToolRegistry == nil {
		return RuntimeDeps{}, errors.New("tool registry is required")
	}
	if opts.Presence == nil || opts.Clock == nil {
		return RuntimeDeps{}, errors.New("presence store and clock are required")
	}
	location, err := cfg.OwnerLocation()
	if err != nil {
		return RuntimeDeps{}, err
	}
	return RuntimeDeps{
		Config:            cfg,
		Store:             store,
		Loader:            resolveLoader(cfg, opts.Loader),
		MCPPendingActions: opts.MCPPendingActionStore,
		ArtifactService:   artifactService,
		ToolRegistry:      opts.ToolRegistry,
		Presence:          opts.Presence,
		Clock:             opts.Clock,
		Location:          location,
	}, nil
}

func resolveLoader(cfg *config.Config, loader *skills.Loader) *skills.Loader {
	if loader == nil {
		return skills.NewLoader(cfg)
	}
	return loader
}

func assembleRunnerFactory(deps RuntimeDeps) *RunnerFactory {
	return &RunnerFactory{
		deps:         deps,
		mcpCache:     &mcpManagerCache{},
		toolRegistry: deps.ToolRegistry,
	}
}

type runCapabilities struct {
	catalog       *tools.Catalog
	skillSnapshot *skills.Snapshot
	stableSkills  []skills.Spec
	close         func() error
}

func (c *runCapabilities) Close() error {
	if c == nil || c.close == nil {
		return nil
	}
	return c.close()
}

func (f *RunnerFactory) buildRun(ctx context.Context, req RunnerBuildRequest) (active *ActiveRunner, err error) {
	if f == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	var capabilities *runCapabilities
	defer func() {
		if err == nil {
			return
		}
		if capabilities != nil {
			_ = capabilities.Close()
		}
	}()
	chatModel, capabilityAssembly, prereqErr := f.buildRunPrerequisites(ctx, req)
	if prereqErr != nil {
		return nil, prereqErr
	}
	capabilities = capabilityAssembly.capabilities
	active, err = f.newAgentRunner(ctx, req, chatModel, capabilityAssembly)
	return active, err
}

func (f *RunnerFactory) newAgentRunner(ctx context.Context, req RunnerBuildRequest, chatModel einomodel.BaseChatModel, capabilityAssembly *capabilityAssembly) (*ActiveRunner, error) {
	if capabilityAssembly == nil || capabilityAssembly.capabilities == nil {
		return nil, errors.New("run capabilities are required")
	}
	capabilities := capabilityAssembly.capabilities
	persona, err := presence.LoadPersona(f.deps.Config.PersonaPath())
	if err != nil {
		return nil, err
	}
	runner, err := buildAgentRunner(ctx, f.deps, agentRunnerRequest{
		RunID:       req.RunID,
		ChatModel:   chatModel,
		Catalog:     capabilities.catalog,
		Instruction: buildAgentInstruction(persona, skillCatalogBrief(capabilities.skillSnapshot)),
	})
	if err != nil {
		return nil, err
	}
	return &ActiveRunner{
		Mcp:           capabilityAssembly.mcpManager,
		Runner:        runner,
		ChatModel:     chatModel,
		RunID:         req.RunID,
		ToolCatalog:   capabilities.catalog,
		CloseRunTools: capabilities.Close,
	}, nil
}

type RunnerFactoryOptions struct {
	Loader                *skills.Loader
	MCPPendingActionStore core.SessionStore
	ArtifactService       core.ArtifactService
	ToolRegistry          core.ToolRegistry
	Presence              core.PresenceStore
	Clock                 func() time.Time
}

// RunnerBuildRequest holds the parameters for building a new run.
type RunnerBuildRequest struct {
	SessionID string
	RunID     string
	Input     string
	SkillID   string
	Sink      core.StreamSink
}

type ActiveRunner struct {
	Mcp           *mcpprovider.Manager
	Runner        *adk.Runner
	ChatModel     einomodel.BaseChatModel
	RunID         string
	ToolCatalog   *tools.Catalog
	CloseRunTools func() error
}

func (f *RunnerFactory) buildRunPrerequisites(ctx context.Context, req RunnerBuildRequest) (einomodel.BaseChatModel, *capabilityAssembly, error) {
	chatModel, err := f.buildRunChatModel(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	capabilityAssembly, err := f.buildRunCapabilityAssembly(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	return chatModel, capabilityAssembly, nil
}

// operatingRules follow the owner's persona in every instruction. They cover
// how to use the runtime's tools; the persona covers who the agent is.
const operatingRules = `Operating rules:
- The <presence> block at the end of your input is your working memory right now: the time, what woke you, your commitments, your thoughts, what the owner said, their tendencies and your concerns. Items are referenced by #id.
- When the owner tells you something worth remembering, keep it with keep, in their own words. Note your own observations and open questions with think.
- When something should happen later, make a commitment with schedule_wake. You will wake in this conversation with the task as input.
- When you wake for a commitment, do the task, then settle it with done, or schedule a new wake if it is not finished.
- Use settle to renew what still matters, to internalize a lasting preference (tendency) or concern (ruler), and to release what no longer matters.
- Use recall to find past conversations and older memory before saying you do not know.
- When the owner should know something now and may not be looking, use notify_owner. Keep the notification to a short summary; details stay in the conversation.
- Before answering a capability question or saying you cannot do something, inspect the skill catalog and the tools you have. If a relevant skill may exist but the catalog summary is not enough, call skill_list or skill_view. If a capability depends on deferred tools (web_search, web_fetch, browser), call tool_search first.
- Some tools pause for the owner's approval on their phone. Say what you are about to do before calling them.
- Prefer available MCP tools over inventing capabilities, and never claim a tool succeeded when it did not run.`

func buildStableInstruction(base string) string {
	parts := []string{
		strings.TrimSpace(base),
		strings.TrimSpace(operatingRules),
	}
	out := make([]string, 0, len(parts))
	for _, item := range parts {
		if strings.TrimSpace(item) != "" {
			out = append(out, strings.TrimSpace(item))
		}
	}
	return strings.Join(out, "\n\n")
}

func skillEligibilityContextFromCatalog(catalog *tools.Catalog) skills.EligibilityContext {
	if catalog == nil {
		return skills.EligibilityContext{}
	}
	return tools.EligibilityContext(catalog, nil)
}

func loadStableSkillSnapshot(ctx context.Context, loader interface {
	ScanSkills(context.Context) (*skills.ScanResult, error)
}, eligibility skills.EligibilityContext) (*skills.Snapshot, error) {
	if loader == nil {
		return nil, nil
	}
	scan, err := loader.ScanSkills(ctx)
	if err != nil {
		return nil, fmt.Errorf("load skills: %w", err)
	}
	if scan == nil {
		return nil, nil
	}
	snapshot, err := skills.BuildSnapshot(*scan, eligibility)
	if err != nil {
		return nil, fmt.Errorf("build skill snapshot: %w", err)
	}
	copied := skills.CopySnapshot(snapshot)
	return &copied, nil
}

func stableSkillsFromSnapshot(snapshot *skills.Snapshot) []skills.Spec {
	if snapshot == nil || len(snapshot.Skills) == 0 {
		return nil
	}
	items := make([]skills.Spec, 0, len(snapshot.Skills))
	for _, item := range snapshot.Skills {
		items = append(items, skills.CopySpec(item.Spec))
	}
	return items
}
