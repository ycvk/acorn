package io.ycvk.acorn.feature.live

import io.ycvk.acorn.api.models.ClientOperatorQuestionDecidedEvent
import io.ycvk.acorn.api.models.ClientOperatorQuestionPendingEvent
import io.ycvk.acorn.api.models.ClientRunCompletedEvent
import io.ycvk.acorn.api.models.ClientRunInterruptedEvent
import io.ycvk.acorn.api.models.ClientRunResumeRequestedEvent
import io.ycvk.acorn.api.models.ClientToolCallStartedEvent
import io.ycvk.acorn.api.models.OperatorQuestionData
import io.ycvk.acorn.api.models.RunCompletedData
import io.ycvk.acorn.api.models.RunEventMessage
import io.ycvk.acorn.api.models.RunInterruptedData
import io.ycvk.acorn.api.models.RunResumeRequestedData
import io.ycvk.acorn.api.models.ToolCallData
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.sse.RunEventPacket
import io.ycvk.acorn.core.sse.RunEventProjection
import io.ycvk.acorn.core.sse.RunStatus
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.IOException
import java.time.OffsetDateTime

@OptIn(ExperimentalCoroutinesApi::class)
class RunFollowerTest {
    private val profile = ConnectionProfile("https://acorn.test", "device_1", "token")
    private val at = OffsetDateTime.parse("2026-10-22T01:00:00Z")

    /** A stream the test drives: what it opened, and the callbacks of the open one per run. */
    private class FakeStreams : RunStreams {
        class Open(
            val runId: String,
            val afterSeq: Long,
            val onEvent: (RunEventPacket) -> Unit,
            val onError: (Throwable) -> Unit,
            val onClosed: () -> Unit,
        ) {
            var closed = false
        }

        val opened = mutableListOf<Open>()

        fun last(runId: String) = opened.last { it.runId == runId }

        override fun open(
            profile: ConnectionProfile,
            runId: String,
            afterSeq: Long,
            onEvent: (RunEventPacket) -> Unit,
            onError: (Throwable) -> Unit,
            onClosed: () -> Unit,
        ): AutoCloseable {
            val open = Open(runId, afterSeq, onEvent, onError, onClosed)
            opened += open
            return AutoCloseable { open.closed = true }
        }
    }

    private class Harness(scope: TestScope) {
        val streams = FakeStreams()
        var shown = 0
        val ended = mutableListOf<FollowedRun>()
        val updates = mutableListOf<RunUpdate>()
        val follower = RunFollower(
            streams = streams,
            projection = RunEventProjection(),
            liveUpdates = { shown++ },
            endings = { ended += it },
            titles = { _, threadId -> "标题 $threadId" },
            scope = CoroutineScope(StandardTestDispatcher(scope.testScheduler)),
            io = StandardTestDispatcher(scope.testScheduler),
        )

        init {
            scope.backgroundScope.launch(StandardTestDispatcher(scope.testScheduler)) { follower.updates.collect { updates += it } }
        }
    }

    private fun tool(runId: String, seq: Long, name: String, summary: String) = RunEventPacket.ToolCallStarted(
        ClientToolCallStartedEvent("$runId:$seq", runId, seq, at, null, ToolCallData("call_$seq", name, summary)),
    )

    private fun completed(runId: String, seq: Long, reply: String) = RunEventPacket.RunCompleted(
        ClientRunCompletedEvent("$runId:$seq", runId, seq, at, null, RunCompletedData(RunEventMessage(role = "assistant", content = reply))),
    )

    private fun interrupted(runId: String, seq: Long) = RunEventPacket.RunInterrupted(
        ClientRunInterruptedEvent("$runId:$seq", runId, seq, at, null, RunInterruptedData()),
    )

    private fun resumed(runId: String, seq: Long) = RunEventPacket.RunResumeRequested(
        ClientRunResumeRequestedEvent("$runId:$seq", runId, seq, at, null, RunResumeRequestedData()),
    )

    private fun asked(runId: String, seq: Long, actionId: String) = RunEventPacket.OperatorQuestionPending(
        ClientOperatorQuestionPendingEvent("$runId:$seq", runId, seq, at, null, OperatorQuestionData(actionId, question = "要继续吗？")),
    )

    private fun answered(runId: String, seq: Long, actionId: String) = RunEventPacket.OperatorQuestionDecided(
        ClientOperatorQuestionDecidedEvent("$runId:$seq", runId, seq, at, null, OperatorQuestionData(actionId)),
    )

    @Test
    fun `a live run is shown while it runs and ends with its reply`() = runTest {
        val h = Harness(this)
        val key = h.follower.beginSend("读一下这篇")
        assertEquals(listOf("读一下这篇"), h.follower.sending.value.map { it.title })
        h.follower.follow(profile, "run_1", "thread_1", "读一下这篇", live = true, sendKey = key)
        runCurrent()
        assertTrue(h.follower.sending.value.isEmpty())
        assertEquals(RunStatus.Running, h.follower.runs.value.getValue("run_1").state.runStatus)

        h.streams.last("run_1").onEvent(tool("run_1", 2, "web_fetch", "https://go.dev/blog/swisstable"))
        runCurrent()
        val running = h.follower.runs.value.getValue("run_1")
        assertEquals(LiveStatus("读网页", "读取网页：https://go.dev/blog/swisstable"), liveStatus(running.state))

        h.streams.last("run_1").onEvent(completed("run_1", 9, "读完了"))
        runCurrent()
        assertNull(h.follower.runs.value["run_1"])
        assertTrue(h.streams.last("run_1").closed)
        assertEquals(listOf("读完了"), h.ended.map { it.state.assistantText })
        assertTrue(h.updates.last().ended)
        assertTrue(h.shown >= 1)
    }

    @Test
    fun `a run waiting on the owner is kept only while its thread is on screen`() = runTest {
        val h = Harness(this)
        h.follower.follow(profile, "run_live", "thread_a", "a", live = true)
        h.follower.watch("thread_b")
        h.follower.follow(profile, "run_seen", "thread_b", "b", live = false)
        runCurrent()

        h.streams.last("run_live").onEvent(interrupted("run_live", 3))
        h.streams.last("run_seen").onEvent(interrupted("run_seen", 3))
        runCurrent()
        assertNull(h.follower.runs.value["run_live"])
        assertEquals(RunStatus.Interrupted, h.follower.runs.value.getValue("run_seen").state.runStatus)

        h.streams.last("run_seen").onEvent(resumed("run_seen", 4))
        runCurrent()
        assertTrue(h.follower.runs.value.getValue("run_seen").running)

        h.follower.unwatch("thread_b")
        runCurrent()
        assertNull(h.follower.runs.value["run_seen"])
        assertTrue(h.streams.last("run_seen").closed)
        assertTrue(h.ended.isEmpty())
    }

    @Test
    fun `following a run again only makes it live, and a missing title is read from the thread`() = runTest {
        val h = Harness(this)
        h.follower.follow(profile, "run_1", "thread_1", null, live = false)
        runCurrent()
        advanceUntilIdle()
        assertEquals("标题 thread_1", h.follower.runs.value.getValue("run_1").title)
        assertFalse(h.follower.runs.value.getValue("run_1").live)

        h.follower.follow(profile, "run_1", "thread_1", null, live = true)
        runCurrent()
        assertTrue(h.follower.runs.value.getValue("run_1").live)
        assertEquals(1, h.streams.opened.size)
    }

    @Test
    fun `a run followed after the owner's decision is not ended by the wait it replays`() = runTest {
        val h = Harness(this)
        h.follower.follow(profile, "run_1", "thread_1", "t", live = true, decided = "act_2")
        runCurrent()
        val stream = h.streams.last("run_1")
        listOf(
            tool("run_1", 1, "web_search", "eino"),
            asked("run_1", 2, "act_1"),
            interrupted("run_1", 3),
            answered("run_1", 4, "act_1"),
            resumed("run_1", 5),
            asked("run_1", 6, "act_2"),
            interrupted("run_1", 7),
            answered("run_1", 8, "act_2"),
        ).forEach(stream.onEvent)
        runCurrent()
        assertTrue(h.follower.runs.value.getValue("run_1").running)
        assertTrue(h.updates.isEmpty())

        stream.onEvent(resumed("run_1", 9))
        runCurrent()
        val run = h.follower.runs.value.getValue("run_1")
        assertTrue(run.running)
        assertEquals(listOf("web_search"), run.state.steps.map { it.toolName })
        assertEquals(1, h.updates.size)

        // A wait after the decision is a new one.
        stream.onEvent(interrupted("run_1", 10))
        runCurrent()
        assertNull(h.follower.runs.value["run_1"])
        assertTrue(stream.closed)
    }

    @Test
    fun `a broken stream continues after the last event read, and is given up after the retries`() = runTest {
        val h = Harness(this)
        h.follower.follow(profile, "run_1", "thread_1", "t", live = true)
        runCurrent()
        h.streams.last("run_1").onEvent(tool("run_1", 2, "web_search", "eino"))
        runCurrent()

        h.streams.last("run_1").onError(IOException("reset"))
        runCurrent()
        assertTrue(h.streams.opened[0].closed)
        advanceTimeBy(1_001)
        assertEquals(listOf(0L, 2L), h.streams.opened.map { it.afterSeq })
        assertEquals(listOf("web_search"), h.follower.runs.value.getValue("run_1").state.steps.map { it.toolName })

        // Each failure without an event in between waits longer, until the retries are spent.
        repeat(3) {
            h.streams.opened.last().onClosed()
            advanceUntilIdle()
        }
        // advanceUntilIdle leaves work that only the background collector waits on.
        runCurrent()
        assertNull(h.follower.runs.value["run_1"])
        val lost = h.updates.last()
        assertEquals("run_1", lost.run.runId)
        assertTrue(lost.lost != null)
        assertTrue(h.ended.isEmpty())
    }
}
