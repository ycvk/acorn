package mcpprovider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestListPromptsToolInfo(t *testing.T) {
	tool := &listPromptsTool{
		session:      nil, // not needed for Info()
		providerName: "my-provider",
	}
	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	wantName := "mcp__my-provider__list_prompts"
	if info.Name != wantName {
		t.Fatalf("tool name = %q, want %q", info.Name, wantName)
	}
	if !strings.Contains(info.Desc, "my-provider") {
		t.Fatalf("tool description should mention provider, got %q", info.Desc)
	}
}

func TestListPromptsToolInvokableRun(t *testing.T) {
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

	regs := mgr.PromptRegistrations()
	if len(regs) == 0 {
		t.Fatal("expected at least one prompt registration")
	}

	tool := &listPromptsTool{
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
	prompts, ok := parsed["prompts"]
	if !ok {
		t.Fatal("result JSON missing 'prompts' key")
	}
	promptArr, ok := prompts.([]interface{})
	if !ok {
		t.Fatalf("prompts is not an array, got %T", prompts)
	}
	if len(promptArr) == 0 {
		t.Fatal("expected at least one prompt in list result")
	}
}

func TestGetPromptToolInfo(t *testing.T) {
	tool := &getPromptTool{
		session:      nil,
		providerName: "test-prov",
	}
	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	wantName := "mcp__test-prov__get_prompt"
	if info.Name != wantName {
		t.Fatalf("tool name = %q, want %q", info.Name, wantName)
	}
	if !strings.Contains(info.Desc, "test-prov") {
		t.Fatalf("tool description should mention provider, got %q", info.Desc)
	}
}

func TestGetPromptToolInvokableRun(t *testing.T) {
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

	regs := mgr.PromptRegistrations()
	if len(regs) == 0 {
		t.Fatal("expected at least one prompt registration")
	}

	tool := &getPromptTool{
		session:      regs[0].Session,
		providerName: "fixture",
	}
	result, err := tool.InvokableRun(context.Background(), `{"name":"test-prompt"}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v\nresult: %s", err, result)
	}
	messages, ok := parsed["messages"]
	if !ok {
		t.Fatal("result JSON missing 'messages' key")
	}
	msgArr, ok := messages.([]interface{})
	if !ok {
		t.Fatalf("messages is not an array, got %T", messages)
	}
	if len(msgArr) == 0 {
		t.Fatal("expected at least one message in get_prompt result")
	}
}

func TestGetPromptToolWithArguments(t *testing.T) {
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

	regs := mgr.PromptRegistrations()
	if len(regs) == 0 {
		t.Fatal("expected at least one prompt registration")
	}

	tool := &getPromptTool{
		session:      regs[0].Session,
		providerName: "fixture",
	}
	// Test with both name and arguments
	result, err := tool.InvokableRun(context.Background(), `{"name":"test-prompt","arguments":{"key":"value"}}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v\nresult: %s", err, result)
	}
}

func TestGetPromptToolMissingName(t *testing.T) {
	tool := &getPromptTool{
		session:      nil,
		providerName: "prov",
	}
	_, err := tool.InvokableRun(context.Background(), `{}`)
	if err == nil {
		t.Fatal("expected error when name is missing from arguments")
	}
}

func TestBuildPromptTools(t *testing.T) {
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

	regs := mgr.PromptRegistrations()
	if len(regs) == 0 {
		t.Fatal("expected at least one prompt registration")
	}

	tools := buildPromptTools(regs[0].Session, "fixture")
	if got, want := len(tools), 2; got != want {
		t.Fatalf("expected %d prompt tools, got %d", want, got)
	}

	names := make(map[string]bool)
	for _, tool := range tools {
		info, err := tool.Info(context.Background())
		if err != nil {
			t.Fatalf("tool Info: %v", err)
		}
		names[info.Name] = true
	}
	if !names["mcp__fixture__list_prompts"] {
		t.Fatal("expected mcp__fixture__list_prompts tool")
	}
	if !names["mcp__fixture__get_prompt"] {
		t.Fatal("expected mcp__fixture__get_prompt tool")
	}
}

func TestManagerPromptToolsExposeProviderTools(t *testing.T) {
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

	promptTools := mgr.PromptTools()
	if got, want := len(promptTools), 2; got != want {
		t.Fatalf("expected %d prompt tools from manager, got %d", want, got)
	}

	for _, tool := range promptTools {
		if _, err := tool.Info(context.Background()); err != nil {
			t.Fatalf("prompt tool info: %v", err)
		}
	}
}

func TestManagerPromptToolsNil(t *testing.T) {
	var mgr *Manager
	if got := mgr.PromptTools(); got != nil {
		t.Fatalf("nil Manager.PromptTools() = %v, want nil", got)
	}
}

func TestGetPromptToolWithSessionError(t *testing.T) {
	tool := &getPromptTool{
		session:      nil, // nil session should cause error
		providerName: "broken",
	}
	_, err := tool.InvokableRun(context.Background(), `{"name":"test"}`)
	if err == nil {
		t.Fatal("expected error when session is nil")
	}
}

func TestListPromptsToolCallsSessionListPrompts(t *testing.T) {
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

	regs := mgr.PromptRegistrations()
	tool := &listPromptsTool{
		session:      regs[0].Session,
		providerName: "fixture",
	}
	result, err := tool.InvokableRun(context.Background(), "")
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}

	if !strings.Contains(result, "test-prompt") {
		t.Fatalf("result should contain test-prompt, got: %s", result)
	}
}

func TestGetPromptToolCallsSessionGetPrompt(t *testing.T) {
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

	regs := mgr.PromptRegistrations()
	tool := &getPromptTool{
		session:      regs[0].Session,
		providerName: "fixture",
	}
	result, err := tool.InvokableRun(context.Background(), `{"name":"test-prompt"}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}

	// The fixture server returns "You are a test prompt." in the prompt message
	if !strings.Contains(result, "test prompt") {
		t.Fatalf("result should contain prompt content, got: %s", result)
	}
}

// Verify that PromptRegistrations has the expected Session field type
func TestPromptRegistrationSessionType(t *testing.T) {
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

	regs := mgr.PromptRegistrations()
	if len(regs) == 0 {
		t.Fatal("expected prompt registrations")
	}
	var _ *mcp.ClientSession = regs[0].Session
}
