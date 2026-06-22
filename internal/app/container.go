package app

import (
	"context"
	"errors"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/runtime"
	storesqlite "github.com/ycvk/acorn/internal/store/sqlite"
)

type Container struct {
	cfg           *config.Config
	store         *storesqlite.Store
	runnerFactory *runtime.RunnerFactory
	runController *runtime.RunController
	runResume     *RunResumeService
	skills        *SkillService
	client        *ClientService
	pendingAction *PendingActionService
	memory        *MemoryService
	capabilities  *CapabilitiesService
	deviceAuth    *DeviceAuthService
	inbox         *InboxService
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

func (c *Container) RunResume() *RunResumeService {
	return c.runResume
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

func (c *Container) Capabilities() *CapabilitiesService {
	return c.capabilities
}

func (c *Container) DeviceAuth() *DeviceAuthService {
	return c.deviceAuth
}

func (c *Container) Inbox() *InboxService {
	return c.inbox
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

	committed = true
	return container, nil
}
