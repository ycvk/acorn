package api

import (
	"log/slog"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/settings"
)

type Dependencies struct {
	Threads       *ThreadService
	Runs          *RunService
	Events        *EventService
	PendingAction *PendingActionService
	Skills        *SkillService
	Capabilities  *CapabilitiesService
	DeviceAuth    *DeviceAuthService
	Inbox         *InboxService
	Knowledge     *KnowledgeService
	Memories      *MemoryService
	Portrait      *PortraitService
	Persona       *PersonaService
	Owner         *OwnerService
	Settings      *settings.Service
	Now           *NowService
	// Browser is nil when no workstation is configured.
	Browser BrowserView
	Logger  *slog.Logger
	Config  *config.Config
}

type Server struct {
	threads       *ThreadService
	runs          *RunService
	events        *EventService
	pendingAction *PendingActionService
	skills        *SkillService
	capabilities  *CapabilitiesService
	deviceAuth    *DeviceAuthService
	inbox         *InboxService
	knowledge     *KnowledgeService
	memories      *MemoryService
	portrait      *PortraitService
	persona       *PersonaService
	owner         *OwnerService
	settings      *settings.Service
	now           *NowService
	browser       BrowserView
	logger        *slog.Logger
	cfg           *config.Config
}
