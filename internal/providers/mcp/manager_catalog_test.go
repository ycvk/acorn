package mcpprovider

import (
	"context"
	"errors"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRefreshProviderCatalogRefreshesOnlyAffectedProvider(t *testing.T) {
	binary := buildFixtureServer(t)

	var events []ProviderEvent
	mgr, err := NewManager(context.Background(), []ProviderConfig{
		{
			Name:                  "alpha",
			Enabled:               true,
			Transport:             "stdio",
			Command:               binary,
			StartupTimeoutSeconds: 10,
		},
		{
			Name:                  "beta",
			Enabled:               true,
			Transport:             "stdio",
			Command:               binary,
			StartupTimeoutSeconds: 10,
		},
	}, WithEventCallback(func(ev ProviderEvent) {
		events = append(events, ev)
	}))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	events = events[:0]

	if err := mgr.RefreshProviderCatalog(context.Background(), "alpha"); err != nil {
		t.Fatalf("RefreshProviderCatalog: %v", err)
	}

	found := false
	for _, ev := range events {
		if ev.Kind == "tool_catalog_refreshed" {
			if ev.Provider == "alpha" {
				found = true
			}
			if ev.Provider == "beta" {
				t.Fatal("beta should not have emitted a catalog refresh event")
			}
		}
	}
	if !found {
		t.Fatalf("expected tool_catalog_refreshed event for alpha, got events: %v", events)
	}

	statuses := mgr.Statuses()
	for _, s := range statuses {
		if s.Name == "beta" && s.ToolCount == 0 {
			t.Fatal("beta should still have tools after refreshing only alpha")
		}
	}
}

func TestRefreshProviderCatalogCopyOnWriteSafety(t *testing.T) {
	binary := buildFixtureServer(t)

	mgr, err := NewManager(context.Background(), []ProviderConfig{{
		Name:                  "alpha",
		Enabled:               true,
		Transport:             "stdio",
		Command:               binary,
		StartupTimeoutSeconds: 10,
	}})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	oldTools := mgr.Tools()
	if len(oldTools) == 0 {
		t.Fatal("expected initial tools")
	}

	if err := mgr.RefreshProviderCatalog(context.Background(), "alpha"); err != nil {
		t.Fatalf("RefreshProviderCatalog: %v", err)
	}

	oldEcho := findToolByName(t, oldTools, "echo")
	invokable, ok := oldEcho.(einotool.InvokableTool)
	if !ok {
		t.Fatal("old echo tool is not invokable after catalog refresh")
	}
	result, err := invokable.InvokableRun(context.Background(), `{"text":"safety"}`)
	if err != nil {
		t.Fatalf("invoke old echo tool after refresh: %v", err)
	}
	if !strings.Contains(result, "echo: safety") {
		t.Fatalf("unexpected result from old tool reference: %s", result)
	}

	newTools := mgr.Tools()
	newEcho := findToolByName(t, newTools, "echo")
	invokable2, ok := newEcho.(einotool.InvokableTool)
	if !ok {
		t.Fatal("new echo tool is not invokable")
	}
	result2, err := invokable2.InvokableRun(context.Background(), `{"text":"fresh"}`)
	if err != nil {
		t.Fatalf("invoke new echo tool: %v", err)
	}
	if !strings.Contains(result2, "echo: fresh") {
		t.Fatalf("unexpected result from new tool reference: %s", result2)
	}
}

func TestRefreshProviderCatalogFailurePreservesOldTools(t *testing.T) {
	binary := buildFixtureServer(t)

	var events []ProviderEvent
	mgr, err := NewManager(context.Background(), []ProviderConfig{{
		Name:                  "alpha",
		Enabled:               true,
		Transport:             "stdio",
		Command:               binary,
		StartupTimeoutSeconds: 10,
	}}, WithEventCallback(func(ev ProviderEvent) {
		events = append(events, ev)
	}))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	initialTools := mgr.Tools()
	if got := len(initialTools); got == 0 {
		t.Fatal("expected initial tools before refresh failure")
	}

	mgr.mu.Lock()
	slot := &mgr.slots[0]
	session := slot.p.session
	slot.p.session = nil
	mgr.mu.Unlock()

	events = events[:0]

	err = mgr.RefreshProviderCatalog(context.Background(), "alpha")
	if err == nil {
		t.Fatal("expected error when refreshing with nil session")
	}

	var foundFailed bool
	for _, ev := range events {
		if ev.Kind == "tool_catalog_refresh_failed" && ev.Provider == "alpha" {
			foundFailed = true
			if ev.Error == "" {
				t.Fatal("tool_catalog_refresh_failed event should have Error set")
			}
		}
	}
	if !foundFailed {
		t.Fatalf("expected tool_catalog_refresh_failed event, got events: %v", events)
	}

	mgr.mu.Lock()
	mgr.slots[0].p.session = session
	mgr.mu.Unlock()

	toolsAfter := mgr.Tools()
	if got := len(toolsAfter); got != len(initialTools) {
		t.Fatalf("tool count after failed refresh = %d, want %d (unchanged)", got, len(initialTools))
	}

	echoTool := findToolByName(t, toolsAfter, "echo")
	invokable, ok := echoTool.(einotool.InvokableTool)
	if !ok {
		t.Fatal("echo tool is not invokable after failed refresh")
	}
	result, err := invokable.InvokableRun(context.Background(), `{"text":"survives"}`)
	if err != nil {
		t.Fatalf("invoke echo tool after failed refresh: %v", err)
	}
	if !strings.Contains(result, "echo: survives") {
		t.Fatalf("unexpected result after failed refresh: %s", result)
	}
}

func TestManagerResourcesAndPrompts(t *testing.T) {
	binary := buildFixtureServer(t)
	mgr, err := NewManager(context.Background(), []ProviderConfig{{
		Name:                  "fixture",
		Enabled:               true,
		Transport:             "stdio",
		Command:               binary,
		StartupTimeoutSeconds: 10,
	}}, WithEventCallback(func(ev ProviderEvent) {
	}))
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	resources := mgr.Resources()
	if got, want := len(resources), 1; got != want {
		t.Fatalf("expected %d resources, got %d", want, got)
	}
	if got, want := resources[0].Name, "test-resource"; got != want {
		t.Fatalf("resource name = %q, want %q", got, want)
	}

	prompts := mgr.Prompts()
	if got, want := len(prompts), 1; got != want {
		t.Fatalf("expected %d prompts, got %d", want, got)
	}
	if got, want := prompts[0].Name, "test-prompt"; got != want {
		t.Fatalf("prompt name = %q, want %q", got, want)
	}
}

func TestManagerResourceRegistrations(t *testing.T) {
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
	if got, want := len(regs), 1; got != want {
		t.Fatalf("expected %d resource registrations, got %d", want, got)
	}
	if got, want := regs[0].ProviderName, "fixture"; got != want {
		t.Fatalf("registration provider = %q, want %q", got, want)
	}
	if got, want := len(regs[0].Resources), 1; got != want {
		t.Fatalf("registration resources count = %d, want %d", got, want)
	}
	if regs[0].Session == nil {
		t.Fatal("registration session must not be nil")
	}
}

func TestManagerPromptRegistrations(t *testing.T) {
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
	if got, want := len(regs), 1; got != want {
		t.Fatalf("expected %d prompt registrations, got %d", want, got)
	}
	if got, want := regs[0].ProviderName, "fixture"; got != want {
		t.Fatalf("registration provider = %q, want %q", got, want)
	}
	if got, want := len(regs[0].Prompts), 1; got != want {
		t.Fatalf("registration prompts count = %d, want %d", got, want)
	}
	if regs[0].Session == nil {
		t.Fatal("registration session must not be nil")
	}
}

func TestProviderStatusAuthStatus(t *testing.T) {
	tests := []struct {
		name       string
		cfg        ProviderConfig
		wantStatus string
	}{
		{
			name: "stdio_transport_gets_env_auth_status",
			cfg: ProviderConfig{
				Name:                  "stdio_prov",
				Enabled:               true,
				Transport:             "stdio",
				Command:               "some-cmd",
				StartupTimeoutSeconds: 10,
			},
			wantStatus: "env",
		},
		{
			name: "oauth_on_sse_gets_none_initially",
			cfg: ProviderConfig{
				Name:                  "oauth_prov",
				Enabled:               true,
				Transport:             "sse",
				URL:                   "http://localhost/sse",
				StartupTimeoutSeconds: 10,
				Auth: AuthConfig{
					Type:     "oauth",
					ClientID: "my-client",
				},
			},
			wantStatus: "none",
		},
		{
			name: "sse_without_auth_gets_none",
			cfg: ProviderConfig{
				Name:                  "sse_noauth",
				Enabled:               true,
				Transport:             "sse",
				URL:                   "http://localhost/sse",
				StartupTimeoutSeconds: 10,
			},
			wantStatus: "none",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := newProviderStatus(tt.cfg)
			if got, want := status.AuthStatus, tt.wantStatus; got != want {
				t.Fatalf("AuthStatus = %q, want %q", got, want)
			}
		})
	}
}

func TestManagerResourcesFromHealthyProvidersOnly(t *testing.T) {
	binary := buildFixtureServer(t)

	origFunc := connectProviderFunc
	t.Cleanup(func() { connectProviderFunc = origFunc })

	connectProviderFunc = func(ctx context.Context, cfg ProviderConfig, opts *mcp.ClientOptions, store TokenStore, onAuthStatusChanged func(status string)) (*provider, error) {
		if cfg.Name == "broken" {
			return nil, errors.New("connection failed")
		}
		return connectProvider(ctx, cfg, opts, store, onAuthStatusChanged)
	}

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
			Command:               "./does-not-exist",
			StartupTimeoutSeconds: 2,
		},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	resources := mgr.Resources()
	if got, want := len(resources), 1; got != want {
		t.Fatalf("expected %d resources from healthy provider only, got %d", want, got)
	}

	prompts := mgr.Prompts()
	if got, want := len(prompts), 1; got != want {
		t.Fatalf("expected %d prompts from healthy provider only, got %d", want, got)
	}
}

func TestManagerNilReturnsEmpty(t *testing.T) {
	var mgr *Manager
	if got := mgr.Resources(); got != nil {
		t.Fatalf("nil Manager.Resources() = %v, want nil", got)
	}
	if got := mgr.Prompts(); got != nil {
		t.Fatalf("nil Manager.Prompts() = %v, want nil", got)
	}
	if got := mgr.ResourceRegistrations(); got != nil {
		t.Fatalf("nil Manager.ResourceRegistrations() = %v, want nil", got)
	}
	if got := mgr.PromptRegistrations(); got != nil {
		t.Fatalf("nil Manager.PromptRegistrations() = %v, want nil", got)
	}
}

func TestNotificationHandlersForResourceAndPrompt(t *testing.T) {
	binary := buildFixtureServer(t)

	var events []ProviderEvent
	mgr, err := NewManager(context.Background(), []ProviderConfig{{
		Name:                  "fixture",
		Enabled:               true,
		Transport:             "stdio",
		Command:               binary,
		StartupTimeoutSeconds: 10,
	}}, WithEventCallback(func(ev ProviderEvent) {
		events = append(events, ev)
	}))
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	events = events[:0]
	if err := mgr.refreshProviderCatalogByType(context.Background(), "fixture", "resources"); err != nil {
		t.Fatalf("refreshProviderCatalogByType resources: %v", err)
	}
	found := false
	for _, ev := range events {
		if ev.Kind == "resource_catalog_refreshed" && ev.Provider == "fixture" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected resource_catalog_refreshed event, got events: %v", events)
	}

	events = events[:0]
	if err := mgr.refreshProviderCatalogByType(context.Background(), "fixture", "prompts"); err != nil {
		t.Fatalf("refreshProviderCatalogByType prompts: %v", err)
	}
	found = false
	for _, ev := range events {
		if ev.Kind == "prompt_catalog_refreshed" && ev.Provider == "fixture" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected prompt_catalog_refreshed event, got events: %v", events)
	}
}

func TestRefreshProviderCatalogResourcesCopyOnWrite(t *testing.T) {
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

	oldResources := mgr.Resources()
	if got, want := len(oldResources), 1; got != want {
		t.Fatalf("expected %d initial resources, got %d", want, got)
	}

	if err := mgr.refreshProviderCatalogByType(context.Background(), "fixture", "resources"); err != nil {
		t.Fatalf("refreshProviderCatalogByType: %v", err)
	}

	newResources := mgr.Resources()
	if got, want := len(newResources), 1; got != want {
		t.Fatalf("expected %d resources after refresh, got %d", want, got)
	}
}

func TestRefreshProviderCatalogByTypeUnknownProvider(t *testing.T) {
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

	err = mgr.refreshProviderCatalogByType(context.Background(), "nonexistent", "resources")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("expected provider name in error, got %v", err)
	}
}

func TestRefreshProviderCatalogByTypeInvalidCatalogType(t *testing.T) {
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

	err = mgr.refreshProviderCatalogByType(context.Background(), "fixture", "widgets")
	if err == nil {
		t.Fatal("expected error for unknown catalog type")
	}
	if !strings.Contains(err.Error(), "unknown catalog type") {
		t.Fatalf("expected catalog type error, got %v", err)
	}
}

func TestManagerToolsDoesNotIncludeResourcePromptTools(t *testing.T) {
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
	for _, tool := range tools {
		info, err := tool.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info: %v", err)
		}
		if strings.Contains(info.Name, "list_resources") ||
			strings.Contains(info.Name, "read_resource") ||
			strings.Contains(info.Name, "list_prompts") ||
			strings.Contains(info.Name, "get_prompt") {
			t.Fatalf("Manager.Tools() should NOT include resource/prompt tools, found %q", info.Name)
		}
	}

	resourceTools := mgr.ResourceTools()
	if len(resourceTools) == 0 {
		t.Fatal("expected resource tools from ResourceTools()")
	}
	promptTools := mgr.PromptTools()
	if len(promptTools) == 0 {
		t.Fatal("expected prompt tools from PromptTools()")
	}
}

func TestManagerToolCountReflectsOnlyRegularTools(t *testing.T) {
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

	statuses := mgr.Statuses()
	if got, want := len(statuses), 1; got != want {
		t.Fatalf("expected %d statuses, got %d", want, got)
	}
	if got, want := statuses[0].ToolCount, 2; got != want {
		t.Fatalf("ToolCount = %d, want %d (only regular tools, not resource/prompt)", got, want)
	}
}
