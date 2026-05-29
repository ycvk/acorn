package app

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/runtime"
	storesqlite "github.com/ycvk/acorn/internal/store/sqlite"
)

type Container struct {
	cfg           *config.Config
	store         *storesqlite.Store
	runnerFactory *runtime.RunnerFactory
	runController *runtime.RunController
	trace         *TraceService
	checkpoints   *WorkingCheckpointService
	skills        *SkillService
	chat          *ChatService
	client        *ClientService
	pendingAction *PendingActionService
	profiles      *decision.ProfileService
	memory        *MemoryService
	capabilities  *CapabilitiesService
	deviceAuth    *DeviceAuthService
	inbox         *InboxService
	notifications *NotificationService
	mcpServer     *mcp.Server
	serveToolset  *runtime.Toolset
}

func NewContainer(ctx context.Context, cfg *config.Config) (*Container, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	return buildContainer(ctx, cfg)
}

func (c *Container) Config() *config.Config {
	return c.cfg
}

func (c *Container) Trace() *TraceService {
	return c.trace
}

func (c *Container) Checkpoints() *WorkingCheckpointService {
	return c.checkpoints
}

func (c *Container) Chat() *ChatService {
	return c.chat
}

func (c *Container) Client() *ClientService {
	return c.client
}

func (c *Container) PendingAction() *PendingActionService {
	return c.pendingAction
}

func (c *Container) Skills() *SkillService {
	return c.skills
}

func (c *Container) Memory() *MemoryService {
	return c.memory
}

func (c *Container) DecisionProfile() (*decision.ParsedProfile, error) {
	if c == nil || c.profiles == nil {
		return nil, errors.New("decision profile service is nil")
	}
	return c.profiles.Load()
}

func (c *Container) InspectRunDecision(ctx context.Context, runID string) (*decision.Record, error) {
	if c == nil || c.store == nil {
		return nil, errors.New("store is nil")
	}
	return c.store.LoadRunDecision(ctx, runID)
}

func (c *Container) Capabilities() *CapabilitiesService {
	return c.capabilities
}

func (c *Container) DeviceAuth() *DeviceAuthService {
	return c.deviceAuth
}

func (c *Container) Inbox() *InboxService {
	return c.inbox
}

func (c *Container) Notifications() *NotificationService {
	return c.notifications
}

func (c *Container) MCPServer() *mcp.Server {
	return c.mcpServer
}

func (c *Container) Close() error {
	if c == nil {
		return nil
	}
	var errs []error
	if c.serveToolset != nil {
		if err := c.serveToolset.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if c.runnerFactory != nil {
		if err := c.runnerFactory.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if c.store != nil {
		if err := c.store.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func buildContainer(ctx context.Context, cfg *config.Config) (*Container, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}

	runtime.RegisterTypes()

	store, err := storesqlite.Open(cfg.Runtime.StorageDir)
	if err != nil {
		return nil, err
	}

	committed := false
	defer func() {
		if !committed {
			_ = store.Close()
		}
	}()

	deps, err := buildContainerRuntimeDeps(ctx, cfg, store)
	if err != nil {
		return nil, err
	}

	container, err := buildContainerAppServices(cfg, store, deps)
	if err != nil {
		return nil, err
	}
	container.store = store

	mcpServer, serveToolset, err := buildContainerMCPServer(cfg, deps.runnerFactory)
	if err != nil {
		return nil, err
	}
	container.mcpServer = mcpServer
	container.serveToolset = serveToolset

	committed = true
	return container, nil
}
