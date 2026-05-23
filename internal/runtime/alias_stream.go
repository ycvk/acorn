package runtime

import "github.com/ycvk/acorn/internal/runtime/stream"

// Stream types re-exported from stream/ subpackage for backward compatibility.
// New code should import stream/ directly.

type StreamItem = stream.StreamItem
type StreamItemKind = stream.StreamItemKind
type StreamPayload = stream.StreamPayload
type StreamSink = stream.StreamSink

type StreamMessage = stream.StreamMessage
type StreamAssistantDelta = stream.StreamAssistantDelta
type StreamToolCall = stream.StreamToolCall
type StreamToolCallProgress = stream.StreamToolCallProgress
type StreamInterrupt = stream.StreamInterrupt
type StreamInterruptContext = stream.StreamInterruptContext
type StreamPlannedToolCall = stream.StreamPlannedToolCall
type StreamSkillCandidate = stream.StreamSkillCandidate
type StreamSkill = stream.StreamSkill
type StreamSkillRequirements = stream.StreamSkillRequirements
type StreamProcedureActivation = stream.StreamProcedureActivation
type StreamPlan = stream.StreamPlan
type StreamContextPressure = stream.StreamContextPressure
type StreamContextCompressed = stream.StreamContextCompressed
type StreamMemoryPrepared = stream.StreamMemoryPrepared
type StreamMemoryPreparedNudge = stream.StreamMemoryPreparedNudge
type StreamMemoryPreparedEntry = stream.StreamMemoryPreparedEntry
type StreamSkillLifecycle = stream.StreamSkillLifecycle

// Payload types
type RunStartedPayload = stream.RunStartedPayload
type RunCompletedPayload = stream.RunCompletedPayload
type RunFailedPayload = stream.RunFailedPayload
type RunInterruptedPayload = stream.RunInterruptedPayload
type RunResumeRequestedPayload = stream.RunResumeRequestedPayload
type RunArchivedPayload = stream.RunArchivedPayload
type DecisionSelectedPayload = stream.DecisionSelectedPayload
type DecisionBlockedPayload = stream.DecisionBlockedPayload
type SkillDiscoveredPayload = stream.SkillDiscoveredPayload
type SkillSelectedPayload = stream.SkillSelectedPayload
type SkillLoadedPayload = stream.SkillLoadedPayload
type SkillFailedPayload = stream.SkillFailedPayload
type SkillLifecyclePayload = stream.SkillLifecyclePayload
type MemoryPreparedPayload = stream.MemoryPreparedPayload
type ContextCompressedPayload = stream.ContextCompressedPayload
type ContextPressurePayload = stream.ContextPressurePayload
type AssistantDeltaPayload = stream.AssistantDeltaPayload
type AssistantMessagePayload = stream.AssistantMessagePayload
type ToolCallStartedPayload = stream.ToolCallStartedPayload
type ToolCallProgressPayload = stream.ToolCallProgressPayload
type ToolCallSucceededPayload = stream.ToolCallSucceededPayload
type ToolCallFailedPayload = stream.ToolCallFailedPayload
type ToolCallInterruptedPayload = stream.ToolCallInterruptedPayload
type ProviderDegradedPayload = stream.ProviderDegradedPayload
type MCPProviderLifecyclePayload = stream.MCPProviderLifecyclePayload
type ElicitationPayload = stream.ElicitationPayload
type SamplingPayload = stream.SamplingPayload
type SubagentStartedPayload = stream.SubagentStartedPayload
type SubagentCompletedPayload = stream.SubagentCompletedPayload
type SubagentFailedPayload = stream.SubagentFailedPayload
type HeartbeatPayload = stream.HeartbeatPayload
type ToolParallelBatchStartedPayload = stream.ToolParallelBatchStartedPayload
type ToolParallelBatchCompletedPayload = stream.ToolParallelBatchCompletedPayload
type PlanCreatedPayload = stream.PlanCreatedPayload
type PlanUpdatedPayload = stream.PlanUpdatedPayload
type PlanClearedPayload = stream.PlanClearedPayload
type CrystallizationFailedPayload = stream.CrystallizationFailedPayload
type CrystallizationVerdictPayload = stream.CrystallizationVerdictPayload
type PlanStepStartedPayload = stream.PlanStepStartedPayload
type PlanStepCompletedPayload = stream.PlanStepCompletedPayload
type PlanStepFailedPayload = stream.PlanStepFailedPayload
type PlanStepPayload = stream.PlanStepPayload
type ProviderDegradedEntry = stream.ProviderDegradedEntry
type ProcedureActivationPayload = stream.ProcedureActivationPayload

// Constants
const (
	StreamKindRunStarted                      = stream.StreamKindRunStarted
	StreamKindRunCompleted                    = stream.StreamKindRunCompleted
	StreamKindRunFailed                       = stream.StreamKindRunFailed
	StreamKindRunInterrupted                  = stream.StreamKindRunInterrupted
	StreamKindRunResumeRequested              = stream.StreamKindRunResumeRequested
	StreamKindDecisionSelected                = stream.StreamKindDecisionSelected
	StreamKindDecisionBlocked                 = stream.StreamKindDecisionBlocked
	StreamKindSkillDiscovered                 = stream.StreamKindSkillDiscovered
	StreamKindSkillSelected                   = stream.StreamKindSkillSelected
	StreamKindSkillLoaded                     = stream.StreamKindSkillLoaded
	StreamKindSkillFailed                     = stream.StreamKindSkillFailed
	StreamKindSkillLifecycle                  = stream.StreamKindSkillLifecycle
	StreamKindProcedureActivation             = stream.StreamKindProcedureActivation
	StreamKindMemoryPrepared                  = stream.StreamKindMemoryPrepared
	StreamKindContextPressure                 = stream.StreamKindContextPressure
	StreamKindContextCompressed               = stream.StreamKindContextCompressed
	StreamKindAssistantDelta                  = stream.StreamKindAssistantDelta
	StreamKindAssistantMessage                = stream.StreamKindAssistantMessage
	StreamKindToolCallStarted                 = stream.StreamKindToolCallStarted
	StreamKindToolCallProgress                = stream.StreamKindToolCallProgress
	StreamKindToolCallSucceeded               = stream.StreamKindToolCallSucceeded
	StreamKindToolCallFailed                  = stream.StreamKindToolCallFailed
	StreamKindToolCallInterrupted             = stream.StreamKindToolCallInterrupted
	StreamKindProviderDegraded                = stream.StreamKindProviderDegraded
	StreamKindMCPToolCatalogRefreshed         = stream.StreamKindMCPToolCatalogRefreshed
	StreamKindMCPToolCatalogRefreshFailed     = stream.StreamKindMCPToolCatalogRefreshFailed
	StreamKindMCPProviderAdded                = stream.StreamKindMCPProviderAdded
	StreamKindMCPProviderRemoved              = stream.StreamKindMCPProviderRemoved
	StreamKindMCPProviderRestarted            = stream.StreamKindMCPProviderRestarted
	StreamKindMCPResourceCatalogRefreshed     = stream.StreamKindMCPResourceCatalogRefreshed
	StreamKindMCPResourceCatalogRefreshFailed = stream.StreamKindMCPResourceCatalogRefreshFailed
	StreamKindMCPPromptCatalogRefreshed       = stream.StreamKindMCPPromptCatalogRefreshed
	StreamKindMCPPromptCatalogRefreshFailed   = stream.StreamKindMCPPromptCatalogRefreshFailed
	StreamKindMCPAuthStatusChanged            = stream.StreamKindMCPAuthStatusChanged
	StreamKindElicitationPending              = stream.StreamKindElicitationPending
	StreamKindElicitationDecided              = stream.StreamKindElicitationDecided
	StreamKindSamplingStarted                 = stream.StreamKindSamplingStarted
	StreamKindSamplingCompleted               = stream.StreamKindSamplingCompleted
	StreamKindSamplingFailed                  = stream.StreamKindSamplingFailed
	StreamKindSubagentStarted                 = stream.StreamKindSubagentStarted
	StreamKindSubagentCompleted               = stream.StreamKindSubagentCompleted
	StreamKindSubagentFailed                  = stream.StreamKindSubagentFailed
	StreamKindHeartbeat                       = stream.StreamKindHeartbeat
	StreamKindToolParallelBatchStarted        = stream.StreamKindToolParallelBatchStarted
	StreamKindToolParallelBatchCompleted      = stream.StreamKindToolParallelBatchCompleted
	StreamKindRunArchived                     = stream.StreamKindRunArchived
	StreamKindPlanCreated                     = stream.StreamKindPlanCreated
	StreamKindPlanUpdated                     = stream.StreamKindPlanUpdated
	StreamKindPlanCleared                     = stream.StreamKindPlanCleared
	StreamKindStepStarted                     = stream.StreamKindStepStarted
	StreamKindStepCompleted                   = stream.StreamKindStepCompleted
	StreamKindStepFailed                      = stream.StreamKindStepFailed
	StreamKindCrystallizationFailed           = stream.StreamKindCrystallizationFailed
	StreamKindCrystallizationVerdict          = stream.StreamKindCrystallizationVerdict
)

var AppendStreamItem = stream.AppendStreamItem
var StreamItemsFromAgentEvent = stream.StreamItemsFromAgentEvent
var UnmarshalPayload = stream.UnmarshalPayload
var ElicitationPayloadFromStream = stream.ElicitationPayloadFromStream
var SamplingPayloadFromStream = stream.SamplingPayloadFromStream
var StreamMessageFromSchema = stream.StreamMessageFromSchema
var ProjectStreamItemToEvent = stream.ProjectStreamItemToEvent

var withStreamSink = stream.WithStreamSink
var streamSinkFromContext = stream.StreamSinkFromContext

// Lowercase aliases for test backward compatibility
var unmarshalPayload = stream.UnmarshalPayload
var streamMessageFromSchema = stream.StreamMessageFromSchema
var projectStreamItemToEvent = stream.ProjectStreamItemToEvent
