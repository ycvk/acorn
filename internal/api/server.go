package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/skills"
)

// StoreView is the store contract required by app-facing services.
// It composes the session, identity, and artifact store capabilities.
type StoreView interface {
	core.SessionStore
	core.IdentityStore
	core.ArtifactStore
}

type RunServiceAPI interface {
	CreateRun(ctx context.Context, threadID, skillID, input string) (*Run, error)
	GetRun(ctx context.Context, runID string) (*Run, error)
	RunIsTerminal(ctx context.Context, runID string) (bool, error)
	InterruptRun(ctx context.Context, runID string) error
}

type EventServiceAPI interface {
	LoadRunEventsAfter(ctx context.Context, runID string, afterSeq int64) (*core.RunEventBatch, error)
	LoadRunEventsForDetail(ctx context.Context, runID string) (*core.RunEventDetail, error)
	ListRunArtifacts(ctx context.Context, runID string) ([]ArtifactSummary, error)
	EventPollInterval() time.Duration
}

type ThreadServiceAPI interface {
	ListThreads(ctx context.Context, limit int) ([]Thread, error)
	CreateThread(ctx context.Context, title string) (*Thread, error)
	GetThread(ctx context.Context, threadID string) (*Thread, error)
	UpdateThread(ctx context.Context, threadID, title string) (*Thread, error)
	DeleteThread(ctx context.Context, threadID string) error
	ListMessages(ctx context.Context, threadID string, limit int) ([]Message, error)
	CreateMessage(ctx context.Context, threadID, content string) (*Message, error)
}

type PendingActionServiceAPI interface {
	List(ctx context.Context, limit int) ([]PendingActionSummary, error)
	Get(ctx context.Context, actionID string) (*PendingActionDetail, error)
	Decide(ctx context.Context, actionID string, input PendingActionDecisionInput) (*core.PendingActionRecord, error)
}

type RunResumeServiceAPI interface {
	Resume(ctx context.Context, runID string) (*RunResult, error)
}

type CapabilityServiceAPI interface {
	Snapshot(ctx context.Context, opts CapabilitySnapshotOptions) SystemCapabilities
}

type InboxServiceAPI interface {
	Load(ctx context.Context) (*MobileInbox, error)
}

type MemoryServiceAPI interface {
	ListFacts(ctx context.Context, selection memory.RecordSelection) ([]memory.Record, error)
	ListSkills(ctx context.Context, selection memory.RecordSelection) ([]memory.Record, error)
	ListHistory(ctx context.Context, selection memory.RecordSelection) ([]memory.Record, error)
	Search(ctx context.Context, req memory.SearchRequest) (*memory.SearchResult, error)
}

type SkillServiceAPI interface {
	Snapshot(ctx context.Context) (*skills.Snapshot, error)
	List(ctx context.Context, limit int) ([]skills.View, error)
	ListFiltered(ctx context.Context, filter SkillListFilter) ([]skills.View, int, error)
	Get(ctx context.Context, id string) (*skills.View, error)
	ReadFile(ctx context.Context, id, relativePath string) (*SkillFileView, error)
}

type DeviceAuthServiceAPI interface {
	PairDevice(ctx context.Context, input PairDeviceInput) (*PairDeviceResult, error)
	Authenticate(ctx context.Context, rawToken string) (*DeviceAuthContext, error)
	ListDevices(ctx context.Context) ([]DeviceView, error)
	RevokeDevice(ctx context.Context, deviceID string) error
}

type Dependencies struct {
	Threads       ThreadServiceAPI
	Runs          RunServiceAPI
	Events        EventServiceAPI
	PendingAction PendingActionServiceAPI
	RunResume     RunResumeServiceAPI
	Memory        MemoryServiceAPI
	Skills        SkillServiceAPI
	Capabilities  CapabilityServiceAPI
	DeviceAuth    DeviceAuthServiceAPI
	Inbox         InboxServiceAPI
	Logger        *slog.Logger
	Config        *config.Config
}

type Server struct {
	threads       ThreadServiceAPI
	runs          RunServiceAPI
	events        EventServiceAPI
	pendingAction PendingActionServiceAPI
	runResume     RunResumeServiceAPI
	memory        MemoryServiceAPI
	skills        SkillServiceAPI
	capabilities  CapabilityServiceAPI
	deviceAuth    DeviceAuthServiceAPI
	inbox         InboxServiceAPI
	logger        *slog.Logger
	cfg           *config.Config
}
