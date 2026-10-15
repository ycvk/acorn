package api

import (
	"log/slog"

	"github.com/ycvk/acorn/internal/config"
)

type Dependencies struct {
	Threads            *ThreadService
	Runs               *RunService
	Events             *EventService
	PendingAction      *PendingActionService
	Skills             *SkillService
	Capabilities       *CapabilitiesService
	DeviceAuth         *DeviceAuthService
	Inbox              *InboxService
	Knowledge          *KnowledgeService
	Memories           *MemoryService
	Persona            *PersonaService
	Captures           *CaptureService
	PhoneNotifications *PhoneNotificationService
	Now                *NowService
	// Browser is nil when no workstation is configured.
	Browser BrowserView
	Logger  *slog.Logger
	Config  *config.Config
}

type Server struct {
	threads            *ThreadService
	runs               *RunService
	events             *EventService
	pendingAction      *PendingActionService
	skills             *SkillService
	capabilities       *CapabilitiesService
	deviceAuth         *DeviceAuthService
	inbox              *InboxService
	knowledge          *KnowledgeService
	memories           *MemoryService
	persona            *PersonaService
	captures           *CaptureService
	phoneNotifications *PhoneNotificationService
	now                *NowService
	browser            BrowserView
	logger             *slog.Logger
	cfg                *config.Config
}
