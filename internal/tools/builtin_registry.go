package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
)

// NativeToolDeps are the dependencies of the eager native tools. Every field
// is required: these tools are part of every run, so a missing dependency is a
// wiring bug and RegisterNativeTools fails on it.
type NativeToolDeps struct {
	ArtifactService core.ArtifactService
	ArtifactContext core.ToolCallContextBridge
	OperatorStore   OperatorQuestionStore
	OperatorContext core.ToolCallContextBridge
	Presence        PresenceToolDeps
	Goals           GoalToolDeps
	Notify          NotifyToolDeps
	Knowledge       KnowledgeToolDeps
	Skills          SkillToolDeps
	Watch           WatchToolDeps
	Views           ViewToolDeps
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
		{"Presence.Memory", deps.Presence.Memory != nil},
		{"Presence.ForgetBarrier", deps.Presence.ForgetBarrier != nil},
		{"Presence.Context", deps.Presence.Context != nil},
		{"Presence.Clock", deps.Presence.Clock != nil},
		{"Presence.Settings", deps.Presence.Settings != nil},
	}
	knowledgeDeps := []nativeToolDep{
		{"Knowledge.Vault", deps.Knowledge.Vault != nil},
		{"Knowledge.Context", deps.Knowledge.Context != nil},
		{"Knowledge.Settings", deps.Knowledge.Settings != nil},
	}
	skillDeps := []nativeToolDep{
		{"Skills.Library", deps.Skills.Library != nil},
		{"Skills.Context", deps.Skills.Context != nil},
		{"Skills.Settings", deps.Skills.Settings != nil},
	}
	watchDeps := []nativeToolDep{
		{"Watch.Store", deps.Watch.Store != nil},
		{"Watch.Checker", deps.Watch.Checker != nil},
		{"Watch.Context", deps.Watch.Context != nil},
		{"Watch.Clock", deps.Watch.Clock != nil},
		{"Watch.Settings", deps.Watch.Settings != nil},
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
		build = func() (einotool.BaseTool, error) { return buildKeepTool(deps.Presence) }
	case "think":
		needs = presenceDeps
		build = func() (einotool.BaseTool, error) { return buildThinkTool(deps.Presence) }
	case "schedule_wake":
		needs = presenceDeps
		build = func() (einotool.BaseTool, error) { return buildScheduleWakeTool(deps.Presence) }
	case "settle":
		needs = presenceDeps
		build = func() (einotool.BaseTool, error) { return buildSettleTool(deps.Presence) }
	case "goal":
		needs = []nativeToolDep{
			{"Goals.Store", deps.Goals.Store != nil},
			{"Goals.Context", deps.Goals.Context != nil},
			{"Goals.Clock", deps.Goals.Clock != nil},
		}
		build = func() (einotool.BaseTool, error) { return buildGoalTool(deps.Goals) }
	case "recall":
		needs = presenceDeps
		build = func() (einotool.BaseTool, error) { return buildRecallTool(deps.Presence) }
	case "memory_read":
		needs = presenceDeps
		build = func() (einotool.BaseTool, error) { return buildMemoryReadTool(deps.Presence) }
	case "memory_correct":
		needs = presenceDeps
		build = func() (einotool.BaseTool, error) { return buildMemoryCorrectTool(deps.Presence) }
	case "memory_forget":
		needs = presenceDeps
		build = func() (einotool.BaseTool, error) { return buildMemoryForgetTool(deps.Presence) }
	case "concern":
		needs = presenceDeps
		build = func() (einotool.BaseTool, error) { return buildConcernTool(deps.Presence) }
	case "notify_owner":
		needs = []nativeToolDep{
			{"Notify.Notifier", deps.Notify.Notifier != nil},
			{"Notify.Context", deps.Notify.Context != nil},
			{"Notify.Settings", deps.Notify.Settings != nil},
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
	case "skill_write":
		needs = skillDeps
		build = func() (einotool.BaseTool, error) { return buildSkillWriteTool(deps.Skills) }
	case "skill_retire":
		needs = skillDeps
		build = func() (einotool.BaseTool, error) { return buildSkillRetireTool(deps.Skills) }
	case "skill_inspect":
		needs = append(slices.Clone(skillDeps), nativeToolDep{"Skills.Packages", deps.Skills.Packages != nil})
		build = func() (einotool.BaseTool, error) { return buildSkillInspectTool(deps.Skills) }
	case "skill_import":
		needs = skillDeps
		build = func() (einotool.BaseTool, error) { return buildSkillImportTool(deps.Skills) }
	case "watch_create":
		needs = append(slices.Clone(watchDeps), nativeToolDep{"Watch.Triggers", deps.Watch.Triggers != nil})
		build = func() (einotool.BaseTool, error) { return buildWatchCreateTool(deps.Watch) }
	case "watch_update":
		needs = watchDeps
		build = func() (einotool.BaseTool, error) { return buildWatchUpdateTool(deps.Watch) }
	case "watch_list":
		needs = watchDeps
		build = func() (einotool.BaseTool, error) { return buildWatchListTool(deps.Watch) }
	case "news_sections":
		needs = watchDeps
		build = func() (einotool.BaseTool, error) { return buildNewsSectionsTool(deps.Watch) }
	case "news_source":
		needs = watchDeps
		build = func() (einotool.BaseTool, error) { return buildNewsSourceTool(deps.Watch) }
	case "news_search":
		needs = append(slices.Clone(watchDeps), nativeToolDep{"Watch.News", deps.Watch.News != nil})
		build = func() (einotool.BaseTool, error) { return buildNewsSearchTool(deps.Watch) }
	case "feed_discover":
		needs = append(slices.Clone(watchDeps), nativeToolDep{"Watch.Feeds", deps.Watch.Feeds != nil})
		build = func() (einotool.BaseTool, error) { return buildFeedDiscoverTool(deps.Watch) }
	case "show_view", "update_view":
		needs = []nativeToolDep{
			{"Views.Store", deps.Views.Store != nil},
			{"Views.Sources.Files", deps.Views.Sources.Files != nil},
			{"Views.Sources.Links", deps.Views.Sources.Links != nil},
			{"Views.Triggers", deps.Views.Triggers != nil},
			{"Views.Context", deps.Views.Context != nil},
		}
		build = func() (einotool.BaseTool, error) {
			if name == "update_view" {
				return buildUpdateViewTool(deps.Views)
			}
			return buildShowViewTool(deps.Views)
		}
	default:
		return nil, fmt.Errorf("no factory for native tool %q", name)
	}
	for _, dep := range needs {
		if !dep.present {
			return nil, fmt.Errorf("native tool %q requires NativeToolDeps.%s", name, dep.field)
		}
	}
	return func(context.Context) (einotool.BaseTool, error) {
		return build()
	}, nil
}

// RegisterNativeTools registers every static local tool declared by
// localToolNames/configuredLocalSpec into the core.ToolRegistry. Run-scoped
// tools (runScopedTool) are excluded: they depend on services (web access,
// workstation) built for each run, so they cannot be resolved at wire time.
// The runtime toolset catalog contributes them instead (see BuildWebToolSpecs).
func RegisterNativeTools(registry core.ToolRegistry, deps NativeToolDeps) error {
	if registry == nil {
		return fmt.Errorf("RegisterNativeTools: registry is nil")
	}
	// localToolNames is the canonical name list in canonical order; reusing it
	// keeps the registry from drifting from configuredLocalSpec.
	for _, name := range localToolNames {
		if runScopedTool(name) {
			continue
		}
		spec := configuredLocalSpec(name)
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
		if name == "skill_inspect" && deps.Skills.Packages == nil {
			if deps.Skills.PackagesDisabledReason == "" {
				return errors.New("RegisterNativeTools: skill_inspect needs NativeToolDeps.Skills.Packages or a PackagesDisabledReason")
			}
			spec.Health = core.ToolHealth{State: core.HealthStateDisabled, Reason: deps.Skills.PackagesDisabledReason}
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
