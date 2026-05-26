package app

import (
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/runtimehistory"
	storecore "github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/workingstate"
)

type contextPlaneStore interface {
	contextplane.RunContextSnapshotStore
	storecore.ToolResultLedger
}

type containerRuntimeStore interface {
	runtime.RunnerFactoryStore
	workingstate.Store
	runtimehistory.SessionSummaryStore
	notificationStore
	PendingActionCreateStore
}

type containerAppStore interface {
	sessionStore
	traceStore
	sessionStateStore
	runtimeWorkbenchStore
	ChatStore
	clientStore
	pendingActionDecisionStore
	runtime.PendingResumeStore
	decisionStore
	deviceAuthStore
	inboxStore
}
