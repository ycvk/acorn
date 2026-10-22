package wire

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/knowledge"
	mcpprovider "github.com/ycvk/acorn/internal/mcp"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/news"
	"github.com/ycvk/acorn/internal/notify"
	"github.com/ycvk/acorn/internal/presence"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/settings"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/views"
	"github.com/ycvk/acorn/internal/watch"
	"github.com/ycvk/acorn/internal/webaccess"
	"github.com/ycvk/acorn/internal/workstation"
)

type containerRuntimeDeps struct {
	memory                *memory.Engine
	skills                *skills.Library
	mcpPendingActionStore core.SessionStore
	toolRegistry          core.ToolRegistry
	runnerFactory         *runtime.RunnerFactory
	runController         *runtime.RunController
	executeRun            func(context.Context, core.ExecuteRequest, core.StreamSink) (*runtime.Result, error)
	resumeRun             func(context.Context, string, map[string]any, core.StreamSink) (*runtime.Result, error)
	settings              *settings.Service
	// notifier is nil when push notifications are not configured.
	notifier     *notify.Sender
	vault        *knowledge.Vault
	watchChecker *watch.Checker
	news         *news.Processor
	// triggers is nil without a workstation.
	triggers      *skills.TriggerRunner
	panels        *views.Feeder
	packageStatus *skills.Status
}

// buildNotifier returns the push sender, or nil and the reason push is off.
func buildNotifier(cfg *config.Config, db *store.Store, owner core.SettingsReader, options buildOptions) (*notify.Sender, string, error) {
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
	sender, err := notify.NewSender(notify.SenderConfig{Store: db, Pusher: client, Clock: options.clock, Settings: owner})
	return sender, "", err
}

func buildContainerRuntimeDeps(ctx context.Context, cfg *config.Config, db *store.Store, options buildOptions) (*containerRuntimeDeps, error) {
	skillLibrary, err := skills.NewLibrary(db, options.clock, runtime.ProviderNamespaces(mcpprovider.ProviderConfigsFromConfig(cfg.MCP.Providers)))
	if err != nil {
		return nil, err
	}
	if err := skillLibrary.Seed(ctx); err != nil {
		return nil, fmt.Errorf("built-in skills: %w", err)
	}
	if err := skillLibrary.CheckProviders(ctx); err != nil {
		return nil, err
	}
	if err := presence.SeedPersona(ctx, db, options.clock()); err != nil {
		return nil, fmt.Errorf("default persona: %w", err)
	}

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
	ownerSettings, err := settings.NewService(db, db, options.clock)
	if err != nil {
		return nil, err
	}
	notifier, notifyDisabled, err := buildNotifier(cfg, db, ownerSettings, options)
	if err != nil {
		return nil, fmt.Errorf("push notifications: %w", err)
	}
	notifyDeps := tools.NotifyToolDeps{Context: ctxBridge, Settings: ownerSettings, DisabledReason: notifyDisabled}
	if notifier != nil {
		notifyDeps.Notifier = notifier
	}
	vault, err := knowledge.NewVault(knowledge.VaultConfig{Store: db, StorageDir: cfg.Runtime.StorageDir, Clock: options.clock, Settings: ownerSettings})
	if err != nil {
		return nil, fmt.Errorf("knowledge base: %w", err)
	}
	memoryEngine, err := buildMemory(ctx, cfg, db, ownerSettings, options.clock, false)
	if err != nil {
		return nil, fmt.Errorf("memory: %w", err)
	}
	webFetcher, webPolicy, err := buildWebFetcher(cfg)
	if err != nil {
		return nil, err
	}
	watchChecker, err := buildWatchChecker(cfg, db, webFetcher, webPolicy, options)
	if err != nil {
		return nil, err
	}
	newsProcessor, err := buildNews(ctx, cfg, db, ownerSettings, webFetcher, options.clock)
	if err != nil {
		return nil, fmt.Errorf("news: %w", err)
	}
	workstationClient, err := buildWorkstationClient(cfg)
	if err != nil {
		return nil, err
	}
	skillDeps := tools.SkillToolDeps{Library: skillLibrary, Context: ctxBridge, Settings: ownerSettings, PackagesDisabledReason: noWorkstation}
	var skillPackages skills.PackageHost
	var skillServers runtime.SkillServerHost
	var triggers *skills.TriggerRunner
	var serverReporter skills.ServerReporter
	if workstationClient != nil {
		skillDeps.Packages, skillDeps.PackagesDisabledReason = workstationClient, ""
		skillPackages, skillServers, serverReporter = workstationClient, workstationClient, workstationClient
		if triggers, err = skills.NewTriggerRunner(skillLibrary, workstationClient, workstation.CommandTimeoutSeconds, workstation.CommandOutputBytes); err != nil {
			return nil, err
		}
	}
	packageStatus, err := skills.NewStatus(skillLibrary, db, serverReporter)
	if err != nil {
		return nil, err
	}
	viewSources := views.Sources{Files: db, Attachments: vault, Links: webaccess.URLPolicy{AllowPrivateNetworks: cfg.WebAccess.AllowPrivateNetworks}}
	panels, err := views.NewFeeder(db, viewSources)
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
			Settings:      ownerSettings,
		},
		Notify: notifyDeps,
		Knowledge: tools.KnowledgeToolDeps{
			Vault:    &knowledge.MemoryView{Vault: vault, Store: db},
			Context:  ctxBridge,
			Settings: ownerSettings,
		},
		Skills: skillDeps,
		Watch: tools.WatchToolDeps{
			Store:    db,
			News:     db,
			Feeds:    webFetcher,
			RSSHub:   strings.TrimSpace(cfg.Watch.RSSHubBaseURL),
			Triggers: skillLibrary,
			Checker:  watchChecker,
			Context:  ctxBridge,
			Clock:    options.clock,
			Settings: ownerSettings,
		},
		Views: tools.ViewToolDeps{
			Store:    db,
			Sources:  viewSources,
			Triggers: skillLibrary,
			Context:  ctxBridge,
		},
	}); err != nil {
		return nil, fmt.Errorf("register native tools: %w", err)
	}

	workstationTools := buildWorkstationTools(cfg, workstationClient, db, ctxBridge)
	var uploads runtime.UploadHost
	var work runtime.WorkHost
	if workstationClient != nil {
		uploads, work = workstationClient, workstationClient
	}
	reviewer, err := buildReviewer(cfg)
	if err != nil {
		return nil, err
	}
	var actionReviewer runtime.ActionReviewer
	if reviewer != nil {
		actionReviewer = reviewer
	}
	runnerFactory, err := runtime.NewRunnerFactory(cfg, db, runtime.RunnerFactoryOptions{
		Attachments:           vault,
		Files:                 db,
		Views:                 db,
		Uploads:               uploads,
		Work:                  work,
		Persona:               db,
		Owner:                 db,
		Skills:                skillLibrary,
		SkillPackages:         skillPackages,
		SkillServers:          skillServers,
		MCPPendingActionStore: mcpPendingActionStore,
		ArtifactService:       artifactSvc,
		ToolRegistry:          toolRegistry,
		Activity:              db,
		MemoryStore:           db,
		Memory:                memoryEngine,
		Commitments:           db,
		PhoneNotifications:    db,
		Workstation:           workstationTools,
		Reviewer:              actionReviewer,
		Clock:                 options.clock,
		Settings:              ownerSettings,
	})
	if err != nil {
		return nil, fmt.Errorf("init runner factory: %w", err)
	}
	announce, err := pendingActionAnnouncement(db, notifier)
	if err != nil {
		return nil, err
	}
	executeRun := func(ctx context.Context, req core.ExecuteRequest, sink core.StreamSink) (*runtime.Result, error) {
		exec, err := runtime.NewExecutorWithRunRuntimeAndController(cfg, db, runnerFactory, runController)
		if err != nil {
			return nil, err
		}
		result, err := exec.ExecuteMessages(ctx, req, sink)
		announce(ctx, result)
		return result, err
	}
	resumeRun := func(ctx context.Context, runID string, targets map[string]any, sink core.StreamSink) (*runtime.Result, error) {
		exec, err := runtime.NewExecutorWithRunRuntimeAndController(cfg, db, runnerFactory, runController)
		if err != nil {
			return nil, err
		}
		result, err := exec.ResumeWithTargets(ctx, runID, targets, sink)
		announce(ctx, result)
		return result, err
	}

	return &containerRuntimeDeps{
		skills:                skillLibrary,
		settings:              ownerSettings,
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
		news:                  newsProcessor,
		triggers:              triggers,
		panels:                panels,
		packageStatus:         packageStatus,
	}, nil
}

// pendingActionAnnouncement returns the hook run after every execution: a run
// that stopped to wait on the owner pushes its pending actions. Without push
// it does nothing. A failed push leaves the action in the app's inbox and is
// logged; it does not fail the run.
func pendingActionAnnouncement(db *store.Store, notifier *notify.Sender) (func(context.Context, *runtime.Result), error) {
	if notifier == nil {
		return func(context.Context, *runtime.Result) {}, nil
	}
	announcer, err := notify.NewPendingActionAnnouncer(db, notifier)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, result *runtime.Result) {
		if result == nil || result.Status != core.RunStatusInterrupted {
			return
		}
		if err := announcer.AnnounceRun(core.DurableContext(ctx), result.RunID); err != nil {
			slog.Error("push pending actions", "run_id", result.RunID, "error", err)
		}
	}, nil
}
