package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/port"
	"github.com/ycvk/acorn/internal/tools"
)

func TestMCPToolName(t *testing.T) {
	tests := []struct {
		provider string
		tool     string
		want     string
	}{
		{"github", "search_issues", "mcp__github__search_issues"},
		{"notion-mcp", "create_page", "mcp__notion-mcp__create_page"},
		{"My Server", "run", "mcp__My-Server__run"},
		{"", "run", "mcp__provider__run"},
		{"a!@#b", "tool", "mcp__a---b__tool"},
	}
	for _, tt := range tests {
		got := mcpToolName(tt.provider, tt.tool)
		if got != tt.want {
			t.Errorf("mcpToolName(%q, %q) = %q, want %q", tt.provider, tt.tool, got, tt.want)
		}
	}
}

func TestSanitizeMCPProviderName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"github", "github"},
		{"notion-mcp", "notion-mcp"},
		{"My Server", "My-Server"},
		{"", "provider"},
		{"   ", "provider"},
		{"...leading", "leading"},
		{"trailing...", "trailing"},
		{"___under", "under"},
		{"a!@#$b", "a----b"},
	}
	for _, tt := range tests {
		got := sanitizeMCPProviderName(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeMCPProviderName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestAugmentDescription(t *testing.T) {
	tests := []struct {
		desc     string
		provider string
		want     string
	}{
		{"Search issues", "github", "Search issues\n\nProvided by MCP server: github"},
		{"", "github", "Provided by MCP server: github"},
	}
	for _, tt := range tests {
		got := augmentDescription(tt.desc, tt.provider)
		if got != tt.want {
			t.Errorf("augmentDescription(%q, %q) = %q, want %q", tt.desc, tt.provider, got, tt.want)
		}
	}
}

type namingStubTool struct {
	name string
	desc string
}

func (s namingStubTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: s.name, Desc: s.desc}, nil
}

func (s namingStubTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	return "stub-result", nil
}

func TestMCPNamespacedToolInfo(t *testing.T) {
	inner := namingStubTool{name: "search_issues", desc: "Search for issues"}
	wrapped, err := NewMCPNamespacedTool(context.Background(), inner, "github", "search_issues")
	if err != nil {
		t.Fatalf("NewMCPNamespacedTool: %v", err)
	}
	info, err := wrapped.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Name != "mcp__github__search_issues" {
		t.Errorf("Name = %q, want %q", info.Name, "mcp__github__search_issues")
	}
	if !strings.Contains(info.Desc, "Search for issues") {
		t.Errorf("Desc missing original: %q", info.Desc)
	}
	if !strings.Contains(info.Desc, "Provided by MCP server: github") {
		t.Errorf("Desc missing attribution: %q", info.Desc)
	}
}

func TestMCPNamespacedToolInvokableRunDelegatesToInner(t *testing.T) {
	inner := namingStubTool{name: "search_issues", desc: "Search for issues"}
	wrapped, err := NewMCPNamespacedTool(context.Background(), inner, "github", "search_issues")
	if err != nil {
		t.Fatalf("NewMCPNamespacedTool: %v", err)
	}
	result, err := wrapped.InvokableRun(context.Background(), `{"q":"test"}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if result != "stub-result" {
		t.Errorf("InvokableRun = %q, want %q", result, "stub-result")
	}
}

func TestBuildCapabilityRegistryMCPNamespace(t *testing.T) {
	registrations := []mcpprovider.ToolRegistration{
		{ProviderName: "github", Tool: namingStubTool{name: "search_issues", desc: "Search issues"}},
		{ProviderName: "notion", Tool: namingStubTool{name: "create_page", desc: "Create a page"}},
	}
	registry, err := buildCapabilityRegistryForTest(context.Background(), nil, registrations, nil, nil)
	if err != nil {
		t.Fatalf("buildCapabilityRegistry: %v", err)
	}
	caps := registry.Specs()
	if len(caps) != 2 {
		t.Fatalf("expected 2 capabilities, got %d", len(caps))
	}
	for _, cap := range caps {
		info, err := cap.Tool.Info(context.Background())
		if err != nil {
			t.Fatalf("Info: %v", err)
		}
		if !strings.HasPrefix(info.Name, "mcp__") {
			t.Errorf("MCP tool name %q should have mcp__ prefix", info.Name)
		}
		if !strings.Contains(info.Desc, "Provided by MCP server:") {
			t.Errorf("MCP tool desc should have provider attribution: %q", info.Desc)
		}
	}
}

func TestBuildCapabilityRegistryLocalToolsNotPrefixed(t *testing.T) {
	local := []einotool.BaseTool{namingStubTool{name: "read_file", desc: "Read a file"}}
	registry, err := buildCapabilityRegistryForTest(context.Background(), local, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildCapabilityRegistry: %v", err)
	}
	caps := registry.Specs()
	if len(caps) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(caps))
	}
	info, err := caps[0].Tool.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if strings.HasPrefix(info.Name, "mcp__") {
		t.Errorf("local tool name %q should NOT have mcp__ prefix", info.Name)
	}
}

func TestBuildCapabilityRegistryCrossProviderDuplicateDisambiguated(t *testing.T) {
	// Two MCP providers expose the same tool name "search".
	// After namespacing, they become mcp__github__search and mcp__notion__search,
	// which are distinct — the registry should build successfully.
	registrations := []mcpprovider.ToolRegistration{
		{ProviderName: "github", Tool: namingStubTool{name: "search", desc: "Search GitHub"}},
		{ProviderName: "notion", Tool: namingStubTool{name: "search", desc: "Search Notion"}},
	}
	registry, err := buildCapabilityRegistryForTest(context.Background(), nil, registrations, nil, nil)
	if err != nil {
		t.Fatalf("expected cross-provider duplicate to be auto-disambiguated, got error: %v", err)
	}
	caps := registry.Specs()
	if len(caps) != 2 {
		t.Fatalf("expected 2 capabilities, got %d", len(caps))
	}
	names := map[string]bool{}
	for _, cap := range caps {
		info, err := cap.Tool.Info(context.Background())
		if err != nil {
			t.Fatalf("Info: %v", err)
		}
		names[info.Name] = true
	}
	if !names["mcp__github__search"] {
		t.Error("expected mcp__github__search in registry")
	}
	if !names["mcp__notion__search"] {
		t.Error("expected mcp__notion__search in registry")
	}
}

func TestBuildCapabilityRegistrySameProviderDuplicateRejected(t *testing.T) {
	// Two tools with the same namespaced name from the same provider.
	// This would only happen if a single MCP server exposed duplicate tool names,
	// which violates the MCP spec. The registry should reject it.
	localA := namingStubTool{name: "dup_tool", desc: "First"}
	localB := namingStubTool{name: "dup_tool", desc: "Second"}
	_, err := buildCapabilityRegistryForTest(context.Background(),
		[]einotool.BaseTool{localA, localB}, nil, nil, nil)
	if err == nil {
		t.Fatal("expected duplicate capability error for same-named local tools")
	}
	if !strings.Contains(err.Error(), "duplicate capability name") {
		t.Errorf("error = %q, want duplicate capability name", err.Error())
	}
}

type failingInfoTool struct {
	message string
}

func (t failingInfoTool) Info(context.Context) (*schema.ToolInfo, error) {
	return nil, errors.New(t.message)
}

func TestBuildCapabilityRegistryMCPInfoError(t *testing.T) {
	registrations := []mcpprovider.ToolRegistration{
		{ProviderName: "broken", Tool: failingInfoTool{message: "info unavailable"}},
	}
	_, err := buildCapabilityRegistryForTest(context.Background(), nil, registrations, nil, nil)
	if err == nil {
		t.Fatal("expected error for MCP tool info failure, got nil")
	}
	if !strings.Contains(err.Error(), "read MCP tool info") {
		t.Errorf("error = %q, want MCP tool info context", err.Error())
	}
}

func buildCapabilityRegistryForTest(
	ctx context.Context,
	localTools []einotool.BaseTool,
	registrations []mcpprovider.ToolRegistration,
	resourceTools []einotool.BaseTool,
	promptTools []einotool.BaseTool,
) (*tools.Catalog, error) {
	specs := make([]port.ToolSpec, 0, len(localTools)+len(registrations)+len(resourceTools)+len(promptTools))
	for _, tool := range localTools {
		specs = append(specs, port.ToolSpec{
			ToolContract: toolNamingContract("", "local", port.ToolKindNative, port.ToolCategoryRead, port.EagerLoadingPolicy()),
			Tool:         tool,
		})
	}
	for _, registration := range registrations {
		info, err := registration.Tool.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("read MCP tool info for provider %q: %w", registration.ProviderName, err)
		}
		namespaced, err := NewMCPNamespacedTool(ctx, registration.Tool, registration.ProviderName, info.Name)
		if err != nil {
			return nil, fmt.Errorf("namespace MCP tool %q for provider %q: %w", info.Name, registration.ProviderName, err)
		}
		specs = append(specs, port.ToolSpec{
			ToolContract: toolNamingContract("", registration.ProviderName, port.ToolKindMCP, port.ToolCategoryIntegration, port.EagerLoadingPolicy()),
			Tool:         namespaced,
		})
	}
	for _, tool := range resourceTools {
		info, err := tool.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("read resource tool info: %w", err)
		}
		if info == nil {
			return nil, fmt.Errorf("read resource tool info: nil ToolInfo")
		}
		specs = append(specs, port.ToolSpec{
			ToolContract: toolNamingContract("", info.Name, port.ToolKindMCP, port.ToolCategoryIntegration, port.DeferredLoadingPolicy("deferred_mcp_catalog")),
			Tool:         tool,
		})
	}
	for _, tool := range promptTools {
		info, err := tool.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("read prompt tool info: %w", err)
		}
		if info == nil {
			return nil, fmt.Errorf("read prompt tool info: nil ToolInfo")
		}
		specs = append(specs, port.ToolSpec{
			ToolContract: toolNamingContract("", info.Name, port.ToolKindMCP, port.ToolCategoryIntegration, port.DeferredLoadingPolicy("deferred_mcp_catalog")),
			Tool:         tool,
		})
	}
	return tools.NewCatalog(ctx, specs)
}

func toolNamingContract(
	name string,
	source string,
	kind port.ToolKind,
	category port.ToolCategory,
	loading port.ToolLoadingPolicy,
) port.ToolContract {
	return port.ToolContract{
		Name:      name,
		Source:    source,
		Kind:      kind,
		Category:  category,
		Loading:   loading,
		Execution: port.ToolExecutionPolicy{ParallelPolicy: port.ParallelPolicyReadOnly},
	}
}
