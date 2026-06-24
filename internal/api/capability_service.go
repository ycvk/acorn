package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/agent"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/port"
	mcpprovider "github.com/ycvk/acorn/internal/mcp"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tools"
)

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
	SessionHistory  bool `json:"session_history"`
}

type SystemToolCapability struct {
	Name           string `json:"name"`
	Source         string `json:"source"`
	Kind           string `json:"kind"`
	Category       string `json:"category"`
	Enabled        bool   `json:"enabled"`
	HealthState    string `json:"health_state"`
	HealthReason   string `json:"health_reason,omitempty"`
	ParallelPolicy string `json:"parallel_policy,omitempty"`
	Risk           string `json:"risk"`
	RootDir        string `json:"root_dir,omitempty"`
	WorkDir        string `json:"work_dir,omitempty"`
	DefaultTimeout int    `json:"default_timeout,omitempty"`
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

type providerStatusDoctor func(ctx context.Context, cfgs []mcpprovider.ProviderConfig) []mcpprovider.ProviderStatus

type CapabilitiesService struct {
	cfg            *config.Config
	skills         func(ctx context.Context) (*skills.Snapshot, error)
	probeProviders providerStatusDoctor
	catalogBuilder *agent.RunnerFactory
}

func NewCapabilitiesService(cfg *config.Config, skills func(ctx context.Context) (*skills.Snapshot, error), probeProviders providerStatusDoctor, catalogBuilder *agent.RunnerFactory) *CapabilitiesService {
	return &CapabilitiesService{
		cfg:            cfg,
		skills:         skills,
		probeProviders: probeProviders,
		catalogBuilder: catalogBuilder,
	}
}

func (s *CapabilitiesService) Snapshot(ctx context.Context, opts CapabilitySnapshotOptions) SystemCapabilities {
	if s == nil || s.cfg == nil {
		return SystemCapabilities{}
	}
	executionErr := s.cfg.ValidateExecutionReady()
	skillsCap := s.snapshotSkills(ctx)
	providers := s.snapshotMCPProviders(ctx, opts)
	tools, catalogErr := s.snapshotTools(ctx, providers)
	healthyProviderCount := 0
	if opts.ProbeMCP {
		healthyProviderCount = healthyCapabilityProviderCount(providers)
	}
	runtimeReadiness := buildRuntimeReadiness(s.runtimeReadinessReason(executionErr, catalogErr))
	providerReadiness := buildProviderReadiness(providers)

	return SystemCapabilities{
		Summary: SystemCapabilitySummary{
			ToolCount:                  len(tools),
			EnabledToolCount:           enabledToolCount(tools),
			SkillCount:                 skillsCap.Count,
			EligibleSkillCount:         skillsCap.EligibleCount,
			IneligibleSkillCount:       skillsCap.IneligibleCount,
			InvalidSkillCount:          skillsCap.InvalidCount,
			MCPConfiguredProviderCount: len(providers),
			MCPEnabledProviderCount:    enabledCapabilityProviderCount(providers),
			MCPHealthyProviderCount:    healthyProviderCount,
		},
		Model: SystemModelCapabilities{
			Name: firstEnabledProviderModel(s.cfg),
		},
		RuntimeReadiness:  runtimeReadiness,
		Features:          SystemFeatureCapabilities{InterruptResume: true, SessionHistory: true},
		ToolCatalogError:  errorString(catalogErr),
		Tools:             tools,
		Skills:            skillsCap,
		MCPProviders:      providers,
		ProviderReadiness: providerReadiness,
	}
}

func (s *CapabilitiesService) runtimeReadinessReason(executionErr error, catalogErr error) string {
	reason := errorString(executionErr)
	if catalogErr != nil {
		if reason == "" {
			reason = catalogErr.Error()
		}
	}
	return strings.TrimSpace(reason)
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

func (s *CapabilitiesService) snapshotTools(ctx context.Context, providers []SystemMCPProviderCapability) ([]SystemToolCapability, error) {
	workspaceRoot, runCommandTimeout := s.workspaceSettings()

	specs, err := s.loadToolSpecs(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]SystemToolCapability, 0, len(specs)+providerToolCount(providers))
	for _, spec := range specs {
		items = append(items, toolCapabilityFromSpec(spec, workspaceRoot, runCommandTimeout))
	}
	for _, provider := range providers {
		providerItems, err := s.providerToolCapabilities(provider)
		if err != nil {
			return nil, err
		}
		items = append(items, providerItems...)
	}
	return items, nil
}

func (s *CapabilitiesService) workspaceSettings() (string, int) {
	workspaceRoot := ""
	runCommandTimeout := 0
	if ws, err := s.cfg.Workspace(); err == nil && ws != nil {
		workspaceRoot = ws.Root()
		runCommandTimeout = ws.RunCommandDefaultTimeout()
	}
	return workspaceRoot, runCommandTimeout
}

func (s *CapabilitiesService) loadToolSpecs(ctx context.Context) ([]port.ToolSpec, error) {
	if s.catalogBuilder != nil {
		specs, err := s.catalogBuilder.BuildCapabilitySpecs(ctx)
		if err != nil {
			return nil, fmt.Errorf("build tool catalog: %w", err)
		}
		return specs, nil
	}
	return tools.ConfiguredLocalSpecs(s.cfg), nil
}

func (s *CapabilitiesService) providerToolCapabilities(provider SystemMCPProviderCapability) ([]SystemToolCapability, error) {
	toolNames := provider.DiscoveredToolNames
	if len(toolNames) == 0 {
		toolNames = provider.ConfiguredToolNames
	}
	parallelPolicy, err := mcpProviderParallelPolicy(s.cfg, provider.Name)
	if err != nil {
		return nil, err
	}
	items := make([]SystemToolCapability, 0, len(toolNames))
	for _, toolName := range toolNames {
		items = append(items, SystemToolCapability{
			Name:           toolName,
			Source:         provider.Name,
			Kind:           string(port.ToolKindMCP),
			Category:       string(port.ToolCategoryIntegration),
			Enabled:        provider.Enabled && provider.Error == "",
			HealthState:    providerHealthState(provider),
			HealthReason:   strings.TrimSpace(provider.Error),
			ParallelPolicy: parallelPolicy,
			Risk:           "integration",
		})
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
		policy, err := port.ParseParallelPolicy(provider.ToolSafety)
		if err != nil {
			return "", err
		}
		return string(policy), nil
	}
	return "", fmt.Errorf("MCP provider %q is not configured", strings.TrimSpace(providerName))
}

func toolCapabilityFromSpec(spec port.ToolSpec, workspaceRoot string, runCommandTimeout int) SystemToolCapability {
	item := SystemToolCapability{
		Name:           spec.Name,
		Source:         spec.Source,
		Kind:           string(spec.Kind),
		Category:       string(spec.Category),
		Enabled:        spec.Enabled(),
		HealthState:    string(spec.Health.State),
		HealthReason:   spec.Health.Reason,
		ParallelPolicy: string(spec.Execution.ParallelPolicy),
		Risk:           toolRisk(spec),
	}
	return item
}

func toolRisk(spec port.ToolSpec) string {
	switch spec.Category {
	case port.ToolCategoryRead, port.ToolCategoryInspect:
		return "read_only"
	case port.ToolCategoryWrite:
		return "mutation"
	case port.ToolCategoryExecute:
		return "escape_hatch"
	case port.ToolCategoryMemory:
		return "memory"
	case port.ToolCategorySkill:
		return "skill"
	default:
		return "integration"
	}
}

func providerHealthState(provider SystemMCPProviderCapability) string {
	switch {
	case !provider.Enabled:
		return string(port.HealthStateDisabled)
	case strings.TrimSpace(provider.Error) != "":
		return string(port.HealthStateDegraded)
	default:
		return string(port.HealthStateHealthy)
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
	snapshot, err := s.skills(ctx)
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
	statuses := s.resolveProviderStatuses(ctx, configured, opts)
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

func (s *CapabilitiesService) resolveProviderStatuses(ctx context.Context, configured []mcpprovider.ProviderConfig, opts CapabilitySnapshotOptions) []mcpprovider.ProviderStatus {
	if opts.ProbeMCP && s.probeProviders != nil {
		return s.probeProviders(ctx, configured)
	}
	statuses := make([]mcpprovider.ProviderStatus, 0, len(configured))
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
	return statuses
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
