package mcpprovider

import (
	"context"
	"errors"
	"fmt"
	"sync"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AuthConfig struct {
	Type       string // "none" | "oauth" | "api_key"
	ClientID   string
	Scopes     []string
	TokenStore string // reserved for future use
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
	onEvent       ProviderEventCallback
	tokenStore    TokenStore
	store         PendingActionStore
	elicitation   *ElicitationHandler
	sampling      *SamplingHandler
	samplingDepth int32
}

// ProviderEventCallback is called when a provider lifecycle event occurs. It
// must not block.
type ProviderEventCallback func(event ProviderEvent)

// ProviderEvent describes a provider lifecycle event emitted by the manager.
type ProviderEvent struct {
	Kind       string // "provider_added", "provider_removed", "provider_restarted", "auth_status_changed", catalog events
	Provider   string
	Transport  string
	Error      string // non-empty on failure
	AuthStatus string // new auth status for auth_status_changed events
}

type providerSlot struct {
	cfg           ProviderConfig
	p             *provider // nil when provider has failed
	lastErr       error
	startupStatus string
	authStatus    string // live auth status: "authenticated", "expired", "none", "env"
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
var connectProviderFunc = func(ctx context.Context, cfg ProviderConfig, opts *mcp.ClientOptions, store TokenStore, onAuthStatusChanged func(status string)) (*provider, error) {
	return connectProvider(ctx, cfg, opts, store, onAuthStatusChanged)
}

// ManagerOption configures Manager construction.
type ManagerOption func(*managerOptions)

type managerOptions struct {
	onEvent    ProviderEventCallback
	tokenStore TokenStore
	store      PendingActionStore
}

// WithEventCallback sets the provider lifecycle event callback on the manager.
func WithEventCallback(cb ProviderEventCallback) ManagerOption {
	return func(o *managerOptions) { o.onEvent = cb }
}

// WithTokenStore sets the OAuth token store on the manager for HTTP providers
// that require OAuth authentication. The store is passed to NewTransportWithStore
// when connecting OAuth-configured providers.
func WithTokenStore(store TokenStore) ManagerOption {
	return func(o *managerOptions) { o.tokenStore = store }
}

// WithStore sets the pending action store on the manager for elicitation handler
// support. The store is used to create, load, and decide PendingActions when
// MCP servers send elicitation/create requests.
func WithStore(store PendingActionStore) ManagerOption {
	return func(o *managerOptions) { o.store = store }
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
		slots:      slots,
		onEvent:    o.onEvent,
		tokenStore: o.tokenStore,
		store:      o.store,
	}

	// Initialize elicitation handler if store is available
	if o.store != nil {
		mgr.elicitation = newElicitationHandler(o.store, o.onEvent)
	}

	// Initialize sampling handler if store is available
	if o.store != nil {
		mgr.sampling = newSamplingHandler(mgr)
		mgr.sampling.store = o.store
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
