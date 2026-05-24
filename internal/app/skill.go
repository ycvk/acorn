package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	storesqlite "github.com/ycvk/acorn/internal/store/sqlite"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/memorymodule"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tooling"
)

type stableSkillScanner interface {
	ScanSkills(ctx context.Context) (*skills.ScanResult, error)
	CreateSkill(ctx context.Context, input skills.CreateInput) (*skills.Spec, error)
	PatchSkillWithSource(ctx context.Context, skillID, patchContent, source string) error
	DeleteSkill(ctx context.Context, skillID string) error
	ReadSkillFile(ctx context.Context, skillID, relativePath string) (string, error)
	WriteSkillFile(ctx context.Context, skillID, relativePath, content string) error
}

type SkillService struct {
	cfg     *config.Config
	scanner stableSkillScanner
}

var (
	ErrSkillAlreadyExists = errors.New("skill already exists")
	ErrSkillNotFound      = errors.New("skill not found")
)

func NewSkillService(cfg *config.Config, scanner stableSkillScanner) *SkillService {
	return &SkillService{cfg: cfg, scanner: scanner}
}

func (s *SkillService) Snapshot(ctx context.Context) (*skills.Snapshot, error) {
	if s == nil || s.scanner == nil {
		return nil, errors.New("stable skill scanner is nil")
	}
	scan, err := s.scanner.ScanSkills(ctx)
	if err != nil {
		return nil, err
	}
	snapshot, err := skills.BuildSnapshot(*scan, staticSkillEligibilityContext(s.cfg))
	if err != nil {
		return nil, err
	}
	copied := skills.CopySnapshot(snapshot)
	return &copied, nil
}

func (s *SkillService) Health(ctx context.Context, fixtures []skills.RoutingFixture) (*skills.HealthReport, error) {
	if s == nil || s.scanner == nil {
		return nil, errors.New("stable skill scanner is nil")
	}
	scan, err := s.scanner.ScanSkills(ctx)
	if err != nil {
		return nil, err
	}
	report, err := skills.BuildHealthReport(*scan, staticSkillEligibilityContext(s.cfg), fixtures)
	if err != nil {
		return nil, err
	}
	copied := skills.CopyHealthReport(*report)
	return &copied, nil
}

type SkillListFilter struct {
	Limit  int
	Offset int
}

type CreateSkillInput struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Version      string              `json:"version,omitempty"`
	Category     string              `json:"category,omitempty"`
	Summary      string              `json:"summary,omitempty"`
	PromotedFrom string              `json:"promoted_from,omitempty"`
	Origin       skills.Origin       `json:"origin,omitempty"`
	TaskPattern  string              `json:"task_pattern,omitempty"`
	Instruction  string              `json:"instruction"`
	Tags         []string            `json:"tags,omitempty"`
	Platforms    []string            `json:"platforms,omitempty"`
	TriggerHints []string            `json:"trigger_hints,omitempty"`
	Requires     skills.Requirements `json:"requirements,omitempty"`
}

type SkillFileView struct {
	SkillID string `json:"skill_id"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (s *SkillService) ListFiltered(ctx context.Context, filter SkillListFilter) ([]skills.View, int, error) {
	snapshot, err := s.Snapshot(ctx)
	if err != nil {
		return nil, 0, err
	}
	items := make([]skills.View, 0, len(snapshot.Skills))
	for _, item := range snapshot.Skills {
		items = append(items, skills.CopyView(item))
	}
	filtered := make([]skills.View, 0, len(items))
	filtered = append(filtered, items...)
	total := len(filtered)
	if filter.Limit > 0 {
		end := filter.Offset + filter.Limit
		if end > total {
			end = total
		}
		if filter.Offset >= total {
			filtered = []skills.View{}
		} else {
			filtered = filtered[filter.Offset:end]
		}
	}
	return filtered, total, nil
}

func (s *SkillService) List(ctx context.Context, limit int) ([]skills.View, error) {
	items, _, err := s.ListFiltered(ctx, SkillListFilter{Limit: limit})
	return items, err
}

func (s *SkillService) Get(ctx context.Context, id string) (*skills.View, error) {
	snapshot, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return nil, fmt.Errorf("skill id is required")
	}
	for _, item := range snapshot.Skills {
		if item.ID == trimmedID {
			return new(skills.CopyView(item)), nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrSkillNotFound, trimmedID)
}

func (s *SkillService) Create(ctx context.Context, input CreateSkillInput) (*skills.View, error) {
	if s == nil || s.scanner == nil {
		return nil, errors.New("stable skill scanner is nil")
	}
	spec, err := s.scanner.CreateSkill(ctx, skills.CreateInput{
		ID:           input.ID,
		Name:         input.Name,
		Version:      input.Version,
		Category:     input.Category,
		Summary:      input.Summary,
		PromotedFrom: input.PromotedFrom,
		Origin:       input.Origin,
		TaskPattern:  input.TaskPattern,
		Instruction:  input.Instruction,
		Tags:         append([]string(nil), input.Tags...),
		Platforms:    append([]string(nil), input.Platforms...),
		TriggerHints: append([]string(nil), input.TriggerHints...),
		Requires:     skills.CopyRequirements(input.Requires),
	})
	if err != nil {
		return nil, translateSkillStoreError(err)
	}
	view, err := skills.Evaluate(*spec, staticSkillEligibilityContext(s.cfg))
	if err != nil {
		return nil, err
	}
	copied := skills.CopyView(view)
	return &copied, nil
}

func (s *SkillService) Patch(ctx context.Context, id, content, source string) (*skills.View, error) {
	if s == nil || s.scanner == nil {
		return nil, errors.New("stable skill scanner is nil")
	}
	if err := s.scanner.PatchSkillWithSource(ctx, id, content, source); err != nil {
		return nil, translateSkillStoreError(err)
	}
	return s.Get(ctx, id)
}

func (s *SkillService) Delete(ctx context.Context, id string) error {
	if s == nil || s.scanner == nil {
		return errors.New("stable skill scanner is nil")
	}
	return translateSkillStoreError(s.scanner.DeleteSkill(ctx, id))
}

func (s *SkillService) ReadFile(ctx context.Context, id, relativePath string) (*SkillFileView, error) {
	if s == nil || s.scanner == nil {
		return nil, errors.New("stable skill scanner is nil")
	}
	content, err := s.scanner.ReadSkillFile(ctx, id, relativePath)
	if err != nil {
		return nil, translateSkillStoreError(err)
	}
	path := strings.TrimSpace(relativePath)
	if path == "" {
		path = "SKILL.md"
	}
	return &SkillFileView{SkillID: strings.TrimSpace(id), Path: path, Content: content}, nil
}

func (s *SkillService) WriteFile(ctx context.Context, id, relativePath, content string) error {
	if s == nil || s.scanner == nil {
		return errors.New("stable skill scanner is nil")
	}
	return translateSkillStoreError(s.scanner.WriteSkillFile(ctx, id, relativePath, content))
}

func translateSkillStoreError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, skills.ErrAlreadyExists):
		return fmt.Errorf("%w: %v", ErrSkillAlreadyExists, err)
	case errors.Is(err, skills.ErrNotFound):
		return fmt.Errorf("%w: %v", ErrSkillNotFound, err)
	default:
		return err
	}
}

type MemoryService struct {
	module   memorymodule.Service
	semantic MemoryServiceSemanticOptions
}

type MemoryServiceSemanticOptions struct {
	Index      memorymodule.SemanticIndex
	Embedder   memorymodule.Embedder
	Model      string
	Dimensions int
	BatchSize  int
	Schema     string
	IndexName  string
}

func NewMemoryService(module memorymodule.Service, semantic MemoryServiceSemanticOptions) (*MemoryService, error) {
	if module == nil {
		return nil, errors.New("memory module service is required")
	}
	return &MemoryService{module: module, semantic: semantic}, nil
}

func (s *MemoryService) ListFacts(ctx context.Context, selection memorymodule.RecordSelection) ([]memorymodule.Record, error) {
	return s.module.ListFacts(ctx, selection)
}

func (s *MemoryService) ListSkills(ctx context.Context, selection memorymodule.RecordSelection) ([]memorymodule.Record, error) {
	return s.module.ListSkills(ctx, selection)
}

func (s *MemoryService) ListHistory(ctx context.Context, selection memorymodule.RecordSelection) ([]memorymodule.Record, error) {
	return s.module.ListHistory(ctx, selection)
}

func (s *MemoryService) Search(ctx context.Context, req memorymodule.SearchRequest) (*memorymodule.SearchResult, error) {
	return s.module.Search(ctx, req)
}

func (s *MemoryService) RebuildSemanticIndex(ctx context.Context) (*memorymodule.SemanticRebuildResult, error) {
	if s == nil || s.module == nil {
		return nil, errors.New("memory service is required")
	}
	return s.module.RebuildSemanticIndex(ctx, memorymodule.SemanticRebuildOptions{
		Index:      s.semantic.Index,
		Embedder:   s.semantic.Embedder,
		Model:      s.semantic.Model,
		Dimensions: s.semantic.Dimensions,
		BatchSize:  s.semantic.BatchSize,
		Schema:     s.semantic.Schema,
		IndexName:  s.semantic.IndexName,
	})
}

func (s *MemoryService) PlanMemoryMutation(ctx context.Context, req memorymodule.PlanMemoryMutationRequest) (*memorymodule.MemoryMutationPlan, error) {
	return s.module.PlanMemoryMutation(ctx, req)
}

func (s *MemoryService) CreateProcedure(ctx context.Context, req memorymodule.CreateProcedureRequest) (*memorymodule.ProcedureRecord, error) {
	return s.module.CreateProcedure(ctx, req)
}

type CapabilitySnapshotOptions struct {
	ProbeMCP bool `json:"probe_mcp"`
}

type SystemCapabilities struct {
	Summary           SystemCapabilitySummary       `json:"summary"`
	Model             SystemModelCapabilities       `json:"model"`
	RuntimeReadiness  *RuntimeReadiness             `json:"runtime_readiness,omitempty"`
	Features          SystemFeatureCapabilities     `json:"features"`
	ToolCatalogError  string                        `json:"tool_catalog_error,omitempty"`
	Tools             []SystemToolCapability        `json:"tools,omitempty"`
	Skills            SystemSkillCapabilities       `json:"skills"`
	MCPProviders      []SystemMCPProviderCapability `json:"mcp_providers,omitempty"`
	ProviderReadiness []ProviderReadinessSummary    `json:"provider_readiness,omitempty"`
}

type SystemCapabilitySummary struct {
	ToolCount                  int `json:"tool_count"`
	EnabledToolCount           int `json:"enabled_tool_count"`
	SkillCount                 int `json:"skill_count"`
	EligibleSkillCount         int `json:"eligible_skill_count"`
	IneligibleSkillCount       int `json:"ineligible_skill_count"`
	InvalidSkillCount          int `json:"invalid_skill_count"`
	MCPConfiguredProviderCount int `json:"mcp_configured_provider_count"`
	MCPEnabledProviderCount    int `json:"mcp_enabled_provider_count"`
	MCPHealthyProviderCount    int `json:"mcp_healthy_provider_count"`
}

type SystemModelCapabilities struct {
	Name string `json:"name"`
}

type SystemFeatureCapabilities struct {
	InterruptResume bool `json:"interrupt_resume"`
	TraceDebug      bool `json:"trace_debug"`
	SessionHistory  bool `json:"session_history"`
}

type SystemToolCapability struct {
	Name           string   `json:"name"`
	Source         string   `json:"source"`
	Kind           string   `json:"kind"`
	Category       string   `json:"category"`
	ResourceScope  string   `json:"resource_scope,omitempty"`
	Profiles       []string `json:"profiles,omitempty"`
	Enabled        bool     `json:"enabled"`
	HealthState    string   `json:"health_state"`
	HealthReason   string   `json:"health_reason,omitempty"`
	ParallelPolicy string   `json:"parallel_policy,omitempty"`
	PlanPolicy     string   `json:"plan_policy,omitempty"`
	FactPolicy     string   `json:"fact_policy,omitempty"`
	Risk           string   `json:"risk"`
	RootDir        string   `json:"root_dir,omitempty"`
	WorkDir        string   `json:"work_dir,omitempty"`
	DefaultTimeout int      `json:"default_timeout,omitempty"`
}

type SystemSkillCapabilities struct {
	Count           int                  `json:"count"`
	EligibleCount   int                  `json:"eligible_count,omitempty"`
	IneligibleCount int                  `json:"ineligible_count,omitempty"`
	InvalidCount    int                  `json:"invalid_count,omitempty"`
	Items           []SystemSkillSummary `json:"items,omitempty"`
	Problems        []SystemSkillProblem `json:"problems,omitempty"`
	LoadError       string               `json:"load_error,omitempty"`
}

type SystemSkillSummary struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Version         string   `json:"version"`
	Source          string   `json:"source"`
	Origin          string   `json:"origin"`
	TaskPattern     string   `json:"task_pattern,omitempty"`
	Summary         string   `json:"summary,omitempty"`
	PromotedFrom    string   `json:"promoted_from,omitempty"`
	Eligible        bool     `json:"eligible"`
	DisabledReasons []string `json:"disabled_reasons,omitempty"`
}

type SystemSkillProblem struct {
	ID     string `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Source string `json:"source,omitempty"`
	Path   string `json:"path,omitempty"`
	Error  string `json:"error,omitempty"`
}

type SystemMCPProviderCapability struct {
	Name                string   `json:"name"`
	Configured          bool     `json:"configured"`
	Enabled             bool     `json:"enabled"`
	Transport           string   `json:"transport,omitempty"`
	StartupStatus       string   `json:"startup_status,omitempty"`
	Command             string   `json:"command"`
	Args                []string `json:"args,omitempty"`
	WorkDir             string   `json:"work_dir,omitempty"`
	CommandPath         string   `json:"command_path,omitempty"`
	ConfiguredToolNames []string `json:"configured_tool_names,omitempty"`
	DiscoveredToolNames []string `json:"discovered_tool_names,omitempty"`
	ToolCount           int      `json:"tool_count"`
	Error               string   `json:"error,omitempty"`
	AuthStatus          string   `json:"auth_status,omitempty"`
}

type skillSnapshotStore interface {
	Snapshot(ctx context.Context) (*skills.Snapshot, error)
}

type providerStatusDoctor func(ctx context.Context, cfgs []mcpprovider.ProviderConfig) []mcpprovider.ProviderStatus

type liveMCPManager interface {
	Statuses() []mcpprovider.ProviderStatus
}

type toolCatalogBuilder interface {
	BuildCapabilityCatalog(ctx context.Context) (*tooling.Catalog, error)
}

type CapabilitiesService struct {
	cfg            *config.Config
	skills         skillSnapshotStore
	probeProviders providerStatusDoctor
	liveManager    liveMCPManager
	catalogBuilder toolCatalogBuilder
}

func NewCapabilitiesService(cfg *config.Config, skills skillSnapshotStore, probeProviders providerStatusDoctor, catalogBuilder toolCatalogBuilder) *CapabilitiesService {
	return &CapabilitiesService{
		cfg:            cfg,
		skills:         skills,
		probeProviders: probeProviders,
		catalogBuilder: catalogBuilder,
	}
}

func (s *CapabilitiesService) SetLiveManager(mgr liveMCPManager) {
	s.liveManager = mgr
}

func (s *CapabilitiesService) Snapshot(ctx context.Context, opts CapabilitySnapshotOptions) SystemCapabilities {
	if s == nil || s.cfg == nil {
		return SystemCapabilities{}
	}
	executionErr := s.cfg.ValidateExecutionReady()
	skills := s.snapshotSkills(ctx)
	providers := s.snapshotMCPProviders(ctx, opts)
	tools, catalogErr := s.snapshotTools(ctx, providers)
	healthyProviderCount := 0
	if opts.ProbeMCP || s.liveManager != nil {
		healthyProviderCount = healthyCapabilityProviderCount(providers)
	}
	runtimeReadinessReason := errorString(executionErr)
	if catalogErr != nil {
		if runtimeReadinessReason == "" {
			runtimeReadinessReason = catalogErr.Error()
		}
	}
	runtimeReadiness := buildRuntimeReadiness(runtimeReadinessReason)
	providerReadiness := buildProviderReadiness(providers)

	return SystemCapabilities{
		Summary: SystemCapabilitySummary{
			ToolCount:                  len(tools),
			EnabledToolCount:           enabledToolCount(tools),
			SkillCount:                 skills.Count,
			EligibleSkillCount:         skills.EligibleCount,
			IneligibleSkillCount:       skills.IneligibleCount,
			InvalidSkillCount:          skills.InvalidCount,
			MCPConfiguredProviderCount: len(providers),
			MCPEnabledProviderCount:    enabledCapabilityProviderCount(providers),
			MCPHealthyProviderCount:    healthyProviderCount,
		},
		Model: SystemModelCapabilities{
			Name: firstEnabledProviderModel(s.cfg),
		},
		RuntimeReadiness:  runtimeReadiness,
		Features:          SystemFeatureCapabilities{InterruptResume: true, TraceDebug: true, SessionHistory: true},
		ToolCatalogError:  errorString(catalogErr),
		Tools:             tools,
		Skills:            skills,
		MCPProviders:      providers,
		ProviderReadiness: providerReadiness,
	}
}

func (s *CapabilitiesService) snapshotTools(ctx context.Context, providers []SystemMCPProviderCapability) ([]SystemToolCapability, error) {
	workspaceRoot := ""
	runCommandTimeout := 0
	if ws, err := s.cfg.Workspace(); err == nil && ws != nil {
		workspaceRoot = ws.Root()
		runCommandTimeout = ws.RunCommandDefaultTimeout()
	}

	var specs []tooling.ToolSpec
	if s.catalogBuilder != nil {
		catalog, err := s.catalogBuilder.BuildCapabilityCatalog(ctx)
		if err != nil {
			return nil, fmt.Errorf("build tool catalog: %w", err)
		}
		specs = catalog.Specs()
	} else {
		specs = tooling.ConfiguredLocalSpecs(s.cfg)
	}

	items := make([]SystemToolCapability, 0, len(specs)+providerToolCount(providers))
	for _, spec := range specs {
		items = append(items, toolCapabilityFromSpec(spec, workspaceRoot, runCommandTimeout))
	}
	for _, provider := range providers {
		toolNames := provider.DiscoveredToolNames
		if len(toolNames) == 0 {
			toolNames = provider.ConfiguredToolNames
		}
		parallelPolicy, err := mcpProviderParallelPolicy(s.cfg, provider.Name)
		if err != nil {
			return nil, err
		}
		for _, toolName := range toolNames {
			items = append(items, SystemToolCapability{
				Name:           toolName,
				Source:         provider.Name,
				Kind:           string(tooling.ToolKindMCP),
				Category:       string(tooling.ToolCategoryIntegration),
				ResourceScope:  string(tooling.ResourceScopeMCP),
				Profiles:       []string{string(tooling.ToolProfileRun)},
				Enabled:        provider.Enabled && provider.Error == "",
				HealthState:    providerHealthState(provider),
				HealthReason:   strings.TrimSpace(provider.Error),
				ParallelPolicy: parallelPolicy,
				PlanPolicy:     string(tooling.PlanPolicyNone),
				FactPolicy:     string(tooling.FactPolicyAuto),
				Risk:           "integration",
			})
		}
	}
	return items, nil
}

func mcpProviderParallelPolicy(cfg *config.Config, providerName string) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("MCP provider %q requires configured tool_safety", strings.TrimSpace(providerName))
	}
	for _, provider := range cfg.MCP.Providers {
		if strings.TrimSpace(provider.Name) != strings.TrimSpace(providerName) {
			continue
		}
		policy, err := tooling.ParseParallelPolicy(provider.ToolSafety)
		if err != nil {
			return "", err
		}
		return string(policy), nil
	}
	return "", fmt.Errorf("MCP provider %q is not configured", strings.TrimSpace(providerName))
}

func toolCapabilityFromSpec(spec tooling.ToolSpec, workspaceRoot string, runCommandTimeout int) SystemToolCapability {
	item := SystemToolCapability{
		Name:           spec.Name,
		Source:         spec.Source,
		Kind:           string(spec.Kind),
		Category:       string(spec.Category),
		ResourceScope:  string(spec.ResourceScope),
		Profiles:       profileStrings(spec.Profiles),
		Enabled:        spec.Enabled(),
		HealthState:    string(spec.Health.State),
		HealthReason:   spec.Health.Reason,
		ParallelPolicy: string(spec.Execution.ParallelPolicy),
		PlanPolicy:     string(spec.PlanPolicy),
		FactPolicy:     string(spec.FactPolicy),
		Risk:           toolRisk(spec),
	}
	switch spec.ResourceScope {
	case tooling.ResourceScopeWorkspaceFile:
		item.RootDir = workspaceRoot
	case tooling.ResourceScopeWorkspaceCommand:
		item.WorkDir = workspaceRoot
		item.DefaultTimeout = runCommandTimeout
	}
	return item
}

func toolRisk(spec tooling.ToolSpec) string {
	switch spec.Category {
	case tooling.ToolCategoryRead, tooling.ToolCategoryInspect:
		return "read_only"
	case tooling.ToolCategoryWrite:
		return "mutation"
	case tooling.ToolCategoryExecute:
		return "escape_hatch"
	case tooling.ToolCategoryMemory:
		return "memory"
	case tooling.ToolCategorySkill:
		return "skill"
	default:
		return "integration"
	}
}

func profileStrings(items []tooling.ToolProfile) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, string(item))
	}
	return out
}

func providerHealthState(provider SystemMCPProviderCapability) string {
	switch {
	case !provider.Enabled:
		return string(tooling.HealthStateDisabled)
	case strings.TrimSpace(provider.Error) != "":
		return string(tooling.HealthStateDegraded)
	default:
		return string(tooling.HealthStateHealthy)
	}
}

func providerToolCount(providers []SystemMCPProviderCapability) int {
	total := 0
	for _, provider := range providers {
		if len(provider.DiscoveredToolNames) > 0 {
			total += len(provider.DiscoveredToolNames)
			continue
		}
		total += len(provider.ConfiguredToolNames)
	}
	return total
}

func (s *CapabilitiesService) snapshotSkills(ctx context.Context) SystemSkillCapabilities {
	if s == nil || s.skills == nil {
		return SystemSkillCapabilities{}
	}
	snapshot, err := s.skills.Snapshot(ctx)
	if err != nil {
		return SystemSkillCapabilities{
			LoadError: fmt.Sprintf("load stable skills: %v", err),
		}
	}
	out := make([]SystemSkillSummary, 0, len(snapshot.Skills))
	eligibleCount := 0
	for _, item := range snapshot.Skills {
		if item.Eligible {
			eligibleCount++
		}
		out = append(out, SystemSkillSummary{
			ID:              item.ID,
			Name:            item.Name,
			Version:         item.Version,
			Source:          item.Source,
			Origin:          string(item.Origin),
			TaskPattern:     item.TaskPattern,
			Summary:         item.Summary,
			PromotedFrom:    item.PromotedFrom,
			Eligible:        item.Eligible,
			DisabledReasons: append([]string(nil), item.DisabledReasons...),
		})
	}
	problems := make([]SystemSkillProblem, 0, len(snapshot.Problems))
	for _, item := range snapshot.Problems {
		problems = append(problems, SystemSkillProblem{
			ID:     item.ID,
			Name:   item.Name,
			Source: item.Source,
			Path:   item.Path,
			Error:  item.Error,
		})
	}
	return SystemSkillCapabilities{
		Count:           len(out),
		EligibleCount:   eligibleCount,
		IneligibleCount: len(out) - eligibleCount,
		InvalidCount:    len(problems),
		Items:           out,
		Problems:        problems,
	}
}

func (s *CapabilitiesService) snapshotMCPProviders(ctx context.Context, opts CapabilitySnapshotOptions) []SystemMCPProviderCapability {
	configured := configuredProviderConfigs(s.cfg)
	if len(configured) == 0 {
		return nil
	}
	var statuses []mcpprovider.ProviderStatus
	if s.liveManager != nil {
		statuses = s.liveManager.Statuses()
	} else if opts.ProbeMCP && s.probeProviders != nil {
		statuses = s.probeProviders(ctx, configured)
	} else {
		statuses = make([]mcpprovider.ProviderStatus, 0, len(configured))
		for _, cfg := range configured {
			statuses = append(statuses, mcpprovider.ProviderStatus{
				Name:                cfg.Name,
				Configured:          true,
				Enabled:             cfg.Enabled,
				Transport:           cfg.Transport,
				Command:             cfg.Command,
				Args:                append([]string(nil), cfg.Args...),
				WorkDir:             cfg.WorkDir,
				ConfiguredToolNames: append([]string(nil), cfg.ToolNames...),
			})
		}
	}
	out := make([]SystemMCPProviderCapability, 0, len(statuses))
	for _, status := range statuses {
		out = append(out, SystemMCPProviderCapability{
			Name:                status.Name,
			Configured:          status.Configured,
			Enabled:             status.Enabled,
			Transport:           status.Transport,
			StartupStatus:       status.StartupStatus,
			Command:             status.Command,
			Args:                append([]string(nil), status.Args...),
			WorkDir:             status.WorkDir,
			CommandPath:         status.CommandPath,
			ConfiguredToolNames: append([]string(nil), status.ConfiguredToolNames...),
			DiscoveredToolNames: append([]string(nil), status.DiscoveredToolNames...),
			ToolCount:           status.ToolCount,
			Error:               status.Error,
			AuthStatus:          status.AuthStatus,
		})
	}
	return out
}

func configuredProviderConfigs(cfg *config.Config) []mcpprovider.ProviderConfig {
	if cfg == nil {
		return nil
	}
	return mcpprovider.ProviderConfigsFromConfig(cfg.MCP.Providers)
}

func enabledToolCount(items []SystemToolCapability) int {
	count := 0
	for _, item := range items {
		if item.Enabled {
			count++
		}
	}
	return count
}

func enabledCapabilityProviderCount(items []SystemMCPProviderCapability) int {
	count := 0
	for _, item := range items {
		if item.Enabled {
			count++
		}
	}
	return count
}

func healthyCapabilityProviderCount(items []SystemMCPProviderCapability) int {
	count := 0
	for _, item := range items {
		if item.Enabled && item.Error == "" {
			count++
		}
	}
	return count
}

func firstEnabledProviderModel(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	for _, item := range cfg.Providers {
		if !item.Enabled {
			continue
		}
		if name := strings.TrimSpace(item.Name); name != "" {
			return name
		}
	}
	return ""
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

type RuntimeReadinessStatus string

const (
	RuntimeReadinessReady   RuntimeReadinessStatus = "ready"
	RuntimeReadinessBlocked RuntimeReadinessStatus = "blocked"
)

type ProviderReadinessStatus string

const (
	ProviderReadinessPassed  ProviderReadinessStatus = "passed"
	ProviderReadinessFailed  ProviderReadinessStatus = "failed"
	ProviderReadinessBlocked ProviderReadinessStatus = "blocked"
)

const providerReadinessScopeMCP = "mcp"

type RuntimeReadiness struct {
	Status RuntimeReadinessStatus `json:"status"`
	Reason string                 `json:"reason,omitempty"`
}

type ProviderReadinessSummary struct {
	Scope         string                  `json:"scope"`
	Provider      string                  `json:"provider"`
	Status        ProviderReadinessStatus `json:"status"`
	Reason        string                  `json:"reason,omitempty"`
	StartupStatus string                  `json:"startup_status,omitempty"`
	AuthStatus    string                  `json:"auth_status,omitempty"`
}

func buildRuntimeReadiness(blockedReason string) *RuntimeReadiness {
	reason := strings.TrimSpace(blockedReason)
	if reason == "" {
		return &RuntimeReadiness{Status: RuntimeReadinessReady}
	}
	return &RuntimeReadiness{
		Status: RuntimeReadinessBlocked,
		Reason: reason,
	}
}

func buildProviderReadiness(items []SystemMCPProviderCapability) []ProviderReadinessSummary {
	if len(items) == 0 {
		return nil
	}
	out := make([]ProviderReadinessSummary, 0, len(items))
	for _, item := range items {
		out = append(out, providerReadinessFromCapability(item))
	}
	return out
}

func providerReadinessFromCapability(provider SystemMCPProviderCapability) ProviderReadinessSummary {
	summary := ProviderReadinessSummary{
		Scope:         providerReadinessScopeMCP,
		Provider:      provider.Name,
		StartupStatus: strings.TrimSpace(provider.StartupStatus),
		AuthStatus:    strings.TrimSpace(provider.AuthStatus),
	}

	switch {
	case !provider.Configured:
		summary.Status = ProviderReadinessBlocked
		summary.Reason = "provider is not configured"
	case !provider.Enabled:
		summary.Status = ProviderReadinessBlocked
		summary.Reason = "provider is disabled"
	case strings.TrimSpace(provider.Error) != "":
		summary.Status = ProviderReadinessFailed
		summary.Reason = strings.TrimSpace(provider.Error)
	case summary.AuthStatus == "expired":
		summary.Status = ProviderReadinessFailed
		summary.Reason = "provider auth expired"
	case summary.StartupStatus == "":
		summary.Status = ProviderReadinessBlocked
		summary.Reason = "provider status has not been probed"
	case summary.StartupStatus == "failed" || summary.StartupStatus == "degraded":
		summary.Status = ProviderReadinessFailed
		summary.Reason = providerStartupReason(summary.StartupStatus)
	default:
		summary.Status = ProviderReadinessPassed
	}

	return summary
}

func providerStartupReason(status string) string {
	switch strings.TrimSpace(status) {
	case "failed":
		return "provider startup failed"
	case "degraded":
		return "provider startup degraded"
	default:
		return ""
	}
}

type DecisionService struct {
	profiles *decision.ProfileService
	store    *storesqlite.Store
}

func NewDecisionService(profiles *decision.ProfileService, store *storesqlite.Store) *DecisionService {
	return &DecisionService{profiles: profiles, store: store}
}

func (s *DecisionService) Profile(ctx context.Context) (*decision.ParsedProfile, error) {
	_ = ctx
	if s == nil || s.profiles == nil {
		return nil, fmt.Errorf("decision profile service is nil")
	}
	return s.profiles.Load()
}

func (s *DecisionService) InspectRun(ctx context.Context, runID string) (*decision.Record, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("decision store is nil")
	}
	return s.store.LoadRunDecision(ctx, strings.TrimSpace(runID))
}
