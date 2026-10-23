package io.ycvk.acorn.feature.approvals

import io.ycvk.acorn.api.models.DecidePendingActionRequest
import io.ycvk.acorn.api.models.PendingActionDetail
import io.ycvk.acorn.api.models.PendingActionOption
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.OffsetDateTime

class ApprovalNotificationsTest {
    @Test
    fun `only pending action pushes are read as such`() {
        assertEquals(
            PendingActionPush("action_1", "thread_1", "发送邮件", "给张三"),
            pendingActionPush(mapOf("type" to "pending_action", "action_id" to "action_1", "thread_id" to "thread_1", "title" to "发送邮件", "body" to "给张三")),
        )
        assertNull(pendingActionPush(mapOf("thread_id" to "thread_1", "run_id" to "run_1")))
        assertNull(pendingActionPush(mapOf("type" to "pending_action", "action_id" to " ")))
    }

    private fun detail(kind: PendingActionDetail.Kind, vararg optionIds: String) = PendingActionDetail(
        actionId = "action_1",
        runId = "run_1",
        threadId = "thread_1",
        kind = kind,
        status = PendingActionDetail.Status.pending,
        title = "t",
        options = optionIds.map { PendingActionOption(id = it, label = it) },
        createdAt = OffsetDateTime.now(),
        payload = emptyMap(),
    )

    @Test
    fun `tool approvals decide from the notification, decline first`() {
        assertEquals(
            listOf("decline" to DecidePendingActionRequest.Decision.decline, "accept" to DecidePendingActionRequest.Decision.accept),
            notificationDecisions(detail(PendingActionDetail.Kind.tool_approval, "accept", "decline")),
        )
        assertTrue(notificationDecisions(detail(PendingActionDetail.Kind.operator_question, "a", "b")).isEmpty())
        assertTrue(notificationDecisions(detail(PendingActionDetail.Kind.tool_approval, "accept")).isEmpty())
    }

    @Test
    fun `tool approvals read as what Acorn wants to do`() {
        assertEquals("Acorn 想读取网页", approvalTitle("Approve web_fetch", "web_fetch"))
        assertEquals("https://go.dev", approvalBody("web_fetch wants to run with arguments: {}", "web_fetch", "https://go.dev"))
        assertEquals("Acorn 想调整新闻源", approvalTitle("Approve news_source", "news_source"))
        assertEquals(null, approvalDetail("把新闻源「知乎热榜」转正。理由：x", "把新闻源「知乎热榜」转正。理由：x"))
        assertEquals("全文，摘要只是开头", approvalDetail("全文，摘要只是开头", "全文…"))
        assertEquals(null, approvalDetail(" ", "正文"))
        assertEquals("周几方便？", approvalTitle("周几方便？", null))
        assertEquals("说明", approvalBody("说明", null, null))
        assertNull(approvalBody("ignored", "knowledge_list", ""))
    }
}
