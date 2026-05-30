package web

import (
	"context"
	"log/slog"
	"time"

	"github.com/ycvk/acorn/internal/app"
	"github.com/ycvk/acorn/internal/clientevents"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/skills"
)

type ClientService interface {
	ListThreads(ctx context.Context, limit int) ([]app.Thread, error)
	CreateThread(ctx context.Context, title string) (*app.Thread, error)
	GetThread(ctx context.Context, threadID string) (*app.Thread, error)
	UpdateThread(ctx context.Context, threadID, title string) (*app.Thread, error)
	DeleteThread(ctx context.Context, threadID string) error
	ListMessages(ctx context.Context, threadID string, limit int) ([]app.Message, error)
	CreateMessage(ctx context.Context, threadID, content string) (*app.Message, error)
	CreateRun(ctx context.Context, threadID, skillID, mode string) (*app.Run, error)
	GetRun(ctx context.Context, runID string) (*app.Run, error)
	LoadRunEventsAfter(ctx context.Context, runID string, afterSeq int64) (*clientevents.RunEventBatch, error)
	LoadRunEventsForDetail(ctx context.Context, runID string) (*clientevents.RunEventDetail, error)
	ListRunArtifacts(ctx context.Context, runID string) ([]app.ArtifactSummary, error)
	RunIsTerminal(ctx context.Context, runID string) (bool, error)
	InterruptRun(ctx context.Context, runID string) error
	EventPollInterval() time.Duration
}

type PendingActionService interface {
	List(ctx context.Context, limit int) ([]app.PendingActionSummary, error)
	Get(ctx context.Context, actionID string) (*app.PendingActionDetail, error)
	Decide(ctx context.Context, actionID string, input app.PendingActionDecisionInput) (*events.PendingActionRecord, error)
}

type WorkingCheckpointService interface {
	Get(ctx context.Context, threadID string) (*app.WorkingCheckpointView, error)
	Update(ctx context.Context, threadID, content, relatedSkillID string) (*app.WorkingCheckpointView, error)
	Clear(ctx context.Context, threadID string) error
}

type RunResumeService interface {
	Resume(ctx context.Context, runID string) (*app.RunResult, error)
}

type CapabilityService interface {
	Snapshot(ctx context.Context, opts app.CapabilitySnapshotOptions) app.SystemCapabilities
}

type InboxService interface {
	Load(ctx context.Context) (*app.MobileInbox, error)
}

type MemoryService interface {
	ListFacts(ctx context.Context, selection memorymodule.RecordSelection) ([]memorymodule.Record, error)
	ListSkills(ctx context.Context, selection memorymodule.RecordSelection) ([]memorymodule.Record, error)
	ListHistory(ctx context.Context, selection memorymodule.RecordSelection) ([]memorymodule.Record, error)
	Search(ctx context.Context, req memorymodule.SearchRequest) (*memorymodule.SearchResult, error)
}

type SkillService interface {
	Snapshot(ctx context.Context) (*skills.Snapshot, error)
	List(ctx context.Context, limit int) ([]skills.View, error)
	ListFiltered(ctx context.Context, filter app.SkillListFilter) ([]skills.View, int, error)
	Get(ctx context.Context, id string) (*skills.View, error)
	ReadFile(ctx context.Context, id, relativePath string) (*app.SkillFileView, error)
}

type DeviceAuthService interface {
	PairDevice(ctx context.Context, input app.PairDeviceInput) (*app.PairDeviceResult, error)
	Authenticate(ctx context.Context, rawToken string) (*app.DeviceAuthContext, error)
	ListDevices(ctx context.Context) ([]app.DeviceView, error)
	RevokeDevice(ctx context.Context, deviceID string) error
}

type Dependencies struct {
	Client        ClientService
	PendingAction PendingActionService
	Checkpoints   WorkingCheckpointService
	RunResume     RunResumeService
	Memory        MemoryService
	Skills        SkillService
	Capabilities  CapabilityService
	DeviceAuth    DeviceAuthService
	Inbox         InboxService
	Logger        *slog.Logger
	Config        *config.Config
}

type Server struct {
	client        ClientService
	pendingAction PendingActionService
	checkpoints   WorkingCheckpointService
	runResume     RunResumeService
	memory        MemoryService
	skills        SkillService
	capabilities  CapabilityService
	deviceAuth    DeviceAuthService
	inbox         InboxService
	logger        *slog.Logger
	cfg           *config.Config
}
