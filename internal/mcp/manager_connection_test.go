package mcp

import (
	"context"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
)

func TestNewManagerLoadsToolsFromFixtureServer(t *testing.T) {
	binary := buildFixtureServer(t)
	mgr, err := NewManager(context.Background(), []ProviderConfig{{
		Name:                  "fixture",
		Enabled:               true,
		Transport:             "stdio",
		Command:               binary,
		StartupTimeoutSeconds: 10,
	}})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	tools := mgr.Tools()
	if got, want := len(tools), 2; got != want {
		t.Fatalf("expected %d tools, got %d", want, got)
	}

	echoTool := findToolByName(t, tools, "echo")
	invokable, ok := echoTool.(einotool.InvokableTool)
	if !ok {
		t.Fatal("echo tool is not invokable")
	}
	result, err := invokable.InvokableRun(context.Background(), `{"text":"hello"}`)
	if err != nil {
		t.Fatalf("invoke echo tool: %v", err)
	}
	if !strings.Contains(result, "echo: hello") {
		t.Fatalf("unexpected echo result: %s", result)
	}
}

func TestDoctorReportsDiscoveredTools(t *testing.T) {
	binary := buildFixtureServer(t)
	statuses := Doctor(context.Background(), []ProviderConfig{{
		Name:                  "fixture",
		Enabled:               true,
		Transport:             "stdio",
		Command:               binary,
		StartupTimeoutSeconds: 10,
		ToolNames:             []string{"add"},
	}})
	if got, want := len(statuses), 1; got != want {
		t.Fatalf("expected %d status, got %d", want, got)
	}
	status := statuses[0]
	if status.Error != "" {
		t.Fatalf("unexpected doctor error: %s", status.Error)
	}
	if !status.Configured {
		t.Fatal("expected configured provider status")
	}
	if !status.Enabled {
		t.Fatal("expected enabled provider status")
	}
	if got, want := strings.Join(status.ConfiguredToolNames, ","), "add"; got != want {
		t.Fatalf("expected configured tools %s, got %s", want, got)
	}
	if got, want := status.ToolCount, 1; got != want {
		t.Fatalf("expected tool count %d, got %d", want, got)
	}
	if got, want := strings.Join(status.DiscoveredToolNames, ","), "add"; got != want {
		t.Fatalf("expected discovered tools %s, got %s", want, got)
	}
	if status.CommandPath == "" {
		t.Fatal("expected resolved command path")
	}
}

func TestDoctorKeepsProviderVisibleOnFailure(t *testing.T) {
	statuses := Doctor(context.Background(), []ProviderConfig{{
		Name:                  "broken",
		Enabled:               true,
		Transport:             "stdio",
		Command:               "./does-not-exist-mcp",
		Args:                  []string{"--flag"},
		ToolNames:             []string{"echo"},
		StartupTimeoutSeconds: 10,
	}})
	if got, want := len(statuses), 1; got != want {
		t.Fatalf("expected %d status, got %d", want, got)
	}
	status := statuses[0]
	if !status.Configured {
		t.Fatal("expected configured provider status")
	}
	if !status.Enabled {
		t.Fatal("expected enabled provider status")
	}
	if status.Error == "" {
		t.Fatal("expected provider error to remain visible")
	}
	if got, want := strings.Join(status.ConfiguredToolNames, ","), "echo"; got != want {
		t.Fatalf("expected configured tools %s, got %s", want, got)
	}
	if len(status.DiscoveredToolNames) != 0 {
		t.Fatalf("expected zero discovered tools on failure, got %#v", status.DiscoveredToolNames)
	}
	if status.ToolCount != 0 {
		t.Fatalf("expected zero tool count on failure, got %d", status.ToolCount)
	}
}

func TestConnectProviderFailsWhenCommandPathCannotBeResolved(t *testing.T) {
	_, err := connectProvider(context.Background(), ProviderConfig{
		Name:                  "broken",
		Enabled:               true,
		Transport:             "stdio",
		Command:               "./does-not-exist-mcp",
		StartupTimeoutSeconds: 1,
	}, nil, nil, nil)
	if err == nil {
		t.Fatal("expected command resolution failure")
	}
	if !strings.Contains(err.Error(), "does-not-exist-mcp") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenProviderLogFileUsesPrivatePermissionsAndSanitizedName(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TMPDIR", tempRoot)
	t.Setenv("TMP", tempRoot)
	t.Setenv("TEMP", tempRoot)

	file, err := openProviderLogFile(" ../unsafe/name\\with..dots ")
	if err != nil {
		t.Fatalf("open provider log file: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })

	if got, want := filepath.Base(file.Name()), "unsafe-name-with-dots.log"; got != want {
		t.Fatalf("log filename = %q, want %q", got, want)
	}
	if got, want := filepath.Dir(file.Name()), filepath.Join(tempRoot, "acorn-mcp-logs"); got != want {
		t.Fatalf("log dir = %q, want %q", got, want)
	}

	if goruntime.GOOS == "windows" {
		return
	}

	info, err := file.Stat()
	if err != nil {
		t.Fatalf("stat log file: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("log file mode = %o, want %o", got, want)
	}
	dirInfo, err := os.Stat(filepath.Dir(file.Name()))
	if err != nil {
		t.Fatalf("stat log dir: %v", err)
	}
	if got, want := dirInfo.Mode().Perm(), os.FileMode(0o700); got != want {
		t.Fatalf("log dir mode = %o, want %o", got, want)
	}
}

func TestManagerCloseCleansUpProviders(t *testing.T) {
	binary := buildFixtureServer(t)
	mgr, err := NewManager(context.Background(), []ProviderConfig{{
		Name:                  "fixture",
		Enabled:               true,
		Transport:             "stdio",
		Command:               binary,
		StartupTimeoutSeconds: 10,
	}})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if err := mgr.Close(); err != nil {
		t.Fatalf("manager close: %v", err)
	}
	if err := mgr.Close(); err != nil {
		t.Fatalf("second manager close: %v", err)
	}
}

func TestNewManagerKeepsHealthyProvidersWhenOneFails(t *testing.T) {
	binary := buildFixtureServer(t)
	mgr, err := NewManager(context.Background(), []ProviderConfig{
		{
			Name:                  "healthy",
			Enabled:               true,
			Transport:             "stdio",
			Command:               binary,
			StartupTimeoutSeconds: 10,
		},
		{
			Name:                  "broken",
			Enabled:               true,
			Transport:             "stdio",
			Command:               "./does-not-exist-mcp",
			StartupTimeoutSeconds: 2,
		},
	})
	if err != nil {
		t.Fatalf("new manager should succeed with partial health: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	tools := mgr.Tools()
	if got, want := len(tools), 2; got != want {
		t.Fatalf("expected %d tools from healthy provider, got %d", want, got)
	}

	statuses := mgr.Statuses()
	if got, want := len(statuses), 2; got != want {
		t.Fatalf("expected %d statuses (1 healthy + 1 failed), got %d", want, got)
	}

	var healthyFound, failedFound bool
	for _, s := range statuses {
		if s.Name == "healthy" {
			healthyFound = true
			if s.Error != "" {
				t.Fatalf("healthy provider should not have error, got %q", s.Error)
			}
			if s.ToolCount != 2 {
				t.Fatalf("healthy provider tool count = %d, want 2", s.ToolCount)
			}
		}
		if s.Name == "broken" {
			failedFound = true
			if s.Error == "" {
				t.Fatal("broken provider should have error in status")
			}
		}
	}
	if !healthyFound {
		t.Fatal("healthy provider status not found")
	}
	if !failedFound {
		t.Fatal("failed provider status not found")
	}
}

func TestNewManagerReturnsErrorWhenAllProvidersFail(t *testing.T) {
	_, err := NewManager(context.Background(), []ProviderConfig{
		{
			Name:                  "broken1",
			Enabled:               true,
			Transport:             "stdio",
			Command:               "./does-not-exist-1",
			StartupTimeoutSeconds: 2,
		},
		{
			Name:                  "broken2",
			Enabled:               true,
			Transport:             "stdio",
			Command:               "./does-not-exist-2",
			StartupTimeoutSeconds: 2,
		},
	})
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
	if !strings.Contains(err.Error(), "broken1") || !strings.Contains(err.Error(), "broken2") {
		t.Fatalf("error should mention both providers, got %q", err.Error())
	}
}

func TestNewManagerAppliesPerProviderTimeout(t *testing.T) {
	start := time.Now()
	_, err := NewManager(context.Background(), []ProviderConfig{
		{
			Name:                  "timeout_provider",
			Enabled:               true,
			Transport:             "stdio",
			Command:               "sleep",
			Args:                  []string{"30"},
			StartupTimeoutSeconds: 1,
		},
	})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error for provider that cannot start within timeout")
	}
	if elapsed > 10*time.Second {
		t.Fatalf("provider startup took %v, expected it to timeout within a few seconds", elapsed)
	}
}
