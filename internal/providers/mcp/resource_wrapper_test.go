package mcpprovider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestListResourcesToolInfo(t *testing.T) {
	tool := &listResourcesTool{
		session:      nil, // not needed for Info()
		providerName: "my-provider",
	}
	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	wantName := "mcp__my-provider__list_resources"
	if info.Name != wantName {
		t.Fatalf("tool name = %q, want %q", info.Name, wantName)
	}
	if !strings.Contains(info.Desc, "my-provider") {
		t.Fatalf("tool description should mention provider, got %q", info.Desc)
	}
}

func TestListResourcesToolInvokableRun(t *testing.T) {
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

	regs := mgr.ResourceRegistrations()
	if len(regs) == 0 {
		t.Fatal("expected at least one resource registration")
	}

	tool := &listResourcesTool{
		session:      regs[0].Session,
		providerName: "fixture",
	}
	result, err := tool.InvokableRun(context.Background(), "")
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v\nresult: %s", err, result)
	}
	resources, ok := parsed["resources"]
	if !ok {
		t.Fatal("result JSON missing 'resources' key")
	}
	resArr, ok := resources.([]interface{})
	if !ok {
		t.Fatalf("resources is not an array, got %T", resources)
	}
	if len(resArr) == 0 {
		t.Fatal("expected at least one resource in list result")
	}
}

func TestReadResourceToolInfo(t *testing.T) {
	tool := &readResourceTool{
		session:      nil,
		providerName: "test-prov",
	}
	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	wantName := "mcp__test-prov__read_resource"
	if info.Name != wantName {
		t.Fatalf("tool name = %q, want %q", info.Name, wantName)
	}
	if !strings.Contains(info.Desc, "test-prov") {
		t.Fatalf("tool description should mention provider, got %q", info.Desc)
	}
}

func TestReadResourceToolInvokableRun(t *testing.T) {
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

	regs := mgr.ResourceRegistrations()
	if len(regs) == 0 {
		t.Fatal("expected at least one resource registration")
	}

	tool := &readResourceTool{
		session:      regs[0].Session,
		providerName: "fixture",
	}
	result, err := tool.InvokableRun(context.Background(), `{"uri":"test://resource"}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v\nresult: %s", err, result)
	}
	contents, ok := parsed["contents"]
	if !ok {
		t.Fatal("result JSON missing 'contents' key")
	}
	contentArr, ok := contents.([]interface{})
	if !ok {
		t.Fatalf("contents is not an array, got %T", contents)
	}
	if len(contentArr) == 0 {
		t.Fatal("expected at least one content item in read result")
	}
}

func TestReadResourceToolMissingURI(t *testing.T) {
	tool := &readResourceTool{
		session:      nil, // not needed for this error path
		providerName: "prov",
	}
	_, err := tool.InvokableRun(context.Background(), `{}`)
	if err == nil {
		t.Fatal("expected error when uri is missing from arguments")
	}
}

func TestBuildResourceTools(t *testing.T) {
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

	regs := mgr.ResourceRegistrations()
	if len(regs) == 0 {
		t.Fatal("expected at least one resource registration")
	}

	tools := buildResourceTools(regs[0].Session, "fixture")
	if got, want := len(tools), 2; got != want {
		t.Fatalf("expected %d resource tools, got %d", want, got)
	}

	// Verify tool names
	names := make(map[string]bool)
	for _, tool := range tools {
		info, err := tool.Info(context.Background())
		if err != nil {
			t.Fatalf("tool Info: %v", err)
		}
		names[info.Name] = true
	}
	if !names["mcp__fixture__list_resources"] {
		t.Fatal("expected mcp__fixture__list_resources tool")
	}
	if !names["mcp__fixture__read_resource"] {
		t.Fatal("expected mcp__fixture__read_resource tool")
	}
}

func TestManagerResourceToolsExposeProviderTools(t *testing.T) {
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

	resourceTools := mgr.ResourceTools()
	if got, want := len(resourceTools), 2; got != want {
		t.Fatalf("expected %d resource tools from manager, got %d", want, got)
	}

	for _, tool := range resourceTools {
		if _, err := tool.Info(context.Background()); err != nil {
			t.Fatalf("resource tool info: %v", err)
		}
	}
}

func TestManagerResourceToolsNil(t *testing.T) {
	var mgr *Manager
	if got := mgr.ResourceTools(); got != nil {
		t.Fatalf("nil Manager.ResourceTools() = %v, want nil", got)
	}
}

func TestReadResourceToolWithSessionError(t *testing.T) {
	tool := &readResourceTool{
		session:      nil, // nil session should cause error
		providerName: "broken",
	}
	_, err := tool.InvokableRun(context.Background(), `{"uri":"test://resource"}`)
	if err == nil {
		t.Fatal("expected error when session is nil")
	}
}

// Ensure listResourcesTool and readResourceTool implement mcp session calls
// correctly by using a real session from the fixture server.
func TestListResourcesToolCallsSessionListResources(t *testing.T) {
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

	regs := mgr.ResourceRegistrations()
	tool := &listResourcesTool{
		session:      regs[0].Session,
		providerName: "fixture",
	}
	result, err := tool.InvokableRun(context.Background(), "")
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}

	// Verify the result contains the fixture server's test-resource
	if !strings.Contains(result, "test-resource") && !strings.Contains(result, "test://resource") {
		t.Fatalf("result should contain test-resource data, got: %s", result)
	}
}

func TestReadResourceToolCallsSessionReadResource(t *testing.T) {
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

	regs := mgr.ResourceRegistrations()
	tool := &readResourceTool{
		session:      regs[0].Session,
		providerName: "fixture",
	}
	result, err := tool.InvokableRun(context.Background(), `{"uri":"test://resource"}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}

	// Verify the result contains the resource content
	if !strings.Contains(result, "hello from resource") {
		t.Fatalf("result should contain resource content, got: %s", result)
	}
}

// Verify that ResourceRegistrations has the expected Session field type
func TestResourceRegistrationSessionType(t *testing.T) {
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

	regs := mgr.ResourceRegistrations()
	if len(regs) == 0 {
		t.Fatal("expected resource registrations")
	}
	var _ *mcp.ClientSession = regs[0].Session
}
