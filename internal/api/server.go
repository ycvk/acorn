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
	Captures           *CaptureService
	PhoneNotifications *PhoneNotificationService
	Logger             *slog.Logger
	Config             *config.Config
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
	captures           *CaptureService
	phoneNotifications *PhoneNotificationService
	logger             *slog.Logger
	cfg                *config.Config
}
