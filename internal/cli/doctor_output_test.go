package cli

import (
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/app"
)

func mustContainAll(t *testing.T, body string, values []string) {
	t.Helper()
	for _, value := range values {
		if !strings.Contains(body, value) {
			t.Fatalf("expected %q to contain %q", body, value)
		}
	}
}

func TestRenderDoctorProviderLineWithTransportAndStatus(t *testing.T) {
	cases := []struct {
		name     string
		provider app.SystemMCPProviderCapability
		wantSubs []string
	}{
		{
			name: "healthy stdio provider",
			provider: app.SystemMCPProviderCapability{
				Name:          "my-server",
				Configured:    true,
				Enabled:       true,
				Transport:     "stdio",
				StartupStatus: "healthy",
				ToolCount:     3,
			},
			wantSubs: []string{"[stdio]", "healthy", "tools=3"},
		},
		{
			name: "failed sse provider",
			provider: app.SystemMCPProviderCapability{
				Name:          "remote-server",
				Configured:    true,
				Enabled:       true,
				Transport:     "sse",
				StartupStatus: "failed",
				Error:         "connection refused",
				ToolCount:     0,
			},
			wantSubs: []string{"[sse]", "failed", "tools=0"},
		},
		{
			name: "degraded streamable_http provider",
			provider: app.SystemMCPProviderCapability{
				Name:          "partial-server",
				Configured:    true,
				Enabled:       true,
				Transport:     "streamable_http",
				StartupStatus: "degraded",
				ToolCount:     1,
			},
			wantSubs: []string{"[streamable_http]", "degraded", "tools=1"},
		},
		{
			name: "disabled provider shows no transport",
			provider: app.SystemMCPProviderCapability{
				Name:       "disabled-server",
				Configured: true,
				Enabled:    false,
				Transport:  "stdio",
				ToolCount:  0,
			},
			wantSubs: []string{"disabled", "tools=0"},
		},
		{
			name: "empty transport is visible",
			provider: app.SystemMCPProviderCapability{
				Name:          "legacy-server",
				Configured:    true,
				Enabled:       true,
				Transport:     "",
				StartupStatus: "healthy",
				ToolCount:     2,
			},
			wantSubs: []string{"[missing transport]", "healthy", "tools=2"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line := renderDoctorProviderLine(tc.provider)
			mustContainAll(t, line, tc.wantSubs)
			if tc.provider.Enabled && tc.provider.Error != "" && !strings.Contains(line, tc.provider.Name) {
				t.Fatalf("expected provider name %q in line, got %q", tc.provider.Name, line)
			}
		})
	}
}

func TestRenderDoctorProviderLineErrorSubline(t *testing.T) {
	snapshot := app.SystemCapabilities{
		RuntimeReadiness: &app.RuntimeReadiness{Status: app.RuntimeReadinessReady},
		Summary: app.SystemCapabilitySummary{
			ToolCount:                  3,
			EnabledToolCount:           3,
			MCPConfiguredProviderCount: 1,
			MCPEnabledProviderCount:    1,
			MCPHealthyProviderCount:    0,
		},
		Tools: []app.SystemToolCapability{
			{Name: "read_file", Enabled: true, Risk: "read_only", Source: "local", Kind: "native", Category: "read", HealthState: "healthy"},
			{Name: "create_file", Enabled: true, Risk: "mutation", Source: "local", Kind: "native", Category: "write", HealthState: "healthy"},
			{Name: "run_command", Enabled: true, Risk: "escape_hatch", Source: "local", Kind: "native", Category: "execute", HealthState: "healthy"},
		},
		MCPProviders: []app.SystemMCPProviderCapability{
			{
				Name:          "failing-server",
				Configured:    true,
				Enabled:       true,
				Transport:     "sse",
				StartupStatus: "failed",
				Error:         "discover MCP tools: connection refused",
				ToolCount:     0,
			},
		},
	}

	summary := renderDoctorSummary(snapshot)
	mustContainAll(t, summary, []string{
		"[sse] failed",
		"Error:",
		"discover MCP tools: connection refused",
	})
}

func TestRenderDoctorProviderLineDoesNotPrintCircuitState(t *testing.T) {
	line := renderDoctorProviderLine(app.SystemMCPProviderCapability{
		Name:          "provider",
		Configured:    true,
		Enabled:       true,
		Transport:     "stdio",
		StartupStatus: "failed",
		Error:         "connection refused",
		ToolCount:     0,
		AuthStatus:    "expired",
	})
	mustContainAll(t, line, []string{"provider", "failed", "auth=expired"})
	if strings.Contains(line, "circuit=") || strings.Contains(line, "last_reconnect=") || strings.Contains(line, "failures=") {
		t.Fatalf("doctor provider line must not print removed circuit fields: %q", line)
	}
}
