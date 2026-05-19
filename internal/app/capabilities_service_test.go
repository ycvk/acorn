package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/config"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/skills"
)

func TestCapabilitiesSnapshotContract(t *testing.T) {
	t.Run("execution not ready still returns catalog snapshot", func(t *testing.T) {
		service := NewCapabilitiesService(baseCapabilitiesConfig(""), fakeSkillSnapshotStore{
			snapshot: skills.Snapshot{
				Skills: []skills.View{{
					Spec:     skills.Spec{ID: "inspect_repo", Name: "Inspect Repo", Source: "workspace"},
					Eligible: true,
				}},
			},
		}, nil, nil)

		snapshot := service.Snapshot(context.Background(), CapabilitySnapshotOptions{})

		if snapshot.RuntimeReadiness == nil {
			t.Fatal("expected runtime readiness to be populated")
		}
		if got, want := snapshot.RuntimeReadiness.Status, RuntimeReadinessBlocked; got != want {
			t.Fatalf("runtime readiness status = %q, want %q", got, want)
		}
		if !strings.Contains(snapshot.RuntimeReadiness.Reason, "api_key is required") {
			t.Fatalf("expected runtime readiness reason to mention missing api key, got %q", snapshot.RuntimeReadiness.Reason)
		}
		if got, want := len(snapshot.Tools), 10; got != want {
			t.Fatalf("tool count = %d, want %d", got, want)
		}
		if got, want := snapshot.Skills.Count, 1; got != want {
			t.Fatalf("skills count = %d, want %d", got, want)
		}
	})

	t.Run("disabled skill reasons remain visible", func(t *testing.T) {
		service := NewCapabilitiesService(baseCapabilitiesConfig("test-key"), fakeSkillSnapshotStore{
			snapshot: skills.Snapshot{
				Skills: []skills.View{{
					Spec:            skills.Spec{ID: "edit_repo", Name: "Edit Repo", Source: "workspace"},
					Eligible:        false,
					DisabledReasons: []string{"run_command is disabled", "missing rg"},
				}},
			},
		}, nil, nil)

		snapshot := service.Snapshot(context.Background(), CapabilitySnapshotOptions{})

		if got, want := snapshot.Skills.IneligibleCount, 1; got != want {
			t.Fatalf("ineligible_count = %d, want %d", got, want)
		}
		if got, want := strings.Join(snapshot.Skills.Items[0].DisabledReasons, ","), "run_command is disabled,missing rg"; got != want {
			t.Fatalf("disabled reasons = %q, want %q", got, want)
		}
	})

	t.Run("provider failure visibility keeps healthy providers", func(t *testing.T) {
		cfg := baseCapabilitiesConfig("test-key")
		cfg.MCP.Providers = []config.MCPProviderConfig{
			{
				Name:                  "healthy",
				Enabled:               true,
				Transport:             "stdio",
				Command:               "/bin/healthy",
				Args:                  []string{"--stdio"},
				WorkDir:               "/tmp/healthy",
				ToolNames:             []string{"configured_echo"},
				StartupTimeoutSeconds: 10,
				ToolSafety:            "read_only",
			},
			{
				Name:                  "broken",
				Enabled:               true,
				Transport:             "stdio",
				Command:               "/bin/broken",
				ToolNames:             []string{"configured_add"},
				StartupTimeoutSeconds: 10,
				ToolSafety:            "read_only",
			},
		}
		service := NewCapabilitiesService(cfg, fakeSkillSnapshotStore{}, func(ctx context.Context, providers []mcpprovider.ProviderConfig) []mcpprovider.ProviderStatus {
			if got, want := len(providers), 2; got != want {
				t.Fatalf("provider count passed to doctor = %d, want %d", got, want)
			}
			return []mcpprovider.ProviderStatus{
				{
					Name:                "healthy",
					Configured:          true,
					Enabled:             true,
					Command:             "/bin/healthy",
					Args:                []string{"--stdio"},
					WorkDir:             "/tmp/healthy",
					CommandPath:         "/bin/healthy",
					ConfiguredToolNames: []string{"configured_echo"},
					DiscoveredToolNames: []string{"echo"},
					ToolCount:           1,
				},
				{
					Name:                "broken",
					Configured:          true,
					Enabled:             true,
					Command:             "/bin/broken",
					CommandPath:         "/bin/broken",
					ConfiguredToolNames: []string{"configured_add"},
					Error:               "discover MCP tools: boom",
				},
			}
		}, nil)

		snapshot := service.Snapshot(context.Background(), CapabilitySnapshotOptions{ProbeMCP: true})

		if got, want := len(snapshot.MCPProviders), 2; got != want {
			t.Fatalf("mcp provider count = %d, want %d", got, want)
		}
		if snapshot.MCPProviders[1].Error == "" {
			t.Fatal("expected broken provider error to remain visible")
		}
		if got, want := snapshot.Summary.MCPHealthyProviderCount, 1; got != want {
			t.Fatalf("healthy provider count = %d, want %d", got, want)
		}
		if !hasTool(snapshot.Tools, "echo") {
			t.Fatal("expected discovered MCP tool to appear in tool catalog snapshot")
		}
		if snapshot.RuntimeReadiness == nil {
			t.Fatal("expected runtime readiness to be populated")
		}
		if got, want := snapshot.RuntimeReadiness.Status, RuntimeReadinessReady; got != want {
			t.Fatalf("runtime readiness status = %q, want %q", got, want)
		}
		failed := providerReadinessByName(snapshot.ProviderReadiness, "broken")
		if failed == nil {
			t.Fatal("expected broken provider readiness summary")
		}
		if got, want := failed.Status, ProviderReadinessFailed; got != want {
			t.Fatalf("broken provider readiness = %q, want %q", got, want)
		}
		if !strings.Contains(failed.Reason, "boom") {
			t.Fatalf("broken provider readiness reason = %q, want provider error", failed.Reason)
		}
	})
}

func TestCapabilitiesSnapshotProbeMCPOnlyWhenExplicit(t *testing.T) {
	cfg := baseCapabilitiesConfig("test-key")
	cfg.MCP.Providers = []config.MCPProviderConfig{{
		Name:                  "fixture",
		Enabled:               true,
		Transport:             "stdio",
		Command:               "/bin/fixture",
		ToolNames:             []string{"configured_echo"},
		StartupTimeoutSeconds: 10,
		ToolSafety:            "read_only",
	}}
	probeCalls := 0
	service := NewCapabilitiesService(cfg, fakeSkillSnapshotStore{}, func(ctx context.Context, providers []mcpprovider.ProviderConfig) []mcpprovider.ProviderStatus {
		probeCalls++
		return nil
	}, nil)

	snapshot := service.Snapshot(context.Background(), CapabilitySnapshotOptions{})

	if probeCalls != 0 {
		t.Fatalf("expected passive snapshot not to probe MCP providers, got %d calls", probeCalls)
	}
	if got, want := len(snapshot.MCPProviders), 1; got != want {
		t.Fatalf("mcp provider count = %d, want %d", got, want)
	}
	if got, want := snapshot.Summary.MCPHealthyProviderCount, 0; got != want {
		t.Fatalf("healthy provider count = %d, want %d without probing", got, want)
	}
	blocked := providerReadinessByName(snapshot.ProviderReadiness, "fixture")
	if blocked == nil {
		t.Fatal("expected fixture provider readiness summary")
	}
	if got, want := blocked.Status, ProviderReadinessBlocked; got != want {
		t.Fatalf("fixture provider readiness = %q, want %q", got, want)
	}
	if got, want := blocked.Reason, "provider status has not been probed"; got != want {
		t.Fatalf("fixture provider readiness reason = %q, want %q", got, want)
	}
}

func TestCapabilitiesSnapshotExposesSkillLoadError(t *testing.T) {
	service := NewCapabilitiesService(baseCapabilitiesConfig("test-key"), failingSkillSnapshotStore{err: errors.New("loader failed")}, nil, nil)

	snapshot := service.Snapshot(context.Background(), CapabilitySnapshotOptions{})

	if snapshot.Skills.LoadError == "" {
		t.Fatal("expected non-empty skill load error")
	}
}

func TestCapabilitiesSnapshotReadsLiveManager(t *testing.T) {
	cfg := baseCapabilitiesConfig("test-key")
	cfg.MCP.Providers = []config.MCPProviderConfig{
		{
			Name:                  "live-provider",
			Enabled:               true,
			Transport:             "stdio",
			Command:               "/bin/live",
			ToolNames:             []string{"configured_live"},
			StartupTimeoutSeconds: 10,
			ToolSafety:            "read_only",
		},
	}

	probeCalls := 0
	service := NewCapabilitiesService(cfg, fakeSkillSnapshotStore{}, func(ctx context.Context, providers []mcpprovider.ProviderConfig) []mcpprovider.ProviderStatus {
		probeCalls++
		return nil
	}, nil)
	service.SetLiveManager(&fakeLiveMCPManager{
		statuses: []mcpprovider.ProviderStatus{{
			Name:                "live-provider",
			Configured:          true,
			Enabled:             true,
			Command:             "/bin/live",
			StartupStatus:       "healthy",
			DiscoveredToolNames: []string{"live_tool"},
			ToolCount:           1,
		}},
	})

	snapshot := service.Snapshot(context.Background(), CapabilitySnapshotOptions{})

	if probeCalls != 0 {
		t.Fatalf("expected live manager to bypass probe, got %d probe calls", probeCalls)
	}
	if got, want := snapshot.Summary.MCPHealthyProviderCount, 1; got != want {
		t.Fatalf("healthy provider count = %d, want %d", got, want)
	}
	if !hasTool(snapshot.Tools, "live_tool") {
		t.Fatal("expected live MCP tool to appear in tool catalog snapshot")
	}
	passed := providerReadinessByName(snapshot.ProviderReadiness, "live-provider")
	if passed == nil {
		t.Fatal("expected live provider readiness summary")
	}
	if got, want := passed.Status, ProviderReadinessPassed; got != want {
		t.Fatalf("live provider readiness = %q, want %q", got, want)
	}
}

func TestCapabilitiesSnapshotRuntimeReadinessBlocked(t *testing.T) {
	service := NewCapabilitiesService(baseCapabilitiesConfig(""), fakeSkillSnapshotStore{}, nil, nil)

	snapshot := service.Snapshot(context.Background(), CapabilitySnapshotOptions{})

	if snapshot.RuntimeReadiness == nil {
		t.Fatal("expected runtime readiness to be populated")
	}
	if got, want := snapshot.RuntimeReadiness.Status, RuntimeReadinessBlocked; got != want {
		t.Fatalf("runtime readiness status = %q, want %q", got, want)
	}
	if !strings.Contains(snapshot.RuntimeReadiness.Reason, "api_key is required") {
		t.Fatalf("runtime readiness reason = %q, want missing api key", snapshot.RuntimeReadiness.Reason)
	}
}

type fakeLiveMCPManager struct {
	statuses []mcpprovider.ProviderStatus
}

func (m *fakeLiveMCPManager) Statuses() []mcpprovider.ProviderStatus {
	if m == nil {
		return nil
	}
	return append([]mcpprovider.ProviderStatus(nil), m.statuses...)
}

type failingSkillSnapshotStore struct {
	err error
}

type fakeSkillSnapshotStore struct {
	snapshot skills.Snapshot
}

func (s failingSkillSnapshotStore) Snapshot(ctx context.Context) (*skills.Snapshot, error) {
	return nil, s.err
}

func (s fakeSkillSnapshotStore) Snapshot(ctx context.Context) (*skills.Snapshot, error) {
	return new(skills.CopySnapshot(s.snapshot)), nil
}

func baseCapabilitiesConfig(apiKey string) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Providers = []config.ProviderConfig{{
		Enabled:             true,
		Name:                "openai",
		Model:               "gpt-4.1-mini",
		BaseURL:             "https://api.openai.com/v1",
		APIKey:              apiKey,
		TimeoutSeconds:      30,
		MaxCompletionTokens: 1024,
	}}
	cfg.Runtime.StorageDir = ".acorn"
	cfg.Web.ListenAddr = "127.0.0.1:8080"
	cfg.Agent.Name = "acorn"
	cfg.Agent.MaxIterations = 8
	cfg.Tools.Workspace.RootDir = "."
	cfg.Tools.Mutation.RootDir = "."
	cfg.Tools.RunCommand.WorkDir = "."
	cfg.Tools.RunCommand.DefaultTimeout = 30
	cfg.Context.WindowTokens = 200000
	cfg.Context.CompactMarginTokens = 13000
	cfg.Context.PreserveRecentTurns = 3
	cfg.Context.SummaryMaxTokens = 2048
	cfg.Memory.Search.TokenBudget = 2000
	cfg.Memory.Semantic.Embedding.APIKey = apiKey
	return cfg
}

func hasTool(items []SystemToolCapability, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}

func providerReadinessByName(items []ProviderReadinessSummary, name string) *ProviderReadinessSummary {
	for i := range items {
		if items[i].Provider == name {
			return &items[i]
		}
	}
	return nil
}
