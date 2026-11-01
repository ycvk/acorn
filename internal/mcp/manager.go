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

type Manager struct {
	slots        []providerSlot
	mu           sync.RWMutex
	tokenStore   core.ArtifactStore
	store        core.SessionStore
	toolRegistry core.ToolRegistry
	specBuilder  ToolSpecBuilder
	elicitation  *ElicitationHandler
}

type providerSlot struct {
	cfg           core.ProviderConfig
	p             *provider // nil when provider has failed
	lastErr       error
	startupStatus string
	// registeredToolNames holds the namespaced names registered into the unified
	// ToolRegistry for this slot's provider. Populated by registerProviderTools
	// when a registry is wired; consumed by unregisterProviderTools and Close so
	// they remove exactly the specs this provider added.
	registeredToolNames []string
}

type provider struct {
	cfg         core.ProviderConfig
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
var connectProviderFunc = connectProvider

// ManagerOption configures Manager construction.
type ManagerOption func(*managerOptions)

type managerOptions struct {
	tokenStore   core.ArtifactStore
	store        core.SessionStore
	toolRegistry core.ToolRegistry
	specBuilder  ToolSpecBuilder
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

// ToolSpecBuilder translates a discovered MCP tool (provider + tool) into a
// core.ToolSpec for registration in the unified ToolRegistry. It is supplied by
// the runtime — which owns MCP namespacing and description augmentation — so
// the manager can register tools without taking a runtime dependency. The namespaced tool name returned in the spec
// MUST match what the capability catalog expects (see runtime.mcpToolName).
type ToolSpecBuilder func(ctx context.Context, providerName string, tool einotool.BaseTool) (core.ToolSpec, error)

// WithToolRegistry wires a unified core.ToolRegistry so MCP tools discovered
// at provider-connect time are registered alongside native tools, and
// unregistered when their provider disconnects. specBuilder translates a
// discovered (provider, tool) pair into a core.ToolSpec; the manager applies it
// and tracks the namespaced names per provider for later removal. Either may
// be nil (tests only): when the registry is nil the manager skips registration
// and MCP main tools are unavailable to runs. Resource/prompt auxiliary specs
// are built from the manager regardless, because they are session-derived
// wrappers outside the registry lifecycle.
func WithToolRegistry(registry core.ToolRegistry, specBuilder ToolSpecBuilder) ManagerOption {
	return func(o *managerOptions) {
		o.toolRegistry = registry
		o.specBuilder = specBuilder
	}
}

func NewManager(ctx context.Context, cfgs []core.ProviderConfig, opts ...ManagerOption) (*Manager, error) {
	var o managerOptions
	for _, opt := range opts {
		opt(&o)
	}

	enabled := make([]core.ProviderConfig, 0, len(cfgs))
	for _, cfg := range cfgs {
		if cfg.Enabled {
			enabled = append(enabled, cfg)
		}
	}
	if len(enabled) == 0 {
		return nil, errors.New("no enabled MCP providers")
	}

	// Construct the manager first so the elicitation handler is initialized
	// before providers connect: buildClientOptions takes the elicitation and
	// list-changed handlers from the manager, and they must exist at connect
	// time.
	mgr := &Manager{
		tokenStore:   o.tokenStore,
		store:        o.store,
		toolRegistry: o.toolRegistry,
		specBuilder:  o.specBuilder,
	}
	if o.store != nil {
		mgr.elicitation = newElicitationHandler(o.store)
	}

	// Connect providers concurrently, preserving config order via indexed results.
	slots := make([]providerSlot, len(enabled))
	var wg sync.WaitGroup
	wg.Add(len(enabled))
	for i, cfg := range enabled {
		go func(idx int, c core.ProviderConfig) {
			defer wg.Done()
			p, err := connectProviderFunc(ctx, c, mgr.buildClientOptions(c), o.tokenStore)
			slot := providerSlot{cfg: c}
			if err != nil {
				slot.lastErr = err
				slot.startupStatus = "failed"
			} else {
				slot.p = p
				slot.startupStatus = "healthy"
			}
			slots[idx] = slot
		}(i, cfg)
	}
	wg.Wait()

	mgr.mu.Lock()
	mgr.slots = slots
	mgr.mu.Unlock()
	for i := range slots {
		if slots[i].startupStatus != "healthy" {
			continue
		}
		if err := mgr.registerProviderTools(ctx, slots[i].cfg.Name); err != nil {
			mgr.failSlot(slots[i].cfg.Name, err)
		}
	}

	var failed []error
	healthy := 0
	mgr.mu.RLock()
	for _, s := range mgr.slots {
		if s.startupStatus == "healthy" {
			healthy++
			continue
		}
		failed = append(failed, fmt.Errorf("MCP provider %s: %w", s.cfg.Name, s.lastErr))
	}
	mgr.mu.RUnlock()
	if healthy == 0 {
		return nil, fmt.Errorf("all MCP providers failed: %w", errors.Join(failed...))
	}
	for _, err := range failed {
		slog.Error("MCP provider left out of runs", "error", err)
	}
	return mgr, nil
}

// failSlot closes a connected provider whose tools could not be registered
// and marks it failed, as a provider that could not connect.
func (m *Manager) failSlot(providerName string, cause error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.slots {
		if m.slots[i].cfg.Name != providerName {
			continue
		}
		if err := m.slots[i].p.close(); err != nil {
			cause = errors.Join(cause, fmt.Errorf("close: %w", err))
		}
		m.slots[i].p, m.slots[i].lastErr, m.slots[i].startupStatus = nil, cause, "failed"
		return
	}
}

// buildClientOptions assembles a provider's MCP client options. Returns nil
// handlers when the corresponding manager subsystem is not initialized (e.g.
// no store wired).
func (m *Manager) buildClientOptions(cfg core.ProviderConfig) *mcp.ClientOptions {
	return &mcp.ClientOptions{
		ToolListChangedHandler:     m.buildToolListChangedHandler(cfg.Name),
		ResourceListChangedHandler: m.buildResourceListChangedHandler(cfg.Name),
		PromptListChangedHandler:   m.buildPromptListChangedHandler(cfg.Name),
		ElicitationHandler:         m.buildElicitationHandler(),
	}
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
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
