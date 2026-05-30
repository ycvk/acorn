package app

import (
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/runtime"
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
	model.SessionSummaryStore
	PendingActionCreateStore
}

type containerAppStore interface {
	sessionStore
	runResumeStore
	clientStore
	pendingActionDecisionStore
	decisionStore
	deviceAuthStore
	inboxStore
}
