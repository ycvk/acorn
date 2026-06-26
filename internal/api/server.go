package api

import (
	"log/slog"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/memory"
)

type Dependencies struct {
	Threads       *ThreadService
	Runs          *RunService
	Events        *EventService
	PendingAction *PendingActionService
	RunResume     *RunResumeService
	Memory        memory.Service
	Skills        *SkillService
	Capabilities  *CapabilitiesService
	DeviceAuth    *DeviceAuthService
	Inbox         *InboxService
	Logger        *slog.Logger
	Config        *config.Config
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
	logger        *slog.Logger
	cfg           *config.Config
}
