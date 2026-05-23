package runtime

import (
	"context"

	"github.com/ycvk/acorn/internal/runtime/api"
)

// --- Aliases to api/ package ---

// Errors
var (
	ErrRunNotActive      = api.ErrRunNotActive
	ErrRunNotInterrupted = api.ErrRunNotInterrupted
	ErrExecutionNotReady = api.ErrExecutionNotReady
)

// Execute types
type ExecuteRequest = api.ExecuteRequest

// Plan types
type Plan = api.Plan
type PlanStep = api.PlanStep
type PlanStepStatus = api.PlanStepStatus
type PlanStepRisk = api.PlanStepRisk
type PlanRepoTarget = api.PlanRepoTarget
type VerificationIntent = api.VerificationIntent
type PlanStore = api.PlanStore
type PlanRecordStore = api.PlanRecordStore

// Plan constants
const (
	PlanStepPending    = api.PlanStepPending
	PlanStepInProgress = api.PlanStepInProgress
	PlanStepCompleted  = api.PlanStepCompleted
	PlanStepFailed     = api.PlanStepFailed
	PlanStepSkipped    = api.PlanStepSkipped

	PlanStepRiskRead     = api.PlanStepRiskRead
	PlanStepRiskWrite    = api.PlanStepRiskWrite
	PlanStepRiskExecute  = api.PlanStepRiskExecute
	PlanStepRiskDelegate = api.PlanStepRiskDelegate
)

// Evidence types
type PlanEvidence = api.PlanEvidence
type EvidenceKind = api.EvidenceKind
type EvidenceStatus = api.EvidenceStatus

// Evidence constants
const (
	EvidenceKindTool       = api.EvidenceKindTool
	EvidenceKindCommand    = api.EvidenceKindCommand
	EvidenceKindDiff       = api.EvidenceKindDiff
	EvidenceKindCheckpoint = api.EvidenceKindCheckpoint
	EvidenceKindRollback   = api.EvidenceKindRollback
	EvidenceKindTest       = api.EvidenceKindTest
	EvidenceKindSubagent   = api.EvidenceKindSubagent
	EvidenceKindVerifier   = api.EvidenceKindVerifier
	EvidenceKindManual     = api.EvidenceKindManual

	EvidenceStatusRecorded  = api.EvidenceStatusRecorded
	EvidenceStatusPassed    = api.EvidenceStatusPassed
	EvidenceStatusFailed    = api.EvidenceStatusFailed
	EvidenceStatusConfirmed = api.EvidenceStatusConfirmed
)

// Session types
type SessionState = api.SessionState

const (
	SessionStateNew         = api.SessionStateNew
	SessionStateRunning     = api.SessionStateRunning
	SessionStateCompleted   = api.SessionStateCompleted
	SessionStateFailed      = api.SessionStateFailed
	SessionStateInterrupted = api.SessionStateInterrupted
	SessionStateDegraded    = api.SessionStateDegraded
)

var DeriveSessionState = api.DeriveSessionState

// Context helpers
func WithRunID(ctx context.Context, runID string) context.Context {
	return api.WithRunID(ctx, runID)
}
func GetRunID(ctx context.Context) string {
	return api.GetRunID(ctx)
}
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return api.WithSessionID(ctx, sessionID)
}
func SessionIDFromContext(ctx context.Context) string {
	return api.SessionIDFromContext(ctx)
}
func WithStore(ctx context.Context, store any) context.Context {
	return api.WithStore(ctx, store)
}

type EventAppender = api.EventAppender
