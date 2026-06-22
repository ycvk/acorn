package io.ycvk.acorn.core.sse

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
            )

            is RunEventPacket.AssistantDelta -> {
                val delta = packet.event.data.assistantDelta
                state.copy(
                    assistantText = state.assistantText + (delta.delta ?: ""),
                    assistantReasoning = state.assistantReasoning + (delta.reasoning ?: ""),
                    isStreaming = true,
                )
            }

            is RunEventPacket.AgentMessage -> {
                val msg = packet.event.data.message
                state.copy(
                    assistantText = msg?.content ?: state.assistantText,
                    assistantReasoning = msg?.reasoning ?: state.assistantReasoning,
                )
            }

            is RunEventPacket.RunCompleted -> {
                val msg = packet.event.data.message
                state.copy(
                    assistantText = msg?.content ?: state.assistantText,
                    assistantReasoning = msg?.reasoning ?: state.assistantReasoning,
                    isStreaming = false,
                    runStatus = RunStatus.Completed,
                )
            }

            is RunEventPacket.RunFailed -> state.copy(
                isStreaming = false,
                runStatus = RunStatus.Failed,
            )

            is RunEventPacket.RunInterrupted -> state.copy(
                isStreaming = false,
                runStatus = RunStatus.Interrupted,
            )

            is RunEventPacket.RunResumeRequested -> state.copy(
                activities = state.activities + ActivityItem(
                    id = packet.eventId,
                    label = "Resume requested",
                    kind = ActivityKind.ResumeRequested,
                ),
            )

            is RunEventPacket.ElicitationPending -> state.copy(
                activities = state.activities + ActivityItem(
                    id = packet.eventId,
                    label = "Elicitation pending",
                    kind = ActivityKind.Elicitation,
                ),
            )

            is RunEventPacket.ElicitationDecided -> state.copy(
                activities = state.activities.filter { it.id != packet.eventId },
            )

            is RunEventPacket.OperatorQuestionPending -> state.copy(
                activities = state.activities + ActivityItem(
                    id = packet.eventId,
                    label = "Operator question",
                    kind = ActivityKind.OperatorQuestion,
                ),
            )

            is RunEventPacket.OperatorQuestionDecided -> state.copy(
                activities = state.activities.filter { it.id != packet.eventId },
            )

            is RunEventPacket.DecisionBlocked -> state.copy(
                activities = state.activities + ActivityItem(
                    id = packet.eventId,
                    label = "Decision blocked",
                    kind = ActivityKind.DecisionBlocked,
                ),
            )

            is RunEventPacket.Unknown -> state // ignore unknown events
        }
    }
}

/**
 * Snapshot of the projected chat state for a single run.
 */
data class ChatState(
    val assistantText: String = "",
    val assistantReasoning: String = "",
    val isStreaming: Boolean = false,
    val runStatus: RunStatus = RunStatus.Idle,
    val activities: List<ActivityItem> = emptyList(),
)

enum class RunStatus { Idle, Running, Completed, Failed, Interrupted }

data class ActivityItem(
    val id: String,
    val label: String,
    val kind: ActivityKind,
)

enum class ActivityKind { ResumeRequested, Elicitation, OperatorQuestion, DecisionBlocked }
