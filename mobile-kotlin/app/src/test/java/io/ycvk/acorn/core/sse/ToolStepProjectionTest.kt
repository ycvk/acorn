package io.ycvk.acorn.core.sse

import io.ycvk.acorn.api.models.ClientToolCallFailedEvent
import io.ycvk.acorn.api.models.ClientToolCallStartedEvent
import io.ycvk.acorn.api.models.ClientToolCallSucceededEvent
import io.ycvk.acorn.api.models.ToolCallData
import org.junit.Assert.assertEquals
import org.junit.Test
import java.time.OffsetDateTime

class ToolStepProjectionTest {
    private val projection = RunEventProjection()
    private val now = OffsetDateTime.now()

    private fun started(seq: Long, id: String, name: String, summary: String) = RunEventPacket.ToolCallStarted(
        ClientToolCallStartedEvent(eventId = "evt-$seq", runId = "run-1", seq = seq, ts = now, type = null, data = ToolCallData(toolCallId = id, toolName = name, summary = summary)),
    )

    private fun succeeded(seq: Long, id: String, name: String) = RunEventPacket.ToolCallSucceeded(
        ClientToolCallSucceededEvent(eventId = "evt-$seq", runId = "run-1", seq = seq, ts = now, type = null, data = ToolCallData(toolCallId = id, toolName = name)),
    )

    private fun failed(seq: Long, id: String, name: String, error: String) = RunEventPacket.ToolCallFailed(
        ClientToolCallFailedEvent(eventId = "evt-$seq", runId = "run-1", seq = seq, ts = now, type = null, data = ToolCallData(toolCallId = id, toolName = name, error = error)),
    )

    @Test
    fun `steps follow calls from start to result`() {
        var state = ChatState(isStreaming = true)
        state = projection.apply(state, started(1, "a", "web_search", "go 1.28"))
        state = projection.apply(state, started(2, "b", "web_fetch", "https://go.dev"))
        assertEquals(listOf(StepStatus.Running, StepStatus.Running), state.steps.map { it.status })

        state = projection.apply(state, succeeded(3, "a", "web_search"))
        state = projection.apply(state, failed(4, "b", "web_fetch", "HTTP 503"))
        assertEquals(
            listOf(
                ToolStep("a", "web_search", "go 1.28", StepStatus.Succeeded),
                ToolStep("b", "web_fetch", "https://go.dev", StepStatus.Failed, "HTTP 503"),
            ),
            state.steps,
        )
    }

    @Test
    fun `a result without its start still shows as a step`() {
        val state = projection.apply(ChatState(), succeeded(9, "z", "recall"))
        assertEquals(listOf(ToolStep("z", "recall", "", StepStatus.Succeeded)), state.steps)
    }
}
