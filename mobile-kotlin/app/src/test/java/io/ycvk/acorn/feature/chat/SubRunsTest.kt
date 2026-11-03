package io.ycvk.acorn.feature.chat

import io.ycvk.acorn.api.models.ClientSubRunUpdatedEvent
import io.ycvk.acorn.api.models.Message
import io.ycvk.acorn.api.models.SubRunData
import io.ycvk.acorn.core.sse.ChatState
import io.ycvk.acorn.core.sse.RunEventPacket
import io.ycvk.acorn.core.sse.RunEventProjection
import java.time.OffsetDateTime
import org.junit.Assert.assertEquals
import org.junit.Test

class SubRunsTest {
    private val readA = SubRunData(task = 1, attempt = 1, state = SubRunData.State.running, objective = "读 A")
    private val check = SubRunData(task = 2, attempt = 1, state = SubRunData.State.running, objective = "复核 A 的要点")

    private fun updated(seq: Long, task: SubRunData) = RunEventPacket.SubRunUpdated(
        ClientSubRunUpdatedEvent("run_1:$seq", "run_1", seq, OffsetDateTime.parse("2026-11-03T08:00:00Z"), "sub_run.updated", task),
    )

    @Test
    fun `a run's tasks follow their latest attempt, in task order`() {
        val projection = RunEventProjection()
        val reported = readA.copy(state = SubRunData.State.reported, summary = "三条要点")
        val redo = check.copy(attempt = 2)
        val state = listOf(updated(1, check), updated(2, readA), updated(3, reported), updated(4, check.copy(state = SubRunData.State.reported)), updated(5, redo))
            .fold(ChatState()) { state, packet -> projection.apply(state, packet) }
        assertEquals(listOf(reported, redo), state.subRuns)
        assertEquals(1, runningSubRuns(state.subRuns))
    }

    @Test
    fun `the folded line says how many are at work, or how they ended`() {
        assertEquals("2 个子任务，2 个在做", subRunsHeadline(listOf(readA, check)))
        assertEquals("2 个子任务，1 个失败", subRunsHeadline(listOf(readA.copy(state = SubRunData.State.accepted), check.copy(state = SubRunData.State.failed))))
        assertEquals("2 个子任务都已交回", subRunsHeadline(listOf(readA.copy(state = SubRunData.State.accepted), check.copy(state = SubRunData.State.reported))))
    }

    @Test
    fun `a task names its state, and its attempt once sent back`() {
        assertEquals("在做", subRunStateLabel(readA))
        assertEquals("已收下", subRunStateLabel(readA.copy(state = SubRunData.State.accepted)))
        assertEquals("第 2 次 · 已交回", subRunStateLabel(check.copy(attempt = 2, state = SubRunData.State.reported)))
        assertEquals("失败", subRunStateLabel(check.copy(state = SubRunData.State.failed)))
    }

    @Test
    fun `a stored reply keeps the tasks its run delegated`() {
        val reply = chatMessageFrom(Message.Role.assistant, "汇总好了", subRuns = listOf(readA, check)) as ChatMessage.Assistant
        assertEquals(listOf(readA, check), reply.subRuns)
    }
}
