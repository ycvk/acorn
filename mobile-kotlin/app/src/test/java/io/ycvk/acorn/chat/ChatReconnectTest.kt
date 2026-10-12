package io.ycvk.acorn.chat

import io.ycvk.acorn.api.models.PendingActionSummary
import io.ycvk.acorn.api.models.RunSummary
import io.ycvk.acorn.feature.chat.reconnectDelayMs
import io.ycvk.acorn.feature.chat.runToFollow
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.time.OffsetDateTime

class ChatReconnectTest {
    private val at = OffsetDateTime.parse("2026-10-12T09:00:00Z")

    private fun run(id: String, thread: String) = RunSummary(
        runId = id, threadId = thread, threadTitle = "", status = RunSummary.Status.running, preview = "",
        lastEventLabel = "", attentionLevel = RunSummary.AttentionLevel.running, durationMs = 0, createdAt = at, updatedAt = at,
    )

    private fun waiting(run: String, thread: String) = PendingActionSummary(
        actionId = "action_$run", runId = run, threadId = thread, kind = PendingActionSummary.Kind.tool_approval,
        status = PendingActionSummary.Status.pending, title = "", options = emptyList(), createdAt = at,
    )

    @Test
    fun followsTheThreadsRunningOrWaitingRun() {
        assertEquals("run_2", runToFollow(listOf(run("run_1", "other"), run("run_2", "thread")), emptyList(), "thread"))
        assertEquals("run_3", runToFollow(emptyList(), listOf(waiting("run_3", "thread")), "thread"))
    }

    @Test
    fun anIdleThreadHasNoRunToFollow() {
        assertNull(runToFollow(listOf(run("run_1", "other")), listOf(waiting("run_2", "other")), "thread"))
    }

    @Test
    fun reconnectsBackOffThenGiveUp() {
        assertEquals(listOf(1_000L, 2_000L, 4_000L, null), (0..3).map(::reconnectDelayMs))
    }
}
