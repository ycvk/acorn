package api

import (
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

func TestProviderToolsTakeTheProvidersReadiness(t *testing.T) {
	for _, tc := range []struct {
		name        string
		provider    SystemMCPProviderCapability
		wantHealth  core.HealthState
		wantEnabled bool
		wantPassed  bool
	}{
		{"healthy", SystemMCPProviderCapability{Configured: true, Enabled: true, StartupStatus: "healthy"}, core.HealthStateHealthy, true, true},
		{"not probed", SystemMCPProviderCapability{Configured: true, Enabled: true}, core.HealthStateUnknown, true, false},
		{"startup failed", SystemMCPProviderCapability{Configured: true, Enabled: true, StartupStatus: "failed"}, core.HealthStateDegraded, false, false},
		{"auth expired", SystemMCPProviderCapability{Configured: true, Enabled: true, StartupStatus: "healthy", AuthStatus: "expired"}, core.HealthStateDegraded, false, false},
		{"error", SystemMCPProviderCapability{Configured: true, Enabled: true, StartupStatus: "healthy", Error: "connect: refused"}, core.HealthStateDegraded, false, false},
		{"disabled", SystemMCPProviderCapability{Configured: true}, core.HealthStateDisabled, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.provider.Name = "fixture"
			tc.provider.ConfiguredToolNames = []string{"mcp__fixture__x"}
			items, err := (&CapabilitiesService{}).providerToolCapabilities(tc.provider)
			if err != nil {
				t.Fatal(err)
			}
			if got := items[0]; got.HealthState != string(tc.wantHealth) || got.Enabled != tc.wantEnabled {
				t.Fatalf("tool = %+v, want health %s enabled %t", got, tc.wantHealth, tc.wantEnabled)
			}
			passed := providerReadinessFromCapability(tc.provider).Status == ProviderReadinessPassed
			if passed != tc.wantPassed || (healthyCapabilityProviderCount([]SystemMCPProviderCapability{tc.provider}) == 1) != tc.wantPassed {
				t.Fatalf("readiness passed = %t, want %t", passed, tc.wantPassed)
			}
		})
	}
}
