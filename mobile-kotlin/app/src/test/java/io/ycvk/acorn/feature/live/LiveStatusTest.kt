package io.ycvk.acorn.feature.live

import io.ycvk.acorn.core.sse.ChatState
import io.ycvk.acorn.core.sse.RunStatus
import io.ycvk.acorn.core.sse.StepStatus
import io.ycvk.acorn.core.sse.ToolStep
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class LiveStatusTest {
    private val running = ChatState(isStreaming = true, runStatus = RunStatus.Running)

    @Test
    fun `the status names the running tool, then the reply, then thinking`() {
        val searching = running.copy(
            steps = listOf(
                ToolStep("1", "skill", "views", StepStatus.Succeeded),
                ToolStep("2", "web_search", "eino 最新版本\n第二行", StepStatus.Running),
            ),
        )
        assertEquals(LiveStatus("搜索中", "搜索网页：eino 最新版本"), liveStatus(searching))
        assertEquals(LiveStatus("回复中", "正在写回复"), liveStatus(running.copy(assistantText = "读完了")))
        assertEquals(LiveStatus("思考中", "正在思考"), liveStatus(running))
        val loading = running.copy(steps = listOf(ToolStep("3", "tool_search", "select:show_view,update_view", StepStatus.Running)))
        assertEquals("加载工具：整理成视图、更新视图", liveStatus(loading).text)
    }

    @Test
    fun `every chip fits the status bar`() {
        val tools = listOf(
            "web_search", "web_fetch", "browser", "knowledge_read", "knowledge_write", "recall", "show_view", "send_file",
            "ask_operator", "shell", "files", "skill", "think", "keep", "tool_search", "mcp__fetch__fetch", "unknown_tool",
        )
        tools.forEach { assertTrue("$it -> ${toolChip(it)}", toolChip(it).length <= CHIP_CHARS) }
        assertTrue(Sending.chip.length <= CHIP_CHARS)
    }

    @Test
    fun `live updates show what is being sent and the runs of this phone that run`() {
        val runs = mapOf(
            "run_live" to FollowedRun("run_live", "thread_a", "读一下", live = true, state = running),
            "run_waiting" to FollowedRun("run_waiting", "thread_b", "等审批", live = true, state = running.copy(runStatus = RunStatus.Interrupted)),
            "run_seen" to FollowedRun("run_seen", "thread_c", "别处发起", live = false, state = running),
        )
        val items = liveItems(runs, listOf(PendingSend("key_1", "新对话")))
        assertEquals(listOf("新对话", "读一下"), items.map { it.title })
        assertEquals(Sending, items[0].status)
        assertNull(items[0].runId)
        assertEquals("run_live", items[1].runId)
        assertEquals(liveNotificationId("run_live"), items[1].id)
        val stopping = runs + ("run_live" to runs.getValue("run_live").copy(stopping = true))
        assertEquals(Stopping, liveItems(stopping, emptyList()).single().status)
    }

    @Test
    fun `an ending gives the reply's opening or the failure, and nothing for a run waiting on the owner or stopped`() {
        val done = FollowedRun("run_1", "thread_1", "读一下", live = true, state = ChatState(runStatus = RunStatus.Completed, assistantText = "**读完了**。这篇讲 Swiss Table。"))
        assertEquals(Ending("读一下", "读完了。这篇讲 Swiss Table。"), ending(done))
        val failed = done.copy(state = ChatState(runStatus = RunStatus.Failed, failure = "模型超时"))
        assertEquals(Ending("没能完成：读一下", "模型超时"), ending(failed))
        assertNull(ending(failed.copy(stopping = true)))
        assertNull(ending(done.copy(state = ChatState(runStatus = RunStatus.Interrupted))))
    }
}
