package io.ycvk.acorn.feature.live

import io.ycvk.acorn.core.sse.ChatState
import io.ycvk.acorn.core.sse.StepStatus
import io.ycvk.acorn.core.ui.OrbState
import io.ycvk.acorn.feature.chat.stepDetail
import io.ycvk.acorn.feature.chat.toolLabel
import io.ycvk.acorn.feature.chat.toolOrb

/** What a Live Update says about a run: a few characters for the status bar chip, and a line for the card. */
data class LiveStatus(val chip: String, val text: String)

/** The most characters the chip shows in full. */
const val CHIP_CHARS = 6

/** The card's line before a run has its id: the message is on its way. */
val Sending = LiveStatus("发送中", "正在发出")

/** The card's line after the owner asked to stop the run. */
val Stopping = LiveStatus("停止中", "正在停止")

/** Where a run is: the tool it is calling, writing its reply, or thinking. */
fun liveStatus(state: ChatState): LiveStatus {
    val running = state.steps.lastOrNull { it.status == StepStatus.Running }
    return when {
        running != null -> {
            val detail = stepDetail(running).lines().firstOrNull().orEmpty().trim()
            val label = toolLabel(running.toolName)
            LiveStatus(toolChip(running.toolName), if (detail.isEmpty()) label else "$label：$detail")
        }
        state.assistantText.isNotBlank() -> LiveStatus("回复中", "正在写回复")
        else -> LiveStatus("思考中", "正在思考")
    }
}

/** The chip for a tool: what the agent is doing, in at most [CHIP_CHARS] characters. */
fun toolChip(name: String): String = when (name) {
    "web_search" -> "搜索中"
    "web_fetch", "browser" -> "读网页"
    "knowledge_read", "knowledge_search", "knowledge_list" -> "查笔记"
    "knowledge_write", "knowledge_edit" -> "写笔记"
    "recall", "memory_read" -> "回忆中"
    "show_view", "update_view" -> "做视图"
    "send_file" -> "发文件"
    "ask_operator" -> "问你"
    else -> when (toolOrb(name)) {
        OrbState.Searching -> "查找中"
        OrbState.Working -> "动手中"
        OrbState.Connecting -> "用工具"
        OrbState.Solving -> "思考中"
        OrbState.Weaving -> "整理中"
        else -> "处理中"
    }
}
