package app

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/runtime"
	storesqlite "github.com/ycvk/acorn/internal/store/sqlite"
)

type executorHandle interface {
	Run(ctx context.Context, input, skillID string, sink runtime.StreamSink) (*runtime.Result, error)
	ExecuteMessages(ctx context.Context, req runtime.ExecuteRequest, sink runtime.StreamSink) (*runtime.Result, error)
	ResumeWithTargets(ctx context.Context, runID string, targets map[string]any, sink runtime.StreamSink) (*runtime.Result, error)
}

func newExecutorFactory(cfg *config.Config, store *storesqlite.Store, runnerFactory *runtime.RunnerFactory, controller *runtime.RunController) func(context.Context) (executorHandle, error) {
	return func(_ context.Context) (executorHandle, error) {
		return runtime.NewExecutorWithRunnerFactoryAndController(cfg, store, runnerFactory, controller)
	}
}

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
