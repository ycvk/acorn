package wire

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/knowledge"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/notify"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/watch"
)

type containerRuntimeDeps struct {
	memory                *memory.Engine
	loader                *skills.Loader
	mcpPendingActionStore core.SessionStore
	toolRegistry          core.ToolRegistry
	runnerFactory         *runtime.RunnerFactory
	runController         *runtime.RunController
	executeRun            func(context.Context, core.ExecuteRequest, core.StreamSink) (*runtime.Result, error)
	resumeRun             func(context.Context, string, map[string]any, core.StreamSink) (*runtime.Result, error)
	// notifier is nil when push notifications are not configured.
	notifier     *notify.Sender
	vault        *knowledge.Vault
	watchChecker *watch.Checker
	watchBrowser *tools.Service
}

// buildNotifier returns the push sender, or nil and the reason push is off.
func buildNotifier(cfg *config.Config, db *store.Store, loc *time.Location, options buildOptions) (*notify.Sender, string, error) {
	file := cfg.Notify.FCM.ServiceAccountFile
	if file == "" {
		return nil, "notify.fcm.service_account_file is not configured", nil
	}
	account, err := config.LoadFCMServiceAccount(file)
	if err != nil {
		return nil, "", err
	}
	var client *notify.FCMClient
	if options.fcmEndpoint != "" {
		client, err = notify.NewFCMClientAt(notify.ServiceAccount(account), options.fcmEndpoint)
	} else {
		client, err = notify.NewFCMClient(notify.ServiceAccount(account))
	}
	if err != nil {
		return nil, "", err
	}
	var quiet notify.QuietHours
	if cfg.Notify.QuietHours.Start != "" {
		if quiet.Start, err = config.ParseClock(cfg.Notify.QuietHours.Start); err != nil {
			return nil, "", err
		}
		if quiet.End, err = config.ParseClock(cfg.Notify.QuietHours.End); err != nil {
			return nil, "", err
		}
	}
	sender, err := notify.NewSender(notify.SenderConfig{
		Store: db, Pusher: client, Clock: options.clock, Location: loc,
		MaxPerHour: cfg.Notify.MaxPerHour, Quiet: quiet,
	})
	return sender, "", err
}

func buildContainerRuntimeDeps(ctx context.Context, cfg *config.Config, db *store.Store, options buildOptions) (*containerRuntimeDeps, error) {
	loader := skills.NewLoader(cfg.Skills.Dir)

	runController := runtime.NewRunController()

	var mcpPendingActionStore core.SessionStore = db

	artifactSvc, err := store.NewArtifactService(
		filepath.Join(cfg.Runtime.StorageDir, "artifacts"),
		db,
	)
	if err != nil {
		return nil, fmt.Errorf("artifact service: %w", err)
	}

	ctxBridge := runtime.NewContextBridge()
	ownerLoc, err := cfg.OwnerLocation()
	if err != nil {
		return nil, err
	}
	notifier, notifyDisabled, err := buildNotifier(cfg, db, ownerLoc, options)
	if err != nil {
		return nil, fmt.Errorf("push notifications: %w", err)
	}
	notifyDeps := tools.NotifyToolDeps{Context: ctxBridge, Location: ownerLoc, DisabledReason: notifyDisabled}
	if notifier != nil {
		notifyDeps.Notifier = notifier
	}
	vault, err := knowledge.NewVault(knowledge.VaultConfig{Store: db, StorageDir: cfg.Runtime.StorageDir, Clock: options.clock, Location: ownerLoc})
	if err != nil {
		return nil, fmt.Errorf("knowledge base: %w", err)
	}
	memoryEngine, err := buildMemory(ctx, cfg, db, options.clock, false)
	if err != nil {
		return nil, fmt.Errorf("memory: %w", err)
	}
	watchChecker, watchBrowser, err := buildWatchChecker(cfg, db, options)
	if err != nil {
		return nil, err
	}
	toolRegistry := tools.NewToolRegistry()
	if err := tools.RegisterNativeTools(toolRegistry, tools.NativeToolDeps{
		ArtifactService: artifactSvc,
		ArtifactContext: ctxBridge,
		OperatorStore:   mcpPendingActionStore,
		OperatorContext: ctxBridge,
		Presence: tools.PresenceToolDeps{
			Store:         db,
			Memory:        memoryEngine,
			ForgetBarrier: memoryForgetBarrier(runController, db),
			Context:       ctxBridge,
			Clock:         options.clock,
			Location:      ownerLoc,
		},
		Notify: notifyDeps,
		Knowledge: tools.KnowledgeToolDeps{
			Vault:    &knowledge.MemoryView{Vault: vault, Store: db},
			Context:  ctxBridge,
			Location: ownerLoc,
		},
		Watch: tools.WatchToolDeps{
			Store:    db,
			Checker:  watchChecker,
			Context:  ctxBridge,
			Clock:    options.clock,
			Location: ownerLoc,
		},
	}); err != nil {
		return nil, fmt.Errorf("register native tools: %w", err)
	}

	runnerFactory, err := runtime.NewRunnerFactory(cfg, db, runtime.RunnerFactoryOptions{
		Attachments:           vault,
		Loader:                loader,
		MCPPendingActionStore: mcpPendingActionStore,
		ArtifactService:       artifactSvc,
		ToolRegistry:          toolRegistry,
		Activity:              db,
		MemoryStore:           db,
		Memory:                memoryEngine,
		Commitments:           db,
		PhoneNotifications:    db,
		Clock:                 options.clock,
	})
	if err != nil {
		return nil, fmt.Errorf("init runner factory: %w", err)
	}
	executeRun := func(ctx context.Context, req core.ExecuteRequest, sink core.StreamSink) (*runtime.Result, error) {
		exec, err := runtime.NewExecutorWithRunRuntimeAndController(cfg, db, runnerFactory, runController)
		if err != nil {
			return nil, err
		}
		return exec.ExecuteMessages(ctx, req, sink)
	}
	resumeRun := func(ctx context.Context, runID string, targets map[string]any, sink core.StreamSink) (*runtime.Result, error) {
		exec, err := runtime.NewExecutorWithRunRuntimeAndController(cfg, db, runnerFactory, runController)
		if err != nil {
			return nil, err
		}
		return exec.ResumeWithTargets(ctx, runID, targets, sink)
	}

	return &containerRuntimeDeps{
		loader:                loader,
		memory:                memoryEngine,
		mcpPendingActionStore: mcpPendingActionStore,
		toolRegistry:          toolRegistry,
		runnerFactory:         runnerFactory,
		runController:         runController,
		executeRun:            executeRun,
		resumeRun:             resumeRun,
		notifier:              notifier,
		vault:                 vault,
		watchChecker:          watchChecker,
		watchBrowser:          watchBrowser,
	}, nil
}
