package tools

import (
	"context"
	"errors"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
)

// builtinToolOrder is the canonical list of dynamically-registered built-in
// tools. It is the
// single source of truth for built-in tool identity: BuiltinToolNames and the
// runtime spec resolver (tool.RuntimeToolSpec via BuiltinToolSpec) both derive
// from it, so adding a built-in tool means editing this one place.
//
// Static local tools (artifact_*, ask_operator, keep, recall, ...) are declared
// separately in localToolNames/configuredLocalSpec.
var builtinToolOrder = []string{
	"ask_operator",
}

// builtinToolContract returns the contract template (without Source/Profiles,
// which are caller-supplied) for a built-in tool. ok is false for any name that
// is not a built-in tool (e.g. MCP tools), which callers resolve elsewhere.
func builtinToolContract(name string) (core.ToolContract, bool) {
	c := core.ToolContract{
		Name:    name,
		Loading: core.EagerLoadingPolicy(),
	}
	switch name {
	case "ask_operator":
		c.Kind = core.ToolKindNative
		c.Category = core.ToolCategoryIntegration
	default:
		return core.ToolContract{}, false
	}
	return c, true
}

// BuiltinToolSpec resolves the full contract for a built-in tool, applying the
// caller-supplied source to the canonical contract template. It returns ok=false
// for names that are not built-in toolset.
func BuiltinToolSpec(name, source string) (core.ToolContract, bool) {
	c, ok := builtinToolContract(name)
	if !ok {
		return core.ToolContract{}, false
	}
	c.Source = source
	return c, true
}

// BuiltinToolNames returns the built-in tools, which are all eager-loaded and
// therefore always eligible for skill matching.
func BuiltinToolNames() []string {
	return append([]string(nil), builtinToolOrder...)
}

// NativeToolDeps are the dependencies of the eager native tools. Every field
// is required: these tools are part of every run, so a missing dependency is a
// wiring bug and RegisterNativeTools fails on it.
type NativeToolDeps struct {
	ArtifactService core.ArtifactService
	ArtifactContext core.ToolCallContextBridge
	OperatorStore   OperatorQuestionStore
	OperatorContext core.ToolCallContextBridge
	Presence        PresenceToolDeps
	Notify          NotifyToolDeps
	Knowledge       KnowledgeToolDeps
	Watch           WatchToolDeps
}

type nativeToolDep struct {
	field   string
	present bool
}

// nativeToolFactory checks the dependencies a single eager native tool needs
// and returns the factory that builds it.
func nativeToolFactory(name string, deps NativeToolDeps) (core.ToolFactory, error) {
	artifactDeps := []nativeToolDep{
		{"ArtifactService", deps.ArtifactService != nil},
		{"ArtifactContext", deps.ArtifactContext != nil},
	}
	presenceDeps := []nativeToolDep{
		{"Presence.Store", deps.Presence.Store != nil},
		{"Presence.Context", deps.Presence.Context != nil},
		{"Presence.Clock", deps.Presence.Clock != nil},
		{"Presence.Location", deps.Presence.Location != nil},
	}
	knowledgeDeps := []nativeToolDep{
		{"Knowledge.Vault", deps.Knowledge.Vault != nil},
		{"Knowledge.Context", deps.Knowledge.Context != nil},
		{"Knowledge.Location", deps.Knowledge.Location != nil},
	}
	watchDeps := []nativeToolDep{
		{"Watch.Store", deps.Watch.Store != nil},
		{"Watch.Checker", deps.Watch.Checker != nil},
		{"Watch.Context", deps.Watch.Context != nil},
		{"Watch.Clock", deps.Watch.Clock != nil},
		{"Watch.Location", deps.Watch.Location != nil},
	}
	var needs []nativeToolDep
	var build func() (einotool.BaseTool, error)
	switch name {
	case "artifact_write":
		needs = artifactDeps
		build = func() (einotool.BaseTool, error) {
			return buildArtifactWriteTool(deps.ArtifactService, deps.ArtifactContext)
		}
	case "artifact_read":
		needs = artifactDeps
		build = func() (einotool.BaseTool, error) { return buildArtifactReadTool(deps.ArtifactService) }
	case "artifact_list":
		needs = artifactDeps
		build = func() (einotool.BaseTool, error) {
			return buildArtifactListTool(deps.ArtifactService, deps.ArtifactContext)
		}
	case "ask_operator":
		needs = []nativeToolDep{
			{"OperatorStore", deps.OperatorStore != nil},
			{"OperatorContext", deps.OperatorContext != nil},
		}
		build = func() (einotool.BaseTool, error) {
			return buildAskOperatorTool(deps.OperatorStore, deps.OperatorContext)
		}
	case "keep":
		needs = presenceDeps
		build = func() (einotool.BaseTool, error) {
			return buildMemoryWriteTool("keep", "Keep something the owner said, in their own words, so it stays in your working memory.", core.MemorySaid, deps.Presence)
		}
	case "think":
		needs = presenceDeps
		build = func() (einotool.BaseTool, error) {
			return buildMemoryWriteTool("think", "Note a thought of your own: something you noticed, suspect or want to follow up on.", core.MemoryThought, deps.Presence)
		}
	case "schedule_wake":
		needs = presenceDeps
		build = func() (einotool.BaseTool, error) { return buildScheduleWakeTool(deps.Presence) }
	case "settle":
		needs = presenceDeps
		build = func() (einotool.BaseTool, error) { return buildSettleTool(deps.Presence) }
	case "recall":
		needs = presenceDeps
		build = func() (einotool.BaseTool, error) { return buildRecallTool(deps.Presence) }
	case "notify_owner":
		needs = []nativeToolDep{
			{"Notify.Notifier", deps.Notify.Notifier != nil},
			{"Notify.Context", deps.Notify.Context != nil},
			{"Notify.Location", deps.Notify.Location != nil},
		}
		build = func() (einotool.BaseTool, error) { return buildNotifyOwnerTool(deps.Notify) }
	case "knowledge_write":
		needs = knowledgeDeps
		build = func() (einotool.BaseTool, error) { return buildKnowledgeWriteTool(deps.Knowledge) }
	case "knowledge_edit":
		needs = knowledgeDeps
		build = func() (einotool.BaseTool, error) { return buildKnowledgeEditTool(deps.Knowledge) }
	case "knowledge_read":
		needs = knowledgeDeps
		build = func() (einotool.BaseTool, error) { return buildKnowledgeReadTool(deps.Knowledge) }
	case "knowledge_search":
		needs = knowledgeDeps
		build = func() (einotool.BaseTool, error) { return buildKnowledgeSearchTool(deps.Knowledge) }
	case "knowledge_list":
		needs = knowledgeDeps
		build = func() (einotool.BaseTool, error) { return buildKnowledgeListTool(deps.Knowledge) }
	case "watch_create":
		needs = watchDeps
		build = func() (einotool.BaseTool, error) { return buildWatchCreateTool(deps.Watch) }
	case "watch_update":
		needs = watchDeps
		build = func() (einotool.BaseTool, error) { return buildWatchUpdateTool(deps.Watch) }
	case "watch_list":
		needs = watchDeps
		build = func() (einotool.BaseTool, error) { return buildWatchListTool(deps.Watch) }
	default:
		return nil, fmt.Errorf("no factory for native tool %q", name)
	}
	for _, dep := range needs {
		if !dep.present {
			return nil, fmt.Errorf("native tool %q requires NativeToolDeps.%s", name, dep.field)
		}
	}
	return func(context.Context, core.RunContext) (einotool.BaseTool, error) {
		return build()
	}, nil
}

// RegisterNativeTools registers every eager-loaded static local tool declared by
// localToolNames/configuredLocalSpec into the core.ToolRegistry. Deferred-loaded
// tools (web_fetch, web_search, browser) are excluded: they depend on per-run
// services (web access, browser) constructed at buildRun time, so they cannot
// be resolved at wire time. They are contributed by the runtime toolset
// catalog instead (see BuildWebToolSpecs).
func RegisterNativeTools(registry core.ToolRegistry, deps NativeToolDeps) error {
	if registry == nil {
		return fmt.Errorf("RegisterNativeTools: registry is nil")
	}
	// localToolNames is the canonical name list in canonical order; reusing it
	// keeps the registry from drifting from configuredLocalSpec.
	for _, name := range localToolNames {
		spec := configuredLocalSpec(name)
		if spec.Loading.Mode == core.ToolLoadingModeDeferred {
			continue
		}
		if name == "notify_owner" && deps.Notify.Notifier == nil {
			if deps.Notify.DisabledReason == "" {
				return errors.New("RegisterNativeTools: notify_owner needs NativeToolDeps.Notify.Notifier or a DisabledReason")
			}
			spec.Health = core.ToolHealth{State: core.HealthStateDisabled, Reason: deps.Notify.DisabledReason}
			if err := registry.Register(spec); err != nil {
				return fmt.Errorf("RegisterNativeTools: register %q: %w", name, err)
			}
			continue
		}
		factory, err := nativeToolFactory(name, deps)
		if err != nil {
			return fmt.Errorf("RegisterNativeTools: %w", err)
		}
		spec.Factory = factory
		if err := registry.Register(spec); err != nil {
			return fmt.Errorf("RegisterNativeTools: register %q: %w", name, err)
		}
	}
	return nil
}
