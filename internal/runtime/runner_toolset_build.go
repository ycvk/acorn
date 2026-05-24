package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/browser"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/memorymodule"
	appmodel "github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/orchestration"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/skilllifecycle"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/webaccess"
	"github.com/ycvk/acorn/internal/workingstate"
)

func (f *RunnerFactory) buildToolset(
	ctx context.Context,
	sessionID string,
	childExec orchestration.ChildAgentExecutor,
	includePlanning bool,
	profile tooling.ToolProfile,
) (*Toolset, error) {
	if f == nil || f.deps.Config == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	if f.deps.Workspace == nil {
		return nil, errors.New("workspace contract is not initialized")
	}
	if f.deps.ArtifactService == nil {
		return nil, errors.New("artifact service is not initialized")
	}
	if f.deps.TerminalService == nil {
		return nil, errors.New("terminal session service is not initialized")
	}

	webFetchService, err := webaccess.NewFetchService(webaccess.FetchConfig{
		UserAgent:        f.deps.Config.WebAccess.UserAgent,
		Timeout:          time.Duration(f.deps.Config.WebAccess.TimeoutSeconds) * time.Second,
		MaxResponseBytes: f.deps.Config.WebAccess.MaxResponseBytes,
		Policy: webaccess.URLPolicy{
			AllowPrivateNetworks: f.deps.Config.WebAccess.AllowPrivateNetworks,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("web fetch service: %w", err)
	}
	webSearchService, err := webaccess.NewSearchService(webaccess.SearchConfig{
		APIKey:           f.deps.Config.WebAccess.Search.APIKey,
		Timeout:          time.Duration(f.deps.Config.WebAccess.Search.TimeoutSeconds) * time.Second,
		MaxResults:       f.deps.Config.WebAccess.Search.MaxResults,
		MaxResponseBytes: f.deps.Config.WebAccess.MaxResponseBytes,
		Policy: webaccess.URLPolicy{
			AllowPrivateNetworks: f.deps.Config.WebAccess.AllowPrivateNetworks,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("web search service: %w", err)
	}
	browserService, err := browser.NewService(browser.Config{
		ExecutablePath: strings.TrimSpace(f.deps.Config.Browser.ExecutablePath),
		Headless:       f.deps.Config.Browser.Headless,
		Timeout:        time.Duration(f.deps.Config.Browser.DefaultTimeoutSeconds) * time.Second,
		UserAgent:      f.deps.Config.WebAccess.UserAgent,
		Policy: webaccess.URLPolicy{
			AllowPrivateNetworks: f.deps.Config.WebAccess.AllowPrivateNetworks,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("browser service: %w", err)
	}

	var operatorStore tools.OperatorQuestionStore
	if f.deps.MCPPendingActions != nil {
		operatorStore = f.deps.MCPPendingActions
	} else if store, ok := f.deps.Store.(tools.OperatorQuestionStore); ok {
		operatorStore = store
	}

	localCatalog, err := tools.BuildCatalog(tools.CatalogConfig{
		Workspace:         f.deps.Workspace,
		MutationEnabled:   !f.deps.Config.Tools.Mutation.Disabled,
		RunCommandEnabled: !f.deps.Config.Tools.RunCommand.Disabled,
		ArtifactService:   f.deps.ArtifactService,
		ArtifactContext:   artifactToolBridge{},
		TerminalService:   f.deps.TerminalService,
		TerminalContext:   artifactToolBridge{},
		OperatorStore:     operatorStore,
		OperatorContext:   artifactToolBridge{},
		WebFetchService:   webFetchService,
		WebSearchService:  webSearchService,
		BrowserService:    browserService,
	}, f.deps.ExtraLocalTools, childExec, delegateTaskBridge{})
	if err != nil {
		return nil, err
	}

	checkpointService := f.deps.CheckpointService
	effectiveSessionID := sessionID
	if !includePlanning {
		checkpointService = nil
		effectiveSessionID = ""
	}
	checkpointTools, err := workingstate.BuildWorkingCheckpointTools(checkpointService, effectiveSessionID)
	if err != nil {
		return nil, fmt.Errorf("build working checkpoint tools: %w", err)
	}
	var memoryTools []einotool.BaseTool
	if f.deps.MemoryModule != nil {
		fileTools, err := buildMemoryFileTools(ctx, f.deps.MemoryModule)
		if err != nil {
			return nil, err
		}
		memoryTools = append(memoryTools, fileTools...)
	}

	skillTools, err := skills.BuildAgentTools(f.deps.Loader)
	if err != nil {
		return nil, fmt.Errorf("build skill tools: %w", err)
	}
	var skillLifecycleTools []einotool.BaseTool
	if includePlanning {
		skillLifecycleTools, err = skilllifecycle.BuildAgentTools(skilllifecycle.ToolOptions{
			Loader: f.deps.Loader,
			Store:  f.deps.Store,
			Bridge: delegateTaskBridge{},
		})
		if err != nil {
			return nil, fmt.Errorf("build skill lifecycle tools: %w", err)
		}
	}

	specs, err := buildCatalogSpecs(ctx, f.deps.Config, "local", tooling.ToolKindNative, []tooling.ToolProfile{tooling.ToolProfileRun, tooling.ToolProfileServe}, append([]einotool.BaseTool(nil), localCatalog.Tools...))
	if err != nil {
		return nil, err
	}
	checkpointSpecs, err := buildCatalogSpecs(ctx, f.deps.Config, "workingstate", tooling.ToolKindMemory, []tooling.ToolProfile{tooling.ToolProfileRun, tooling.ToolProfileServe}, checkpointTools)
	if err != nil {
		return nil, err
	}
	memorySpecs, err := buildCatalogSpecs(ctx, f.deps.Config, "memory", tooling.ToolKindMemory, []tooling.ToolProfile{tooling.ToolProfileRun, tooling.ToolProfileServe}, memoryTools)
	if err != nil {
		return nil, err
	}
	skillSpecs, err := buildCatalogSpecs(ctx, f.deps.Config, "skill", tooling.ToolKindSkill, []tooling.ToolProfile{tooling.ToolProfileRun, tooling.ToolProfileServe}, skillTools)
	if err != nil {
		return nil, err
	}
	skillLifecycleSpecs, err := buildCatalogSpecs(ctx, f.deps.Config, "skill.lifecycle", tooling.ToolKindSkill, []tooling.ToolProfile{tooling.ToolProfileRun}, skillLifecycleTools)
	if err != nil {
		return nil, err
	}
	specs = append(specs, checkpointSpecs...)
	specs = append(specs, memorySpecs...)
	specs = append(specs, skillSpecs...)
	specs = append(specs, skillLifecycleSpecs...)

	if includePlanning {
		loadToolsTool, err := newLoadToolsTool(f.deps.ContextPlane)
		if err != nil {
			return nil, fmt.Errorf("build load_tools tool: %w", err)
		}
		planningSpecs, err := buildCatalogSpecs(ctx, f.deps.Config, "runtime", tooling.ToolKindNative, []tooling.ToolProfile{tooling.ToolProfileRun}, []einotool.BaseTool{loadToolsTool})
		if err != nil {
			return nil, err
		}
		specs = append(specs, planningSpecs...)
	}
	catalog, err := tooling.NewCatalog(ctx, specs)
	if err != nil {
		return nil, fmt.Errorf("build toolset catalog: %w", err)
	}
	return &Toolset{catalog: catalog, profile: profile, closers: []toolsetCloser{browserService}}, nil
}

const capabilityDiscoveryInstruction = `Capability discovery rules:
- Before answering a capability question or saying you cannot do something, inspect the skill catalog and currently loaded tools already present in context.
- If a relevant skill may exist but the catalog summary is not enough, call skill_list or skill_view before answering.
- If a relevant capability depends on deferred tools, call load_tools before concluding the capability is unavailable.
- Prefer the matching skill and tool path over a generic limitation answer.`

func emitProviderDegradedIfNeeded(ctx context.Context, store EventAppender, req RunnerBuildRequest, statuses []mcpprovider.ProviderStatus) error {
	if store == nil || strings.TrimSpace(req.RunID) == "" {
		return nil
	}
	var healthy, failed bool
	var failedEntries []ProviderDegradedEntry
	for _, s := range statuses {
		if !s.Enabled {
			continue
		}
		if s.StartupStatus == "healthy" {
			healthy = true
		} else if s.StartupStatus == "failed" {
			failed = true
			failedEntries = append(failedEntries, ProviderDegradedEntry{
				Name:      s.Name,
				Transport: s.Transport,
				Error:     s.Error,
			})
		}
	}
	if !healthy || !failed {
		return nil
	}
	_, err := AppendStreamItem(ctx, store, req.Sink, StreamItem{
		RunID:     req.RunID,
		Kind:      StreamKindProviderDegraded,
		CreatedAt: time.Now().UTC(),
		Payload: &ProviderDegradedPayload{
			AffectedProviders: failedEntries,
		},
	})
	return err
}

func emitMemoryPreparedEvent(ctx context.Context, store EventAppender, req RunnerBuildRequest, workspaceScope string, result *memorymodule.PrepareResult) error {
	if store == nil || strings.TrimSpace(req.RunID) == "" {
		return nil
	}
	prepared := &StreamMemoryPrepared{
		Query:          strings.TrimSpace(req.Input),
		WorkspaceScope: strings.TrimSpace(workspaceScope),
	}
	if result != nil {
		prepared.NudgeCount = len(result.Nudges)
		prepared.EntryCount = len(result.Entries)
		prepared.Nudges = make([]StreamMemoryPreparedNudge, 0, len(result.Nudges))
		for _, nudge := range result.Nudges {
			prepared.Nudges = append(prepared.Nudges, StreamMemoryPreparedNudge{
				Ref:    nudge.Ref,
				Kind:   nudge.Kind,
				Title:  nudge.Title,
				Status: nudge.Status,
				Reason: nudge.Reason,
			})
		}
		prepared.Entries = make([]StreamMemoryPreparedEntry, 0, len(result.Entries))
		for _, entry := range result.Entries {
			prepared.Entries = append(prepared.Entries, StreamMemoryPreparedEntry{
				Ref:   entry.Ref,
				Kind:  entry.Kind,
				Title: entry.Title,
			})
		}
	}
	_, err := AppendStreamItem(ctx, store, req.Sink, StreamItem{
		RunID:     req.RunID,
		Kind:      StreamKindMemoryPrepared,
		CreatedAt: time.Now().UTC(),
		Payload:   &MemoryPreparedPayload{MemoryPrepared: prepared},
	})
	return err
}

func emitProcedureActivationEvents(ctx context.Context, store EventAppender, sink StreamSink, runID string, activations []memorymodule.ProcedureActivation) error {
	if store == nil || strings.TrimSpace(runID) == "" || len(activations) == 0 {
		return nil
	}
	for _, activation := range activations {
		_, err := AppendStreamItem(ctx, store, sink, StreamItem{
			RunID:     runID,
			Kind:      StreamKindProcedureActivation,
			CreatedAt: time.Now().UTC(),
			Payload: &ProcedureActivationPayload{
				ProcedureActivation: streamProcedureActivationFromDomain(runID, activation),
			},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func filterProcedureActivationsByPhase(items []memorymodule.ProcedureActivation, phase memorymodule.ProcedureActivationPhase) []memorymodule.ProcedureActivation {
	if len(items) == 0 {
		return nil
	}
	out := make([]memorymodule.ProcedureActivation, 0, len(items))
	for _, item := range items {
		if item.Phase == phase {
			out = append(out, item)
		}
	}
	return out
}

func streamProcedureActivationFromDomain(runID string, item memorymodule.ProcedureActivation) *StreamProcedureActivation {
	effectiveRunID := strings.TrimSpace(item.RunID)
	if effectiveRunID == "" {
		effectiveRunID = strings.TrimSpace(runID)
	}
	return &StreamProcedureActivation{
		RunID:        effectiveRunID,
		SessionID:    strings.TrimSpace(item.SessionID),
		ProcedureRef: strings.TrimSpace(item.ProcedureRef),
		Title:        strings.TrimSpace(item.Title),
		Kind:         strings.TrimSpace(item.Kind),
		Phase:        string(item.Phase),
		Reason:       strings.TrimSpace(item.Reason),
		Score:        item.Score,
		Status:       string(item.Status),
		Origin:       string(item.Origin),
		SourceRefs:   append([]string(nil), item.SourceRefs...),
		EvidenceRefs: append([]string(nil), item.EvidenceRefs...),
	}
}

func buildStableInstruction(base string, instructionSuffix string) string {
	parts := []string{
		strings.TrimSpace(base),
		strings.TrimSpace(capabilityDiscoveryInstruction),
		strings.TrimSpace(instructionSuffix),
	}
	out := make([]string, 0, len(parts))
	for _, item := range parts {
		if strings.TrimSpace(item) != "" {
			out = append(out, strings.TrimSpace(item))
		}
	}
	return strings.Join(out, "\n\n")
}

type chatModelBuilder func(context.Context, config.ProviderConfig) (einomodel.BaseChatModel, error)

func buildRuntimeChatModel(ctx context.Context, cfg *config.Config, newModel chatModelBuilder) (einomodel.BaseChatModel, error) {
	model, _, err := buildRuntimeChatModelWithProvider(ctx, cfg, newModel)
	return model, err
}

func buildRuntimeChatModelWithProvider(ctx context.Context, cfg *config.Config, newModel chatModelBuilder) (einomodel.BaseChatModel, config.ProviderConfig, error) {
	if cfg == nil {
		return nil, config.ProviderConfig{}, errors.New("config is required")
	}
	if newModel == nil {
		newModel = appmodel.NewChatModel
	}

	provider, err := cfg.EnabledProvider()
	if err != nil {
		return nil, config.ProviderConfig{}, err
	}
	model, err := newModel(ctx, provider)
	if err != nil {
		return nil, config.ProviderConfig{}, fmt.Errorf("init provider %s: %w", provider.Name, err)
	}
	return model, provider, nil
}

func newRuntimeChatModel(
	ctx context.Context,
	cfg *config.Config,
	newModel chatModelBuilder,
	_ any,
) (einomodel.BaseChatModel, error) {
	return buildRuntimeChatModel(ctx, cfg, newModel)
}

func (f *RunnerFactory) newChatModel(ctx context.Context) (einomodel.BaseChatModel, error) {
	if f == nil || f.deps.Config == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	return newRuntimeChatModel(ctx, f.deps.Config, nil, nil)
}

// Subagent execution is now handled by SubagentExecutor in subagent_executor.go.
// The samplingExecutorAdapter has been replaced.

func skillEligibilityContextFromCatalog(catalog *tooling.Catalog) skills.EligibilityContext {
	if catalog == nil {
		return skills.EligibilityContext{}
	}
	return tooling.EligibilityContextForProfile(catalog, tooling.ToolProfileRun, nil)
}

func emitSkillSelectionEvents(ctx context.Context, store EventAppender, req RunnerBuildRequest, selected *SelectedSkill, matches []SkillMatch) error {
	if store == nil || strings.TrimSpace(req.RunID) == "" {
		return nil
	}
	candidates := topSkillCandidates(matches, 3)
	discoveredSkill := &StreamSkill{
		Candidates: candidates,
	}
	if selected == nil {
		discoveredSkill.NoSelectionReason = deriveNoSelectionReason(req, matches)
	}
	if _, err := AppendStreamItem(ctx, store, req.Sink, StreamItem{
		RunID:     req.RunID,
		Kind:      StreamKindSkillDiscovered,
		CreatedAt: time.Now().UTC(),
		Payload:   &SkillDiscoveredPayload{Skill: discoveredSkill},
	}); err != nil {
		return err
	}
	if selected == nil {
		return nil
	}
	streamSkill := streamSkillFromSelected(selected, candidates)
	if _, err := AppendStreamItem(ctx, store, req.Sink, StreamItem{
		RunID:     req.RunID,
		Kind:      StreamKindSkillSelected,
		CreatedAt: time.Now().UTC(),
		Payload:   &SkillSelectedPayload{Skill: streamSkill},
	}); err != nil {
		return err
	}
	if _, err := AppendStreamItem(ctx, store, req.Sink, StreamItem{
		RunID:     req.RunID,
		Kind:      StreamKindSkillLoaded,
		CreatedAt: time.Now().UTC(),
		Payload:   &SkillLoadedPayload{Skill: streamSkill},
	}); err != nil {
		return err
	}
	if err := emitProcedureActivationEvents(ctx, store, req.Sink, req.RunID, []memorymodule.ProcedureActivation{
		procedureActivationFromSelectedSkill(req, selected, memorymodule.ProcedureActivationSelected, "decision_selected_skill"),
		procedureActivationFromSelectedSkill(req, selected, memorymodule.ProcedureActivationUsed, "skill_loaded_for_run"),
	}); err != nil {
		return err
	}
	return nil
}

func deriveNoSelectionReason(req RunnerBuildRequest, matches []SkillMatch) string {
	if strings.TrimSpace(req.SkillID) != "" {
		if len(matches) == 0 {
			return "explicit_skill_missing"
		}
		if matches[0].FilteredReason != "" {
			return "explicit_skill_ineligible"
		}
	}
	eligible := make([]SkillMatch, 0, len(matches))
	for _, item := range matches {
		if item.FilteredReason != "" || item.Score <= 0 || !item.TriggerMatched {
			continue
		}
		eligible = append(eligible, item)
	}
	if len(eligible) == 0 {
		return noEligibleSkillMatchReason
	}
	if len(eligible) > 1 && eligible[0].Score == eligible[1].Score {
		return ambiguousTopScoreReason
	}
	return ""
}

func topSkillCandidates(matches []SkillMatch, limit int) []StreamSkillCandidate {
	if limit <= 0 || len(matches) == 0 {
		return nil
	}
	if len(matches) < limit {
		limit = len(matches)
	}
	items := make([]StreamSkillCandidate, 0, limit)
	for _, item := range matches[:limit] {
		items = append(items, StreamSkillCandidate{
			ID:             item.Skill.ID,
			Name:           item.Skill.Name,
			Score:          item.Score,
			MatchedTerms:   append([]string(nil), item.MatchedTerms...),
			FilteredReason: item.FilteredReason,
			Requirements:   StreamSkillRequirementsFromDomain(item.Skill.Requires),
			Summary:        item.Skill.Summary,
			Origin:         string(item.Skill.Origin),
			TaskPattern:    item.Skill.TaskPattern,
		})
	}
	return items
}

func streamSkillFromSelected(selected *SelectedSkill, candidates []StreamSkillCandidate) *StreamSkill {
	if selected == nil {
		return nil
	}
	return &StreamSkill{
		SelectedID:   selected.Skill.ID,
		Name:         selected.Skill.Name,
		Candidates:   candidates,
		Source:       selected.Skill.Source,
		Origin:       string(selected.Skill.Origin),
		TaskPattern:  selected.Skill.TaskPattern,
		Path:         selected.Skill.Path,
		Summary:      selected.Skill.Summary,
		Instruction:  selected.Skill.Instruction,
		Scripts:      append([]string(nil), selected.Skill.Scripts...),
		Requirements: StreamSkillRequirementsFromDomain(selected.Skill.Requires),
		Score:        selected.Score,
		MatchedTerms: append([]string(nil), selected.MatchedTerms...),
	}
}

func procedureActivationFromSelectedSkill(req RunnerBuildRequest, selected *SelectedSkill, phase memorymodule.ProcedureActivationPhase, reason string) memorymodule.ProcedureActivation {
	if selected == nil {
		return memorymodule.ProcedureActivation{}
	}
	return memorymodule.ProcedureActivation{
		RunID:        strings.TrimSpace(req.RunID),
		SessionID:    strings.TrimSpace(req.SessionID),
		ProcedureRef: strings.TrimSpace(selected.Skill.ID),
		Title:        strings.TrimSpace(selected.Skill.Name),
		Kind:         "executable_skill",
		Phase:        phase,
		Reason:       reason,
		Score:        float64(selected.Score),
		Origin:       memorymodule.ProcedureOrigin(strings.TrimSpace(string(selected.Skill.Origin))),
		SourceRefs:   nonEmptyStrings(selected.Skill.PromotedFrom, selected.Skill.Path),
	}
}

func nonEmptyStrings(items ...string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
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

func recommendedSkillsFromMatches(matches []SkillMatch) []decision.RecommendedSkill {
	items := make([]decision.RecommendedSkill, 0, len(matches))
	for _, item := range matches {
		items = append(items, decision.RecommendedSkill{
			ID:             item.Skill.ID,
			Name:           item.Skill.Name,
			Score:          item.Score,
			TriggerMatched: item.TriggerMatched,
			FilteredReason: item.FilteredReason,
		})
	}
	return items
}

func emitDecisionEvents(ctx context.Context, store EventAppender, req RunnerBuildRequest, record *decision.Record, explicitSkillID string) error {
	if store == nil || strings.TrimSpace(req.RunID) == "" || record == nil {
		return nil
	}
	finalKind := StreamKindDecisionSelected
	if record.Action == decision.ActionAskUser || record.Action == decision.ActionBlock || record.Action == decision.ActionResumeRun {
		finalKind = StreamKindDecisionBlocked
	}
	decisionPayload := &DecisionSelectedPayload{
		Action:              string(record.Action),
		Intent:              record.Intent,
		SelectedSkillID:     record.SelectedSkillID,
		DecisionReason:      record.DecisionReason,
		DecisionProfileHash: record.DecisionProfileHash,
		ExplicitSkillID:     strings.TrimSpace(explicitSkillID),
	}
	if finalKind == StreamKindDecisionBlocked {
		_, err := AppendStreamItem(ctx, store, req.Sink, StreamItem{
			RunID:     req.RunID,
			Kind:      finalKind,
			CreatedAt: time.Now().UTC(),
			Payload: &DecisionBlockedPayload{
				Action:              string(record.Action),
				Intent:              record.Intent,
				SelectedSkillID:     record.SelectedSkillID,
				DecisionReason:      record.DecisionReason,
				DecisionProfileHash: record.DecisionProfileHash,
				ExplicitSkillID:     strings.TrimSpace(explicitSkillID),
			},
		})
		return err
	}
	_, err := AppendStreamItem(ctx, store, req.Sink, StreamItem{
		RunID:     req.RunID,
		Kind:      finalKind,
		CreatedAt: time.Now().UTC(),
		Payload:   decisionPayload,
	})
	return err
}

func StreamSkillRequirementsFromDomain(item skills.Requirements) StreamSkillRequirements {
	return StreamSkillRequirements{
		Tools:    append([]string(nil), item.Tools...),
		Toolsets: append([]string(nil), item.Toolsets...),
		Bins:     append([]string(nil), item.Bins...),
		Env:      append([]string(nil), item.Env...),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return value
		}
	}
	return ""
}

func runtimeMatchesFromRecommendations(items []skills.Recommendation) []SkillMatch {
	if len(items) == 0 {
		return nil
	}
	out := make([]SkillMatch, 0, len(items))
	for _, item := range items {
		out = append(out, SkillMatch{
			Skill:          skills.CopySpec(item.Skill),
			Score:          item.Score,
			MatchedTerms:   append([]string(nil), item.MatchedTerms...),
			TriggerMatched: item.TriggerMatched,
			FilteredReason: item.FilteredReason,
		})
	}
	return out
}
