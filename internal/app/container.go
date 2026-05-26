package app

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/runtime"
	storesqlite "github.com/ycvk/acorn/internal/store/sqlite"
	"github.com/ycvk/acorn/internal/toolfactory"
)

type Container struct {
	cfg           *config.Config
	store         *storesqlite.Store
	runnerFactory *runtime.RunnerFactory
	runController *runtime.RunController
	sessions      *SessionService
	trace         *TraceService
	sessionState  *SessionStateService
	workbench     *RuntimeWorkbenchService
	checkpoints   *WorkingCheckpointService
	skills        *SkillService
	chat          *ChatService
	client        *ClientService
	pendingAction *PendingActionService
	run           *RunService
	resume        *ResumeService
	decision      *DecisionService
	memory        *MemoryService
	capabilities  *CapabilitiesService
	deviceAuth    *DeviceAuthService
	inbox         *InboxService
	notifications *NotificationService
	mcpServer     *mcp.Server
	serveToolset  *toolfactory.Toolset
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

func (c *Container) Sessions() *SessionService {
	return c.sessions
}

func (c *Container) SessionState() *SessionStateService {
	return c.sessionState
}

func (c *Container) Workbench() *RuntimeWorkbenchService {
	return c.workbench
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

func (c *Container) Run() *RunService {
	return c.run
}

func (c *Container) Resume() *ResumeService {
	return c.resume
}

func (c *Container) Memory() *MemoryService {
	return c.memory
}

func (c *Container) Decision() *DecisionService {
	return c.decision
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
