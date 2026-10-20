package io.ycvk.acorn.feature.chat

import io.ycvk.acorn.api.models.Message
import io.ycvk.acorn.core.sse.StepStatus
import io.ycvk.acorn.core.sse.ToolStep
import io.ycvk.acorn.core.ui.OrbState
import org.junit.Assert.assertEquals
import org.junit.Test
import io.ycvk.acorn.api.models.ToolStep as StoredStep

class ToolStepsTest {
    @Test
    fun `tool search names the tools it loads`() {
        assertEquals("搜索网页、读取网页", stepDetail(ToolStep("1", "tool_search", "select:web_search,web_fetch", StepStatus.Succeeded)))
        assertEquals("go 1.28", stepDetail(ToolStep("2", "web_search", "go 1.28", StepStatus.Running)))
        assertEquals("mystery_tool", toolLabel("mystery_tool"))
    }

    @Test
    fun `a running tool shows the orb for what it does`() {
        assertEquals(OrbState.Searching, toolOrb("web_fetch"))
        assertEquals(OrbState.Searching, toolOrb("recall"))
        assertEquals(OrbState.Working, toolOrb("shell"))
        assertEquals(OrbState.Connecting, toolOrb("mcp__github__list_issues"))
        assertEquals(OrbState.Connecting, toolOrb("skill"))
        assertEquals(OrbState.Solving, toolOrb("ask_operator"))
        assertEquals(OrbState.Weaving, toolOrb("knowledge_write"))
        assertEquals(OrbState.Working, toolOrb("mystery_tool"))
    }

    @Test
    fun `stored steps come with the assistant message`() {
        val message = chatMessageFrom(
            Message.Role.assistant,
            "done",
            listOf(
                StoredStep(toolName = "recall", outcome = StoredStep.Outcome.succeeded, summary = "wifi"),
                StoredStep(toolName = "web_fetch", outcome = StoredStep.Outcome.failed, error = "HTTP 503"),
            ),
        ) as ChatMessage.Assistant
        assertEquals(
            listOf(
                ToolStep("step_0", "recall", "wifi", StepStatus.Succeeded),
                ToolStep("step_1", "web_fetch", "", StepStatus.Failed, "HTTP 503"),
            ),
            message.steps,
        )
    }
}
