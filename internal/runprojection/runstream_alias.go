package runprojection

import (
	"context"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/runstream"
)

// Aliases for runstream types used in projection code

type StreamItem = runstream.StreamItem
type StreamItemKind = runstream.StreamItemKind
type StreamSink = runstream.StreamSink
type StreamPayload = runstream.StreamPayload

type StreamMessage = runstream.StreamMessage
type StreamPlannedToolCall = runstream.StreamPlannedToolCall
type StreamToolCall = runstream.StreamToolCall
type StreamToolCallProgress = runstream.StreamToolCallProgress
type StreamInterrupt = runstream.StreamInterrupt
type StreamInterruptContext = runstream.StreamInterruptContext
type StreamAssistantDelta = runstream.StreamAssistantDelta

type AssistantMessagePayload = runstream.AssistantMessagePayload
type AssistantDeltaPayload = runstream.AssistantDeltaPayload
type ToolCallStartedPayload = runstream.ToolCallStartedPayload
type ToolCallSucceededPayload = runstream.ToolCallSucceededPayload
type ToolCallFailedPayload = runstream.ToolCallFailedPayload
type ToolCallInterruptedPayload = runstream.ToolCallInterruptedPayload
type ToolCallProgressPayload = runstream.ToolCallProgressPayload

type SkillSelectedPayload = runstream.SkillSelectedPayload
type SkillLoadedPayload = runstream.SkillLoadedPayload
type SkillFailedPayload = runstream.SkillFailedPayload
type SkillDiscoveredPayload = runstream.SkillDiscoveredPayload
type SkillLifecyclePayload = runstream.SkillLifecyclePayload
type ProcedureActivationPayload = runstream.ProcedureActivationPayload

type StreamSkill = runstream.StreamSkill
type StreamSkillCandidate = runstream.StreamSkillCandidate
type StreamSkillLifecycle = runstream.StreamSkillLifecycle
type StreamSkillRequirements = runstream.StreamSkillRequirements

type MemoryPreparedPayload = runstream.MemoryPreparedPayload
type ContextCompressedPayload = runstream.ContextCompressedPayload
type ContextPressurePayload = runstream.ContextPressurePayload

type ElicitationPayload = runstream.ElicitationPayload
type SamplingPayload = runstream.SamplingPayload

type SubagentStartedPayload = runstream.SubagentStartedPayload
type SubagentCompletedPayload = runstream.SubagentCompletedPayload
type SubagentFailedPayload = runstream.SubagentFailedPayload

type ProviderDegradedPayload = runstream.ProviderDegradedPayload
type ProviderDegradedEntry = runstream.ProviderDegradedEntry
type MCPProviderLifecyclePayload = runstream.MCPProviderLifecyclePayload

type HeartbeatPayload = runstream.HeartbeatPayload
type RunArchivedPayload = runstream.RunArchivedPayload

type RunStartedPayload = runstream.RunStartedPayload
type RunCompletedPayload = runstream.RunCompletedPayload
type RunFailedPayload = runstream.RunFailedPayload
type RunInterruptedPayload = runstream.RunInterruptedPayload
type RunResumeRequestedPayload = runstream.RunResumeRequestedPayload

type DecisionSelectedPayload = runstream.DecisionSelectedPayload
type DecisionBlockedPayload = runstream.DecisionBlockedPayload

type ToolParallelBatchStartedPayload = runstream.ToolParallelBatchStartedPayload
type ToolParallelBatchCompletedPayload = runstream.ToolParallelBatchCompletedPayload

type PlanCreatedPayload = runstream.PlanCreatedPayload
type PlanUpdatedPayload = runstream.PlanUpdatedPayload
type PlanClearedPayload = runstream.PlanClearedPayload
type PlanStepPayload = runstream.PlanStepPayload
type PlanStepStartedPayload = runstream.PlanStepStartedPayload
type PlanStepCompletedPayload = runstream.PlanStepCompletedPayload
type PlanStepFailedPayload = runstream.PlanStepFailedPayload

type CrystallizationFailedPayload = runstream.CrystallizationFailedPayload
type CrystallizationVerdictPayload = runstream.CrystallizationVerdictPayload

type StreamPlan = runstream.StreamPlan
type PlanStep = runstream.PlanStep
type PlanStepStatus = runstream.PlanStepStatus
type PlanStepRisk = runstream.PlanStepRisk
type PlanRepoTarget = runstream.PlanRepoTarget
type VerificationIntent = runstream.VerificationIntent
type PlanEvidence = runstream.PlanEvidence
type EvidenceKind = runstream.EvidenceKind
type EvidenceStatus = runstream.EvidenceStatus

type StreamMemoryPrepared = runstream.StreamMemoryPrepared
type StreamMemoryPreparedNudge = runstream.StreamMemoryPreparedNudge
type StreamMemoryPreparedEntry = runstream.StreamMemoryPreparedEntry
type StreamContextCompressed = runstream.StreamContextCompressed
type StreamContextPressure = runstream.StreamContextPressure
type StreamProcedureActivation = runstream.StreamProcedureActivation

type Trace = runstream.Trace
type TraceSummary = runstream.TraceSummary
type Result = runstream.Result
type SessionState = runstream.SessionState

type SelectedSkill = contextplane.SelectedSkill

var (
	StreamKindAssistantMessage                = runstream.StreamKindAssistantMessage
	StreamKindAssistantDelta                  = runstream.StreamKindAssistantDelta
	StreamKindToolCallStarted                 = runstream.StreamKindToolCallStarted
	StreamKindToolCallSucceeded               = runstream.StreamKindToolCallSucceeded
	StreamKindToolCallFailed                  = runstream.StreamKindToolCallFailed
	StreamKindToolCallInterrupted             = runstream.StreamKindToolCallInterrupted
	StreamKindToolCallProgress                = runstream.StreamKindToolCallProgress
	StreamKindSkillSelected                   = runstream.StreamKindSkillSelected
	StreamKindSkillLoaded                     = runstream.StreamKindSkillLoaded
	StreamKindSkillFailed                     = runstream.StreamKindSkillFailed
	StreamKindSkillDiscovered                 = runstream.StreamKindSkillDiscovered
	StreamKindSkillLifecycle                  = runstream.StreamKindSkillLifecycle
	StreamKindProcedureActivation             = runstream.StreamKindProcedureActivation
	StreamKindMemoryPrepared                  = runstream.StreamKindMemoryPrepared
	StreamKindContextCompressed               = runstream.StreamKindContextCompressed
	StreamKindContextPressure                 = runstream.StreamKindContextPressure
	StreamKindElicitationPending              = runstream.StreamKindElicitationPending
	StreamKindElicitationDecided              = runstream.StreamKindElicitationDecided
	StreamKindSamplingStarted                 = runstream.StreamKindSamplingStarted
	StreamKindSamplingCompleted               = runstream.StreamKindSamplingCompleted
	StreamKindSamplingFailed                  = runstream.StreamKindSamplingFailed
	StreamKindSubagentStarted                 = runstream.StreamKindSubagentStarted
	StreamKindSubagentCompleted               = runstream.StreamKindSubagentCompleted
	StreamKindSubagentFailed                  = runstream.StreamKindSubagentFailed
	StreamKindProviderDegraded                = runstream.StreamKindProviderDegraded
	StreamKindRunArchived                     = runstream.StreamKindRunArchived
	StreamKindRunStarted                      = runstream.StreamKindRunStarted
	StreamKindRunCompleted                    = runstream.StreamKindRunCompleted
	StreamKindRunFailed                       = runstream.StreamKindRunFailed
	StreamKindRunInterrupted                  = runstream.StreamKindRunInterrupted
	StreamKindRunResumeRequested              = runstream.StreamKindRunResumeRequested
	StreamKindDecisionSelected                = runstream.StreamKindDecisionSelected
	StreamKindDecisionBlocked                 = runstream.StreamKindDecisionBlocked
	StreamKindPlanCreated                     = runstream.StreamKindPlanCreated
	StreamKindPlanUpdated                     = runstream.StreamKindPlanUpdated
	StreamKindPlanCleared                     = runstream.StreamKindPlanCleared
	StreamKindStepStarted                     = runstream.StreamKindStepStarted
	StreamKindStepCompleted                   = runstream.StreamKindStepCompleted
	StreamKindStepFailed                      = runstream.StreamKindStepFailed
	StreamKindCrystallizationFailed           = runstream.StreamKindCrystallizationFailed
	StreamKindCrystallizationVerdict          = runstream.StreamKindCrystallizationVerdict
	StreamKindHeartbeat                       = runstream.StreamKindHeartbeat
	StreamKindToolParallelBatchStarted        = runstream.StreamKindToolParallelBatchStarted
	StreamKindToolParallelBatchCompleted      = runstream.StreamKindToolParallelBatchCompleted
	StreamKindMCPToolCatalogRefreshed         = runstream.StreamKindMCPToolCatalogRefreshed
	StreamKindMCPToolCatalogRefreshFailed     = runstream.StreamKindMCPToolCatalogRefreshFailed
	StreamKindMCPProviderAdded                = runstream.StreamKindMCPProviderAdded
	StreamKindMCPProviderRemoved              = runstream.StreamKindMCPProviderRemoved
	StreamKindMCPProviderRestarted            = runstream.StreamKindMCPProviderRestarted
	StreamKindMCPResourceCatalogRefreshed     = runstream.StreamKindMCPResourceCatalogRefreshed
	StreamKindMCPResourceCatalogRefreshFailed = runstream.StreamKindMCPResourceCatalogRefreshFailed
	StreamKindMCPPromptCatalogRefreshed       = runstream.StreamKindMCPPromptCatalogRefreshed
	StreamKindMCPPromptCatalogRefreshFailed   = runstream.StreamKindMCPPromptCatalogRefreshFailed
	StreamKindMCPAuthStatusChanged            = runstream.StreamKindMCPAuthStatusChanged

	PlanStepPending    = runstream.PlanStepPending
	PlanStepInProgress = runstream.PlanStepInProgress
	PlanStepCompleted  = runstream.PlanStepCompleted
	PlanStepFailed     = runstream.PlanStepFailed
	PlanStepSkipped    = runstream.PlanStepSkipped

	PlanStepRiskRead     = runstream.PlanStepRiskRead
	PlanStepRiskWrite    = runstream.PlanStepRiskWrite
	PlanStepRiskExecute  = runstream.PlanStepRiskExecute
	PlanStepRiskDelegate = runstream.PlanStepRiskDelegate

	EvidenceKindTool       = runstream.EvidenceKindTool
	EvidenceKindCommand    = runstream.EvidenceKindCommand
	EvidenceKindDiff       = runstream.EvidenceKindDiff
	EvidenceKindCheckpoint = runstream.EvidenceKindCheckpoint
	EvidenceKindRollback   = runstream.EvidenceKindRollback
	EvidenceKindTest       = runstream.EvidenceKindTest
	EvidenceKindSubagent   = runstream.EvidenceKindSubagent
	EvidenceKindVerifier   = runstream.EvidenceKindVerifier
	EvidenceKindManual     = runstream.EvidenceKindManual

	EvidenceStatusRecorded  = runstream.EvidenceStatusRecorded
	EvidenceStatusPassed    = runstream.EvidenceStatusPassed
	EvidenceStatusFailed    = runstream.EvidenceStatusFailed
	EvidenceStatusConfirmed = runstream.EvidenceStatusConfirmed

	ErrRunNotActive      = runstream.ErrRunNotActive
	ErrRunNotInterrupted = runstream.ErrRunNotInterrupted
	ErrExecutionNotReady = runstream.ErrExecutionNotReady

	DeriveSessionState = runstream.DeriveSessionState
)

func StreamSinkFromContext(ctx context.Context) StreamSink {
	return runstream.StreamSinkFromContext(ctx)
}

func WithStreamSink(ctx context.Context, sink StreamSink) context.Context {
	return runstream.WithStreamSink(ctx, sink)
}
