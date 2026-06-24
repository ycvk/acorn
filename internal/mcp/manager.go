package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ycvk/acorn/internal/core"
)

type AuthConfig struct {
	Type     string // "none" | "oauth" | "api_key"
	ClientID string
	Scopes   []string
}

type ProviderConfig struct {
	Name                  string
	Enabled               bool
	Transport             string
	URL                   string
	TimeoutSeconds        int
	Command               string
	Args                  []string
	WorkDir               string
	Env                   map[string]string
	ToolNames             []string
	StartupTimeoutSeconds int
	Auth                  AuthConfig
}

type ProviderStatus struct {
	Name                string   `json:"name"`
	Configured          bool     `json:"configured"`
	Enabled             bool     `json:"enabled"`
	Transport           string   `json:"transport,omitempty"`
	StartupStatus       string   `json:"startup_status,omitempty"`
	Command             string   `json:"command"`
	Args                []string `json:"args,omitempty"`
	WorkDir             string   `json:"work_dir,omitempty"`
	CommandPath         string   `json:"command_path,omitempty"`
	ConfiguredToolNames []string `json:"configured_tool_names,omitempty"`
	DiscoveredToolNames []string `json:"discovered_tool_names,omitempty"`
	ToolCount           int      `json:"tool_count"`
	Error               string   `json:"error,omitempty"`
	AuthStatus          string   `json:"auth_status,omitempty"` // "authenticated", "expired", "none", "env"
}

type Manager struct {
	slots         []providerSlot
	mu            sync.RWMutex
	stopped       bool
	stoppedMu     sync.Mutex
	tokenStore    core.ArtifactStore
	store         core.SessionStore
	toolRegistry  core.ToolRegistry
	specBuilder   core.MCPToolSpecBuilder
	elicitation   *ElicitationHandler
	sampling      *SamplingHandler
	samplingDepth int32
}

type providerSlot struct {
	cfg           ProviderConfig
	p             *provider // nil when provider has failed
	lastErr       error
	startupStatus string
	authStatus    string // live auth status: "authenticated", "expired", "none", "env"
	// registeredToolNames holds the namespaced names registered into the unified
	// ToolRegistry for this slot's provider. Populated by registerProviderTools
	// when a registry is wired; consumed by unregisterProviderTools on disconnect
	// so reconcile/close cleanly removes exactly the specs this provider added.
	registeredToolNames []string
}

type ToolRegistration struct {
	ProviderName string
	Tool         einotool.BaseTool
}

type ResourceRegistration struct {
	ProviderName string
	Resources    []*mcp.Resource
	Session      *mcp.ClientSession
}

type PromptRegistration struct {
	ProviderName string
	Prompts      []*mcp.Prompt
	Session      *mcp.ClientSession
}

type provider struct {
	cfg         ProviderConfig
	commandPath string
	session     *mcp.ClientSession
	cleanup     func()
	tools       []einotool.BaseTool
	toolNames   []string
	resources   []*mcp.Resource
	prompts     []*mcp.Prompt
}

// connectProviderFunc is the production default for connecting to a provider.
// Tests can override this to inject fake connections without requiring a
// real MCP server. The seam must not create a production fake-success path.
//
// The third parameter allows the caller to supply ClientOptions (e.g. for
// notification handler registration). When nil, the client is created with
// no options.
var connectProviderFunc = func(ctx context.Context, cfg ProviderConfig, opts *mcp.ClientOptions, store core.ArtifactStore, onAuthStatusChanged func(status string)) (*provider, error) {
	return connectProvider(ctx, cfg, opts, store, onAuthStatusChanged)
}

// ManagerOption configures Manager construction.
type ManagerOption func(*managerOptions)

type managerOptions struct {
	tokenStore   core.ArtifactStore
	store        core.SessionStore
	toolRegistry core.ToolRegistry
	specBuilder  core.MCPToolSpecBuilder
}

// WithTokenStore sets the OAuth token store on the manager for HTTP providers
// that require OAuth authentication. The store is passed to NewTransportWithStore
// when connecting OAuth-configured providers.
func WithTokenStore(store core.ArtifactStore) ManagerOption {
	return func(o *managerOptions) { o.tokenStore = store }
}

// WithStore sets the pending action store on the manager for elicitation handler
// support. The store is used to create, load, and decide PendingActions when
// MCP servers send elicitation/create requests.
func WithStore(store core.SessionStore) ManagerOption {
	return func(o *managerOptions) { o.store = store }
}

// WithToolRegistry wires a unified core.ToolRegistry so MCP tools discovered
// at provider-connect time are registered alongside native tools, and
// unregistered when their provider disconnects. specBuilder translates a
// discovered (provider, tool) pair into a core.ToolSpec; the manager applies it
// and tracks the namespaced names per provider for later removal. Either may be
// nil (e.g. tests): when the registry is nil the manager skips registration and
// the runtime falls back to building MCP specs at run time.
func WithToolRegistry(registry core.ToolRegistry, specBuilder core.MCPToolSpecBuilder) ManagerOption {
	return func(o *managerOptions) {
		o.toolRegistry = registry
		o.specBuilder = specBuilder
	}
}

func NewManager(ctx context.Context, cfgs []ProviderConfig, opts ...ManagerOption) (*Manager, error) {
	var o managerOptions
	for _, opt := range opts {
		opt(&o)
	}

	enabled := make([]ProviderConfig, 0, len(cfgs))
	for _, cfg := range cfgs {
		if cfg.Enabled {
			enabled = append(enabled, cfg)
		}
	}
	if len(enabled) == 0 {
		return nil, errors.New("no enabled MCP providers")
	}

	type slotResult struct {
		slot providerSlot
	}

	results := make([]slotResult, len(enabled))
	var wg sync.WaitGroup
	wg.Add(len(enabled))

	for i, cfg := range enabled {
		go func(idx int, c ProviderConfig) {
			defer wg.Done()
			p, err := connectProviderFunc(ctx, c, nil, o.tokenStore, nil)
			slot := providerSlot{
				cfg:        c,
				authStatus: newProviderStatus(c).AuthStatus,
			}
			if err != nil {
				slot.lastErr = err
				slot.startupStatus = "failed"
			} else {
				slot.p = p
				slot.startupStatus = "healthy"
			}
			results[idx] = slotResult{slot: slot}
		}(i, cfg)
	}
	wg.Wait()

	slots := make([]providerSlot, len(results))
	var healthyCount int
	for i, r := range results {
		slots[i] = r.slot
		if r.slot.startupStatus == "healthy" {
			healthyCount++
		}
	}

	if healthyCount == 0 {
		var errs []error
		for _, s := range slots {
			errs = append(errs, fmt.Errorf("connect MCP provider %s: %w", s.cfg.Name, s.lastErr))
		}
		return nil, fmt.Errorf("all MCP providers failed: %w", errors.Join(errs...))
	}

	mgr := &Manager{
		slots:        slots,
		tokenStore:   o.tokenStore,
		store:        o.store,
		toolRegistry: o.toolRegistry,
		specBuilder:  o.specBuilder,
	}

	// Initialize elicitation handler if store is available
	if o.store != nil {
		mgr.elicitation = newElicitationHandler(o.store)
	}

	// Initialize sampling handler if store is available
	if o.store != nil {
		mgr.sampling = newSamplingHandler(mgr)
	}
	// Register discovered MCP tools for every healthy provider into the unified
	// ToolRegistry. No-op when no registry was wired (legacy/test path).
	for _, slot := range slots {
		if slot.startupStatus == "healthy" {
			mgr.registerProviderTools(ctx, slot.cfg.Name)
		}
	}

	return mgr, nil
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	m.stoppedMu.Lock()
	if !m.stopped {
		m.stopped = true
	}
	m.stoppedMu.Unlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	var errs []error
	// Remove every provider's tools from the unified registry. We hold the
	// write lock already, so unregister inline rather than calling
	// unregisterProviderTools (which would re-lock and deadlock). No-op when
	// no registry was wired.
	if m.toolRegistry != nil {
		for i := range m.slots {
			for _, name := range m.slots[i].registeredToolNames {
				if err := m.toolRegistry.Unregister(name); err != nil {
					slog.Warn("MCP tool unregister on close skipped",
						"provider", m.slots[i].cfg.Name, "tool", name, "error", err)
				}
			}
			m.slots[i].registeredToolNames = nil
		}
	}
	for i := range m.slots {
		if m.slots[i].p != nil {
			if err := m.slots[i].p.close(); err != nil {
				errs = append(errs, fmt.Errorf("close MCP provider %s: %w", m.slots[i].cfg.Name, err))
			}
			m.slots[i].p = nil
		}
	}
	return errors.Join(errs...)
}

// ReconcileProviders applies a new provider config set to the live manager.
// It classifies providers as added, removed, changed, or unchanged by name,
// and only mutates the slots that need to change. Unchanged providers keep
// running without interruption.
//
// The slot order after reconciliation is deterministic: existing providers
// retain their original relative order, followed by newly added providers
// in the order they appear in the input config.
