package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/memory"
)

type Dependencies struct {
	Threads          *ThreadService
	Runs             *RunService
	Events           *EventService
	PendingAction    *PendingActionService
	RunResume        *RunResumeService
	Memory           memory.Service
	Skills           *SkillService
	Capabilities     *CapabilitiesService
	DeviceAuth       *DeviceAuthService
	Inbox            *InboxService
	TriggerScheduler TriggerScheduler
	Logger           *slog.Logger
	Config           *config.Config
}

type Server struct {
	threads       *ThreadService
	runs          *RunService
	events        *EventService
	pendingAction *PendingActionService
	runResume     *RunResumeService
	memory        memory.Service
	skills        *SkillService
	capabilities  *CapabilitiesService
	deviceAuth    *DeviceAuthService
	inbox         *InboxService
	triggerSched  TriggerScheduler
	logger        *slog.Logger
	cfg           *config.Config
}

// TriggerScheduler is the interface the API layer uses to route webhook
// requests to the trigger scheduler. It is implemented by triggers.Scheduler.
type TriggerScheduler interface {
	HandleWebhook(ctx context.Context, triggerID string, r *http.Request) error
}
