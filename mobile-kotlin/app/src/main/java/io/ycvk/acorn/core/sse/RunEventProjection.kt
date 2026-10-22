package io.ycvk.acorn.core.sse

import io.ycvk.acorn.api.models.View

/**
 * Projects a stream of [RunEventPacket]s into [ChatState] for a chat screen.
 *
 * Accumulates assistant text from streaming deltas, finalises on agent.message /
 * run.completed, tracks run lifecycle, and surfaces pending actions as activity rows.
 */
class RunEventProjection {

    fun apply(state: ChatState, packet: RunEventPacket): ChatState {
        return when (packet) {
            is RunEventPacket.Started -> state.copy(
                isStreaming = true,
                runStatus = RunStatus.Running,
                assistantText = "",
                assistantReasoning = "",
                failure = null,
            )

            is RunEventPacket.AssistantDelta -> {
                val delta = packet.event.data.assistantDelta
                state.copy(
                    assistantText = state.assistantText + (delta.delta ?: ""),
                    assistantReasoning = state.assistantReasoning + (delta.reasoning ?: ""),
                    isStreaming = true,
                    runStatus = RunStatus.Running,
                    activities = state.activities.withoutResumeRows(),
                )
            }

            is RunEventPacket.RunCompleted -> {
                val msg = packet.event.data.message
                state.copy(
                    assistantText = msg?.content ?: state.assistantText,
                    assistantReasoning = msg?.reasoning ?: state.assistantReasoning,
                    isStreaming = false,
                    runStatus = RunStatus.Completed,
                    activities = state.activities.withoutResumeRows(),
                )
            }

            is RunEventPacket.RunFailed -> state.copy(
                isStreaming = false,
                runStatus = RunStatus.Failed,
                failure = packet.event.`data`.error?.takeIf { it.isNotBlank() } ?: "服务器没有给出原因",
                activities = state.activities.withoutResumeRows(),
            )

            is RunEventPacket.RunInterrupted -> state.copy(
                isStreaming = false,
                runStatus = RunStatus.Interrupted,
                activities = state.activities.withoutResumeRows(),
            )

            // The row stays until the resumed run streams again or ends.
            is RunEventPacket.RunResumeRequested -> state.copy(
                isStreaming = true,
                runStatus = RunStatus.Running,
                activities = state.activities + ActivityItem(
                    id = packet.eventId,
                    label = "正在继续",
                    kind = ActivityKind.ResumeRequested,
                ),
            )

            // Waiting rows are keyed by action id so the decided event clears its pending row.
            is RunEventPacket.ElicitationPending -> state.copy(
                activities = state.activities + ActivityItem(
                    id = packet.event.`data`.actionId,
                    label = "等你补充信息",
                    kind = ActivityKind.Elicitation,
                ),
            )

            is RunEventPacket.ElicitationDecided -> state.copy(
                activities = state.activities.filter { it.id != packet.event.`data`.actionId },
            )

            is RunEventPacket.OperatorQuestionPending -> state.copy(
                activities = state.activities + ActivityItem(
                    id = packet.event.`data`.actionId,
                    label = "Acorn 在问你",
                    kind = ActivityKind.OperatorQuestion,
                ),
            )

            is RunEventPacket.OperatorQuestionDecided -> state.copy(
                activities = state.activities.filter { it.id != packet.event.`data`.actionId },
            )

            is RunEventPacket.ToolApprovalPending -> state.copy(
                activities = state.activities + ActivityItem(
                    id = packet.event.`data`.actionId,
                    label = "等你批准：${packet.event.`data`.toolName.orEmpty()}",
                    kind = ActivityKind.ToolApproval,
                ),
            )

            is RunEventPacket.ToolApprovalDecided -> state.copy(
                activities = state.activities.filter { it.id != packet.event.`data`.actionId },
            )

            is RunEventPacket.ToolCallStarted -> {
                val data = packet.event.`data`
                state.copy(
                    steps = state.steps + ToolStep(data.toolCallId, data.toolName, data.summary.orEmpty(), StepStatus.Running),
                    activities = state.activities.withoutResumeRows(),
                )
            }

            is RunEventPacket.ToolCallSucceeded -> state.copy(
                steps = state.steps.finish(packet.event.`data`.toolCallId, packet.event.`data`.toolName, StepStatus.Succeeded, null),
            )

            is RunEventPacket.ToolCallFailed -> state.copy(
                steps = state.steps.finish(packet.event.`data`.toolCallId, packet.event.`data`.toolName, StepStatus.Failed, packet.event.`data`.error),
            )

            // A replayed stream repeats events; a view is shown once.
            is RunEventPacket.ViewShown -> {
                val view = packet.event.`data`.view
                if (state.views.any { it.id == view.id }) state else state.copy(views = state.views + view)
            }

            // A view this run showed is redrawn here; one of an earlier reply lives in the thread's messages.
            is RunEventPacket.ViewUpdated -> {
                val view = packet.event.`data`.view
                state.copy(views = state.views.map { if (it.id == view.id) view else it })
            }

            is RunEventPacket.Unknown -> state // ignore unknown events
        }
    }

    private fun List<ActivityItem>.withoutResumeRows(): List<ActivityItem> =
        filter { it.kind != ActivityKind.ResumeRequested }

    /** Marks a call finished; a result without a start (a stream joined mid-call) adds the step. */
    private fun List<ToolStep>.finish(id: String, name: String, status: StepStatus, error: String?): List<ToolStep> =
        if (any { it.id == id }) {
            map { if (it.id == id) it.copy(status = status, error = error) else it }
        } else {
            this + ToolStep(id, name, "", status, error)
        }
}

/** One tool call the agent made during a run. */
data class ToolStep(
    val id: String,
    val toolName: String,
    val summary: String,
    val status: StepStatus,
    val error: String? = null,
)

enum class StepStatus { Running, Succeeded, Failed }

/**
 * Snapshot of the projected chat state for a single run.
 */
data class ChatState(
    val assistantText: String = "",
    val assistantReasoning: String = "",
    val isStreaming: Boolean = false,
    val runStatus: RunStatus = RunStatus.Idle,
    val activities: List<ActivityItem> = emptyList(),
    /** Why the last run failed, shown until the next run starts. */
    val failure: String? = null,
    /** Tool calls of the current run, in the order they started. */
    val steps: List<ToolStep> = emptyList(),
    /** Views the current run showed, in order. */
    val views: List<View> = emptyList(),
)

enum class RunStatus { Idle, Running, Completed, Failed, Interrupted }

enum class ChatHeaderStatus(val label: String) { Running("正在处理"), Waiting("等你确认"), Idle("") }

/** Waiting means the run is paused on a decision only the owner can make. */
fun ChatState.headerStatus(): ChatHeaderStatus = when {
    runStatus == RunStatus.Running || isStreaming -> ChatHeaderStatus.Running
    runStatus == RunStatus.Interrupted && activities.any { it.kind in ownerDecisionKinds } -> ChatHeaderStatus.Waiting
    else -> ChatHeaderStatus.Idle
}

private val ownerDecisionKinds = setOf(ActivityKind.ToolApproval, ActivityKind.Elicitation, ActivityKind.OperatorQuestion)

data class ActivityItem(
    val id: String,
    val label: String,
    val kind: ActivityKind,
)

enum class ActivityKind { ResumeRequested, Elicitation, OperatorQuestion, ToolApproval }
