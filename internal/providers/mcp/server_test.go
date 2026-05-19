package mcpprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ycvk/acorn/internal/config"
)

// stubTool creates a simple InvokableTool with the given name and description.
func stubTool(name, desc string) einotool.BaseTool {
	t, err := toolutils.InferTool(name, desc, func(ctx context.Context, input struct{}) (string, error) {
		return fmt.Sprintf("%s result", name), nil
	})
	if err != nil {
		panic(fmt.Sprintf("stubTool(%q): %v", name, err))
	}
	return t
}

// toolNamesFromServer starts a test StreamableHTTP server for the given MCP
// server, connects a client, and returns the names of registered tools via
// ListTools.
func toolNamesFromServer(t *testing.T, srv *mcp.Server) []string {
	t.Helper()
	ctx := context.Background()

	handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return srv
	}, nil)
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: httpServer.URL}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func TestMCPServer_EmptyAllowlist_ReturnsZeroTools(t *testing.T) {
	tools := []einotool.BaseTool{
		stubTool("echo", "echo tool"),
		stubTool("add", "add tool"),
	}
	cfg := config.ServeConfig{} // empty allowlist

	srv, err := NewMCPServer(context.Background(), cfg, tools)
	if err != nil {
		t.Fatalf("NewMCPServer: %v", err)
	}

	names := toolNamesFromServer(t, srv)
	if len(names) != 0 {
		t.Fatalf("expected zero tools with empty allowlist, got %v", names)
	}
}

func TestMCPServer_AllowlistFiltersTools(t *testing.T) {
	tools := []einotool.BaseTool{
		stubTool("echo", "echo tool"),
		stubTool("add", "add tool"),
		stubTool("delete", "delete tool"),
	}
	cfg := config.ServeConfig{
		Tools: config.ServeToolsConfig{
			Allowlist: []string{"echo", "add"},
		},
	}

	srv, err := NewMCPServer(context.Background(), cfg, tools)
	if err != nil {
		t.Fatalf("NewMCPServer: %v", err)
	}

	names := toolNamesFromServer(t, srv)
	if len(names) != 2 {
		t.Fatalf("expected 2 tools, got %d: %v", len(names), names)
	}

	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}
	if !nameSet["echo"] || !nameSet["add"] {
		t.Fatalf("expected echo and add, got %v", names)
	}
	if nameSet["delete"] {
		t.Fatalf("delete should not be in allowlist, but was registered")
	}
}

func TestMCPServer_BlockedToolsRejectAllowlist(t *testing.T) {
	tools := []einotool.BaseTool{
		stubTool("run_command", "execute local commands"),
		stubTool("create_file", "create files"),
		stubTool("memory_query", "query memory"),
		stubTool("echo", "echo tool"),
	}
	cfg := config.ServeConfig{
		Tools: config.ServeToolsConfig{
			Allowlist: []string{"run_command", "echo", "create_file", "memory_query"},
		},
	}

	_, err := NewMCPServer(context.Background(), cfg, tools)
	if err == nil {
		t.Fatal("expected blocked allowlist entry to fail server creation")
	}
	if !strings.Contains(err.Error(), "blocked tool") {
		t.Fatalf("expected blocked-tool error, got %v", err)
	}
}

func TestMCPServer_AllBlockedToolsRejectAllowlist(t *testing.T) {
	tools := []einotool.BaseTool{
		stubTool("run_command", "execute local commands"),
		stubTool("create_file", "create files"),
	}
	cfg := config.ServeConfig{
		Tools: config.ServeToolsConfig{
			Allowlist: []string{"run_command", "create_file"},
		},
	}

	_, err := NewMCPServer(context.Background(), cfg, tools)
	if err == nil {
		t.Fatal("expected all-blocked allowlist to fail server creation")
	}
}

func TestMCPServer_AllowlistToolNotAvailable(t *testing.T) {
	tools := []einotool.BaseTool{
		stubTool("echo", "echo tool"),
	}
	cfg := config.ServeConfig{
		Tools: config.ServeToolsConfig{
			Allowlist: []string{"echo", "nonexistent_tool"},
		},
	}

	_, err := NewMCPServer(context.Background(), cfg, tools)
	if err == nil {
		t.Fatal("expected unavailable allowlist tool to fail server creation")
	}
	if !strings.Contains(err.Error(), "nonexistent_tool") {
		t.Fatalf("expected missing tool name in error, got %v", err)
	}
}

func TestMCPServer_ServerBlockedToolsSet(t *testing.T) {
	// Verify the compile-time blocklist contains exactly the required tools.
	expected := map[string]bool{
		"run_command":         true,
		"create_file":         true,
		"replace_span":        true,
		"apply_unified_patch": true,
		"memory_query":        true,
	}
	for name := range expected {
		if !serverBlockedTools[name] {
			t.Errorf("serverBlockedTools missing %q", name)
		}
	}
	if len(serverBlockedTools) != len(expected) {
		t.Errorf("serverBlockedTools has %d entries, expected %d", len(serverBlockedTools), len(expected))
	}
}

func TestMCPServer_ToolInfoErrorFails(t *testing.T) {
	badTool := &brokenInfoTool{}
	tools := []einotool.BaseTool{
		badTool,
		stubTool("echo", "echo tool"),
	}
	cfg := config.ServeConfig{
		Tools: config.ServeToolsConfig{
			Allowlist: []string{"broken", "echo"},
		},
	}

	_, err := NewMCPServer(context.Background(), cfg, tools)
	if err == nil {
		t.Fatal("expected tool info error to fail server creation")
	}
	if !strings.Contains(err.Error(), "broken tool info") {
		t.Fatalf("expected original info error, got %v", err)
	}
}

// brokenInfoTool is a BaseTool whose Info() always returns an error.
type brokenInfoTool struct{}

func (b *brokenInfoTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return nil, fmt.Errorf("broken tool info")
}

func (b *brokenInfoTool) InvokableRun(_ context.Context, _ string, _ ...einotool.Option) (string, error) {
	return "", nil
}

// --- Config validation tests ---

func TestValidateServeAllowlistRejectsDuplicates(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.SystemPrompt = "test"
	cfg.Serve.Tools.Allowlist = []string{"echo", "echo"}

	err := cfg.ValidateBase()
	if err == nil {
		t.Fatal("expected duplicate allowlist entries to fail validation")
	}
	if !strings.Contains(err.Error(), "serve.tools.allowlist contains duplicate") {
		t.Fatalf("expected duplicate allowlist validation error, got %v", err)
	}
}

func TestValidateServeAllowlistRejectsEmptyEntry(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.SystemPrompt = "test"
	cfg.Serve.Tools.Allowlist = []string{"echo", "  ", "add"}

	err := cfg.ValidateBase()
	if err == nil {
		t.Fatal("expected empty allowlist entry to fail validation")
	}
	if !strings.Contains(err.Error(), "serve.tools.allowlist contains empty entry") {
		t.Fatalf("expected empty entry validation error, got %v", err)
	}
}

func TestServeConfigYAMLAcceptedByKnownFields(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Serve = config.ServeConfig{
		Tools: config.ServeToolsConfig{
			Allowlist: []string{"echo"},
		},
	}
	if err := cfg.ValidateBase(); err != nil {
		t.Fatalf("ValidateBase with ServeConfig: %v", err)
	}
	if got, want := cfg.Serve.Tools.Allowlist, []string{"echo"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("Serve.Tools.Allowlist = %v, want %v", got, want)
	}
}

func TestValidateServeAllowlistTrimsEntries(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.SystemPrompt = "test"
	cfg.Serve.Tools.Allowlist = []string{" echo ", " add "}

	if err := cfg.ValidateBase(); err != nil {
		t.Fatalf("ValidateBase: %v", err)
	}
	if got, want := cfg.Serve.Tools.Allowlist, []string{"echo", "add"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Serve.Tools.Allowlist = %v, want %v", got, want)
	}
}
