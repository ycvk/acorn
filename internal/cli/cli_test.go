package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/app"
	"github.com/ycvk/acorn/internal/memorymodule"
)

func TestRunWithoutArgsReturnsUsage(t *testing.T) {
	err := Run(context.Background(), nil)
	if err == nil {
		t.Fatal("Run() without args should return usage error")
	}
	for _, want := range []string{"Usage:", "acorn doctor", "acorn pair", "acorn run", "acorn serve", "acorn memory semantic rebuild"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Run() usage error should contain %q, got %q", want, err.Error())
		}
	}
	for _, removed := range []string{"acorn chat", "acorn continue"} {
		if strings.Contains(err.Error(), removed) {
			t.Fatalf("Run() usage error should not contain removed command %q, got %q", removed, err.Error())
		}
	}
}

func TestRunRejectsTopLevelFlagsWithoutCommand(t *testing.T) {
	err := Run(context.Background(), []string{"-c", "configs/acorn.local.yaml"})
	if err == nil {
		t.Fatal("Run() with top-level flags should return usage error")
	}
	if !strings.Contains(err.Error(), "Usage:") {
		t.Fatalf("Run() should return usage text, got %q", err.Error())
	}
}

func TestRunUsageIncludesOperatorCommands(t *testing.T) {
	body := usageText()
	for _, want := range []string{
		`acorn doctor [-c path] [--json]`,
		`acorn pair [-c path] [--json] [--qr] [--ttl duration] [--server-url url]`,
		`acorn memory semantic rebuild [-c path] [--json]`,
		`acorn serve [-c path] [--listen addr]`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("usageText should contain %q, got:\n%s", want, body)
		}
	}
	if !strings.Contains(body, `acorn run [-c path] [--json] "task input"`) {
		t.Fatalf("usageText should contain acorn run, got:\n%s", body)
	}
	for _, removed := range []string{"acorn chat", "acorn continue"} {
		if strings.Contains(body, removed) {
			t.Fatalf("usageText should not contain removed command %q, got:\n%s", removed, body)
		}
	}
}

func TestDefaultConfigPathUsesHomeAcorn(t *testing.T) {
	if defaultConfigPath != "~/.acorn/acorn.yaml" {
		t.Fatalf("defaultConfigPath = %q, want ~/.acorn/acorn.yaml", defaultConfigPath)
	}
}

func TestRenderSemanticRebuildResult(t *testing.T) {
	out := renderSemanticRebuildResult(&memorymodule.SemanticRebuildResult{
		IndexName:    "memory_records",
		Schema:       memorymodule.SemanticSchemaMemoryRecordsV1,
		Model:        "text-embedding-3-small",
		Dimensions:   1536,
		IndexedCount: 7,
		DeletedCount: 1,
		SkippedCount: 2,
	})
	for _, want := range []string{
		"Semantic index rebuild complete",
		"Index: memory_records",
		"Schema: memory_records_v1",
		"Model: text-embedding-3-small",
		"Dimensions: 1536",
		"Indexed: 7",
		"Deleted: 1",
		"Skipped: 2",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderSemanticRebuildResult missing %q:\n%s", want, out)
		}
	}
}

func TestRunPairRejectsNonPositiveTTL(t *testing.T) {
	err := runPair(context.Background(), []string{"--ttl", "0s"})
	if err == nil || err.Error() != "pair ttl must be positive" {
		t.Fatalf("runPair ttl error = %v, want pair ttl must be positive", err)
	}
}

func TestRunPairRejectsQRWithoutServerURL(t *testing.T) {
	err := runPair(context.Background(), []string{"--qr"})
	if err == nil || err.Error() != "pair --qr requires --server-url" {
		t.Fatalf("runPair --qr error = %v, want pair --qr requires --server-url", err)
	}
}

func TestRunPairRejectsJSONAndQRTogether(t *testing.T) {
	err := runPair(context.Background(), []string{"--json", "--qr", "--server-url", "https://acorn.example.com"})
	if err == nil || err.Error() != "pair --json and --qr cannot be used together" {
		t.Fatalf("runPair --json --qr error = %v, want conflict", err)
	}
}

func TestPairPayloadJSONRequiresServerURL(t *testing.T) {
	if _, err := pairPayloadJSON(pairCommandOutput{PairingCode: "ABCD", ExpiresAt: "2026-05-15T00:00:00Z"}); err == nil {
		t.Fatal("pairPayloadJSON should require server_url")
	}
}

func TestPrintPairOutputJSON(t *testing.T) {
	var buf bytes.Buffer
	err := printPairOutput(&buf, pairCommandOutput{
		PairingCode: "ABCD-EFGH",
		ExpiresAt:   "2026-05-15T00:00:00Z",
		ServerURL:   "https://acorn.example.com",
	}, true, false)
	if err != nil {
		t.Fatalf("printPairOutput json error = %v", err)
	}
	for _, want := range []string{
		`"pairing_code": "ABCD-EFGH"`,
		`"expires_at": "2026-05-15T00:00:00Z"`,
		`"server_url": "https://acorn.example.com"`,
	} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("printPairOutput json should contain %q, got:\n%s", want, buf.String())
		}
	}
}

func TestPrintPairOutputQRIncludesPlainFields(t *testing.T) {
	var buf bytes.Buffer
	err := printPairOutput(&buf, pairCommandOutput{
		PairingCode: "ABCD-EFGH",
		ExpiresAt:   "2026-05-15T00:00:00Z",
		ServerURL:   "https://acorn.example.com",
	}, false, true)
	if err != nil {
		t.Fatalf("printPairOutput qr error = %v", err)
	}
	for _, want := range []string{
		"Pairing QR payload:",
		"Server URL: https://acorn.example.com",
		"Pairing code: ABCD-EFGH",
		"Expires at: 2026-05-15T00:00:00Z",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("printPairOutput qr should contain %q, got:\n%s", want, buf.String())
		}
	}
}

func TestRenderDoctorSummaryIncludesGroupedSectionsAndProviderErrors(t *testing.T) {
	summary := renderDoctorSummary(app.SystemCapabilities{
		Summary: app.SystemCapabilitySummary{
			ToolCount:                  3,
			SkillCount:                 2,
			MCPConfiguredProviderCount: 2,
			MCPEnabledProviderCount:    2,
			MCPHealthyProviderCount:    1,
		},
		Model:            app.SystemModelCapabilities{Name: "gpt-4.1-mini"},
		RuntimeReadiness: &app.RuntimeReadiness{Status: app.RuntimeReadinessBlocked, Reason: "model.api_key is required"},
		Tools: []app.SystemToolCapability{
			{Name: "read_file", Enabled: true, Risk: "read_only", Source: "local", Kind: "native", Category: "read", HealthState: "healthy"},
			{Name: "run_command", Enabled: false, Risk: "escape_hatch", Source: "local", Kind: "native", Category: "execute", HealthState: "disabled"},
		},
		Skills: app.SystemSkillCapabilities{
			Count:           2,
			EligibleCount:   1,
			IneligibleCount: 1,
			InvalidCount:    1,
			Items: []app.SystemSkillSummary{
				{ID: "skill.inspect.repo", Eligible: true, PromotedFrom: "inspect-repo"},
				{ID: "skill.ship.patch", Eligible: false, DisabledReasons: []string{"missing run_command"}},
			},
			Problems: []app.SystemSkillProblem{
				{ID: "skill.bad.frontmatter", Source: "workspace", Error: "invalid yaml"},
			},
		},
		MCPProviders: []app.SystemMCPProviderCapability{
			{Name: "healthy", Configured: true, Enabled: true, Transport: "stdio", StartupStatus: "healthy", ToolCount: 2, DiscoveredToolNames: []string{"echo", "inspect"}},
			{Name: "broken", Configured: true, Enabled: true, Transport: "stdio", StartupStatus: "failed", ToolCount: 0, ConfiguredToolNames: []string{"search"}, Error: "discover MCP tools: boom"},
		},
	}, "")

	for _, want := range []string{
		"Execution",
		"Tools",
		"Skills",
		"MCP providers",
		"Ready: not ready",
		"Error: model.api_key is required",
		"Promoted from: inspect-repo",
		"skill.ship.patch: ineligible (missing run_command)",
		"skill.bad.frontmatter source=",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("doctor summary should contain %q, got:\n%s", want, summary)
		}
	}
	for _, want := range []string{
		"healthy: [stdio] healthy tools=2 discovered=echo,inspect",
		"broken: [stdio] failed tools=0 configured=search",
		"Error: discover MCP tools: boom",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("doctor summary should contain provider detail %q, got:\n%s", want, summary)
		}
	}
}

func TestDoctorJSONModeKeepsCanonicalCapabilitySchema(t *testing.T) {
	body, err := json.Marshal(app.SystemCapabilities{
		Summary: app.SystemCapabilitySummary{
			MCPConfiguredProviderCount: 1,
		},
		Tools: []app.SystemToolCapability{{Name: "read_file", Enabled: true, Risk: "read_only", Source: "local", Kind: "native", Category: "read", HealthState: "healthy"}},
		Skills: app.SystemSkillCapabilities{
			Count: 1,
			Items: []app.SystemSkillSummary{{ID: "skill.inspect.repo", Eligible: true, PromotedFrom: "inspect-repo"}},
		},
		MCPProviders: []app.SystemMCPProviderCapability{
			{Name: "broken", Configured: true, Enabled: true, Error: "provider failed"},
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal(SystemCapabilities) error = %v", err)
	}
	jsonText := string(body)
	for _, want := range []string{
		"\"summary\"",
		"\"tools\"",
		"\"skills\"",
		"\"mcp_providers\"",
		"\"error\":\"provider failed\"",
		"\"id\":\"skill.inspect.repo\"",
		"\"promoted_from\":\"inspect-repo\"",
	} {
		if !strings.Contains(jsonText, want) {
			t.Fatalf("doctor json should contain %q, got %s", want, jsonText)
		}
	}
}
