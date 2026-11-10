package io.ycvk.acorn.feature.approvals

import io.ycvk.acorn.api.models.DecidePendingActionRequest
import io.ycvk.acorn.api.models.DecidePendingActionRequest.Decision
import io.ycvk.acorn.api.models.PendingActionOption
import io.ycvk.acorn.api.models.PendingActionSummary
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test
import java.time.OffsetDateTime

class PendingActionsTest {
    private fun waiting(actionId: String) = PendingActionSummary(
        actionId = actionId,
        runId = "run_$actionId",
        threadId = "thread_1",
        kind = PendingActionSummary.Kind.tool_approval,
        status = PendingActionSummary.Status.pending,
        title = "t",
        options = emptyList(),
        createdAt = OffsetDateTime.parse("2026-11-10T08:00:00Z"),
    )

    @Test
    fun `a decided action keeps its place in the list while its card says what was decided`() {
        val a = waiting("a")
        val b = waiting("b")
        val c = waiting("c")
        val d = waiting("d")
        // The server no longer lists b, which the owner just decided, and has a new one.
        assertEquals(listOf(a, b, c, d), withSettled(previous = listOf(a, b, c), loaded = listOf(a, c, d), settled = setOf("b")))
        // Once its card has had its moment the action follows the server's list.
        assertEquals(listOf(a, c, d), withSettled(previous = listOf(a, b, c), loaded = listOf(a, c, d), settled = emptySet()))
        // One the server still lists is not doubled.
        assertEquals(listOf(a, b), withSettled(previous = listOf(a, b), loaded = listOf(a, b), settled = setOf("b")))
        assertEquals(listOf(c), withSettled(previous = listOf(a, b, c), loaded = emptyList(), settled = setOf("c")))
    }

    @Test
    fun `a decided card says what was decided`() {
        assertEquals("已批准，Acorn 会继续", settledLabel(Decision.accept))
        assertEquals("已拒绝", settledLabel(Decision.decline))
        assertEquals("已回答", settledLabel(Decision.answer))
    }

    @Test
    fun `tool approvals and elicitations decide by the option id`() {
        for (kind in listOf("tool_approval", "elicitation")) {
            assertEquals(
                DecidePendingActionRequest(decision = Decision.accept, selectedOptionId = "accept"),
                optionRequest(kind, PendingActionOption(id = "accept", label = "Accept")),
            )
            assertEquals(
                DecidePendingActionRequest(decision = Decision.decline, selectedOptionId = "decline"),
                optionRequest(kind, PendingActionOption(id = "decline", label = "Decline")),
            )
        }
        assertThrows(IllegalStateException::class.java) {
            optionRequest("tool_approval", PendingActionOption(id = "maybe", label = "Maybe"))
        }
    }

    @Test
    fun `a question answers with the option it names, whatever its label says`() {
        for (label in listOf("No, keep it", "Reject the draft", "Cancel", "Yes")) {
            assertEquals(
                DecidePendingActionRequest(decision = Decision.answer, selectedOptionId = "opt_1"),
                optionRequest("operator_question", PendingActionOption(id = "opt_1", label = label)),
            )
        }
        assertEquals(
            DecidePendingActionRequest(decision = Decision.answer, selectedOptionId = "decline"),
            optionRequest("operator_question", PendingActionOption(id = "decline", label = "Decline the invite")),
        )
    }

    @Test
    fun `a freeform answer sends the trimmed text alone`() {
        assertEquals(
            DecidePendingActionRequest(decision = Decision.answer, answer = "周五下午"),
            freeformAnswer("  周五下午\n"),
        )
    }

    @Test
    fun `declining a question sends neither an option nor an answer`() {
        assertEquals(DecidePendingActionRequest(decision = Decision.decline), declineQuestion)
    }
}
