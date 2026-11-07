package io.ycvk.acorn.feature.chat

import io.ycvk.acorn.api.models.ClientPlanUpdatedEvent
import io.ycvk.acorn.api.models.Message
import io.ycvk.acorn.api.models.PlanData
import io.ycvk.acorn.api.models.PlanStep
import io.ycvk.acorn.core.sse.ChatState
import io.ycvk.acorn.core.sse.RunEventPacket
import io.ycvk.acorn.core.sse.RunEventProjection
import java.time.OffsetDateTime
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class RunPlanTest {
    private val read = PlanStep(label = "读三篇文章", status = PlanStep.Status.running, note = "0/3")
    private val write = PlanStep(label = "写汇总", status = PlanStep.Status.pending)

    private fun updated(seq: Long, vararg steps: PlanStep) = RunEventPacket.PlanUpdated(
        ClientPlanUpdatedEvent("run_1:$seq", "run_1", seq, OffsetDateTime.parse("2026-11-07T08:00:00Z"), "plan.updated", PlanData(steps.toList())),
    )

    @Test
    fun `each plan replaces the one before`() {
        val read3 = read.copy(status = PlanStep.Status.done, note = "3/3")
        val writing = write.copy(status = PlanStep.Status.running)
        val state = listOf(updated(1, read, write), updated(2, read3, writing))
            .fold(ChatState()) { state, packet -> RunEventProjection().apply(state, packet) }
        assertEquals(listOf(read3, writing), state.plan)
    }

    @Test
    fun `the header counts the done rows and names the failed ones`() {
        assertEquals("0/2", planCount(listOf(read, write)))
        val ended = listOf(read.copy(status = PlanStep.Status.done), write.copy(status = PlanStep.Status.failed))
        assertEquals("1/2", planCount(ended))
        assertEquals("1 步没做成", planFailures(ended))
        assertNull(planFailures(listOf(read, write)))
    }

    @Test
    fun `a step still running when the run ended reads as not done`() {
        assertEquals("正在做", planStepState(read, live = true))
        assertEquals("没做完", planStepState(read, live = false))
        assertEquals("待做", planStepState(write, live = true))
        assertEquals("已完成", planStepState(read.copy(status = PlanStep.Status.done), live = false))
    }

    @Test
    fun `a stored reply keeps its run's plan`() {
        val reply = chatMessageFrom(Message.Role.assistant, "汇总好了", plan = listOf(read, write)) as ChatMessage.Assistant
        assertEquals(listOf(read, write), reply.plan)
    }
}
