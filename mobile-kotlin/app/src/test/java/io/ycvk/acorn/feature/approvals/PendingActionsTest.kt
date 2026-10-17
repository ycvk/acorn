package io.ycvk.acorn.feature.approvals

import io.ycvk.acorn.api.models.DecidePendingActionRequest
import io.ycvk.acorn.api.models.DecidePendingActionRequest.Decision
import io.ycvk.acorn.api.models.PendingActionOption
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class PendingActionsTest {
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
