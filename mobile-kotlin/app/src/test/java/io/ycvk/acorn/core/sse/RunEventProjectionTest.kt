package io.ycvk.acorn.core.sse

import io.ycvk.acorn.api.models.AssistantDeltaData
import io.ycvk.acorn.api.models.ClientAssistantDeltaEvent
import io.ycvk.acorn.api.models.ClientRunCompletedEvent
import io.ycvk.acorn.api.models.ClientRunFailedEvent
import io.ycvk.acorn.api.models.ClientRunInterruptedEvent
import io.ycvk.acorn.api.models.ClientRunResumeRequestedEvent
import io.ycvk.acorn.api.models.ClientRunStartedEvent
import io.ycvk.acorn.api.models.RunCompletedData
import io.ycvk.acorn.api.models.RunEventAssistantDelta
import io.ycvk.acorn.api.models.RunEventMessage
import io.ycvk.acorn.api.models.RunFailedData
import io.ycvk.acorn.api.models.RunInterruptedData
import io.ycvk.acorn.api.models.RunResumeRequestedData
import io.ycvk.acorn.api.models.RunStartedData
import io.ycvk.acorn.api.models.ClientToolApprovalDecidedEvent
import io.ycvk.acorn.api.models.ClientToolApprovalPendingEvent
import io.ycvk.acorn.api.models.ToolApprovalData
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.OffsetDateTime

class RunEventProjectionTest {

    private val projection = RunEventProjection()
    private val now = OffsetDateTime.now()

    private fun startedEvent() = ClientRunStartedEvent(
        eventId = "evt-1",
        runId = "run-1",
        seq = 1,
        ts = now,
        type = null,
        data = RunStartedData(input = "hello"),
    )

    private fun deltaEvent(seq: Long, delta: String?, reasoning: String? = null) =
        ClientAssistantDeltaEvent(
            eventId = "evt-$seq",
            runId = "run-1",
            seq = seq,
            ts = now,
            type = null,
            data = AssistantDeltaData(
                assistantDelta = RunEventAssistantDelta(
                    sequence = seq.toInt(),
                    delta = delta,
                    reasoning = reasoning,
                ),
            ),
        )

    private fun completedEvent(seq: Long, content: String? = null) =
        ClientRunCompletedEvent(
            eventId = "evt-$seq",
            runId = "run-1",
            seq = seq,
            ts = now,
            type = null,
            data = RunCompletedData(
                message = if (content != null) RunEventMessage(content = content) else null,
            ),
        )

    private fun failedEvent(seq: Long) = ClientRunFailedEvent(
        eventId = "evt-$seq",
        runId = "run-1",
        seq = seq,
        ts = now,
        type = null,
        data = RunFailedData(),
    )

    private fun interruptedEvent(seq: Long) = ClientRunInterruptedEvent(
        eventId = "evt-$seq",
        runId = "run-1",
        seq = seq,
        ts = now,
        type = null,
        data = RunInterruptedData(),
    )

    private fun resumeRequestedEvent(seq: Long) = RunEventPacket.RunResumeRequested(
        ClientRunResumeRequestedEvent(
            eventId = "evt-$seq",
            runId = "run-1",
            seq = seq,
            ts = now,
            type = null,
            data = RunResumeRequestedData(),
        ),
    )

    private fun approvalPending(seq: Long) = RunEventPacket.ToolApprovalPending(
        ClientToolApprovalPendingEvent(
            eventId = "run-1:$seq", runId = "run-1", seq = seq, ts = now, type = null,
            data = ToolApprovalData(actionId = "action_1", toolName = "browser", arguments = "{}"),
        ),
    )

    @Test
    fun `resume row clears on the next delta`() {
        var state = projection.apply(ChatState(runStatus = RunStatus.Interrupted), resumeRequestedEvent(7))
        assertEquals(listOf(ActivityKind.ResumeRequested), state.activities.map { it.kind })
        state = projection.apply(state, RunEventPacket.AssistantDelta(deltaEvent(8, "back")))
        assertTrue(state.activities.isEmpty())
    }

    @Test
    fun `resume row clears on terminal events`() {
        val resumed = projection.apply(ChatState(runStatus = RunStatus.Interrupted), resumeRequestedEvent(7))
        listOf(
            RunEventPacket.RunCompleted(completedEvent(8, "done")),
            RunEventPacket.RunFailed(failedEvent(8)),
            RunEventPacket.RunInterrupted(interruptedEvent(8)),
        ).forEach { terminal ->
            assertTrue(projection.apply(resumed, terminal).activities.isEmpty())
        }
    }

    @Test
    fun `header shows running, waiting on an owner decision, then idle`() {
        var state = projection.apply(ChatState(), RunEventPacket.Started(startedEvent()))
        assertEquals(ChatHeaderStatus.Running, state.headerStatus())

        state = projection.apply(state, approvalPending(2))
        state = projection.apply(state, RunEventPacket.RunInterrupted(interruptedEvent(3)))
        assertEquals(ChatHeaderStatus.Waiting, state.headerStatus())

        state = projection.apply(state, resumeRequestedEvent(4))
        assertEquals(ChatHeaderStatus.Running, state.headerStatus())

        state = projection.apply(state, RunEventPacket.RunCompleted(completedEvent(5, "ok")))
        assertEquals(ChatHeaderStatus.Idle, state.headerStatus())
    }

    @Test
    fun `interrupted without a pending decision is idle`() {
        val state = projection.apply(ChatState(), RunEventPacket.RunInterrupted(interruptedEvent(2)))
        assertEquals(ChatHeaderStatus.Idle, state.headerStatus())
    }

    @Test
    fun `Started resets state to streaming and clears text`() {
        val state = ChatState(assistantText = "old", assistantReasoning = "old reasoning", isStreaming = false, runStatus = RunStatus.Completed)
        val result = projection.apply(state, RunEventPacket.Started(startedEvent()))
        assertTrue(result.isStreaming)
        assertEquals(RunStatus.Running, result.runStatus)
        assertEquals("", result.assistantText)
        assertEquals("", result.assistantReasoning)
    }

    @Test
    fun `AssistantDelta accumulates text and reasoning`() {
        var state = projection.apply(ChatState(), RunEventPacket.Started(startedEvent()))
        state = projection.apply(state, RunEventPacket.AssistantDelta(deltaEvent(2, "Hello")))
        state = projection.apply(state, RunEventPacket.AssistantDelta(deltaEvent(3, " world", reasoning = "thinking")))
        assertEquals("Hello world", state.assistantText)
        assertEquals("thinking", state.assistantReasoning)
        assertTrue(state.isStreaming)
    }

    @Test
    fun `AssistantDelta with null delta does not change text`() {
        var state = projection.apply(ChatState(), RunEventPacket.Started(startedEvent()))
        state = projection.apply(state, RunEventPacket.AssistantDelta(deltaEvent(2, "Hello")))
        state = projection.apply(state, RunEventPacket.AssistantDelta(deltaEvent(3, null)))
        assertEquals("Hello", state.assistantText)
    }

    @Test
    fun `RunCompleted finalizes text and stops streaming`() {
        var state = projection.apply(ChatState(), RunEventPacket.Started(startedEvent()))
        state = projection.apply(state, RunEventPacket.AssistantDelta(deltaEvent(2, "streaming text")))
        state = projection.apply(state, RunEventPacket.RunCompleted(completedEvent(3, "final text")))
        assertFalse(state.isStreaming)
        assertEquals(RunStatus.Completed, state.runStatus)
        assertEquals("final text", state.assistantText)
    }

    @Test
    fun `RunCompleted without message preserves streaming text`() {
        var state = projection.apply(ChatState(), RunEventPacket.Started(startedEvent()))
        state = projection.apply(state, RunEventPacket.AssistantDelta(deltaEvent(2, "streaming text")))
        state = projection.apply(state, RunEventPacket.RunCompleted(completedEvent(3, content = null)))
        assertFalse(state.isStreaming)
        assertEquals(RunStatus.Completed, state.runStatus)
        assertEquals("streaming text", state.assistantText)
    }

    @Test
    fun `RunFailed stops streaming and sets failed status`() {
        var state = projection.apply(ChatState(), RunEventPacket.Started(startedEvent()))
        state = projection.apply(state, RunEventPacket.RunFailed(failedEvent(5)))
        assertFalse(state.isStreaming)
        assertEquals(RunStatus.Failed, state.runStatus)
    }

    @Test
    fun `RunInterrupted stops streaming and sets interrupted status`() {
        var state = projection.apply(ChatState(), RunEventPacket.Started(startedEvent()))
        state = projection.apply(state, RunEventPacket.RunInterrupted(interruptedEvent(6)))
        assertFalse(state.isStreaming)
        assertEquals(RunStatus.Interrupted, state.runStatus)
    }

    @Test
    fun `ToolApproval pending adds a waiting row that its decision clears`() {
        var state = projection.apply(ChatState(), RunEventPacket.Started(startedEvent()))
        state = projection.apply(
            state,
            RunEventPacket.ToolApprovalPending(
                ClientToolApprovalPendingEvent(
                    eventId = "run-1:2", runId = "run-1", seq = 2, ts = now, type = null,
                    data = ToolApprovalData(actionId = "action_1", toolName = "browser", arguments = "{}"),
                ),
            ),
        )
        assertEquals(listOf("Waiting for approval: browser"), state.activities.map { it.label })
        assertEquals(ActivityKind.ToolApproval, state.activities.single().kind)

        state = projection.apply(
            state,
            RunEventPacket.ToolApprovalDecided(
                ClientToolApprovalDecidedEvent(
                    eventId = "run-1:5", runId = "run-1", seq = 5, ts = now, type = null,
                    data = ToolApprovalData(actionId = "action_1", decision = ToolApprovalData.Decision.accept),
                ),
            ),
        )
        assertTrue(state.activities.isEmpty())
    }

    @Test
    fun `Unknown packet leaves state unchanged`() {
        val state = ChatState(assistantText = "hello", isStreaming = true, runStatus = RunStatus.Running)
        val result = projection.apply(state, RunEventPacket.Unknown("unknown_type", "{}"))
        assertEquals(state, result)
    }

    @Test
    fun `full streaming lifecycle - start, deltas, complete`() {
        var state = ChatState()

        state = projection.apply(state, RunEventPacket.Started(startedEvent()))
        assertEquals(RunStatus.Running, state.runStatus)
        assertTrue(state.isStreaming)

        state = projection.apply(state, RunEventPacket.AssistantDelta(deltaEvent(2, "Hello")))
        state = projection.apply(state, RunEventPacket.AssistantDelta(deltaEvent(3, ", ", reasoning = "step 1")))
        state = projection.apply(state, RunEventPacket.AssistantDelta(deltaEvent(4, "world!", reasoning = " step 2")))
        assertEquals("Hello, world!", state.assistantText)
        assertEquals("step 1 step 2", state.assistantReasoning)

        state = projection.apply(state, RunEventPacket.RunCompleted(completedEvent(5, "Hello, world!")))
        assertEquals(RunStatus.Completed, state.runStatus)
        assertFalse(state.isStreaming)
        assertEquals("Hello, world!", state.assistantText)
    }
}
