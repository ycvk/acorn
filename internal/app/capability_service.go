package app

import (
	"context"
	"strings"

	"github.com/ycvk/acorn/internal/config"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/skills"
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
	Name           string   `json:"name"`
	Source         string   `json:"source"`
	Kind           string   `json:"kind"`
	Category       string   `json:"category"`
	Enabled        bool     `json:"enabled"`
	HealthState    string   `json:"health_state"`
	HealthReason   string   `json:"health_reason,omitempty"`
	ParallelPolicy string   `json:"parallel_policy,omitempty"`
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

type CapabilitiesService struct {
	cfg            *config.Config
	skills         skillSnapshotStore
	probeProviders providerStatusDoctor
	catalogBuilder *runtime.RunnerFactory
}

func NewCapabilitiesService(cfg *config.Config, skills skillSnapshotStore, probeProviders providerStatusDoctor, catalogBuilder *runtime.RunnerFactory) *CapabilitiesService {
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
