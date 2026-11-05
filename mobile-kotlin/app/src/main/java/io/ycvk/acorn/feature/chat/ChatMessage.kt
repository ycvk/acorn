package io.ycvk.acorn.feature.chat

import io.ycvk.acorn.api.models.Message
import io.ycvk.acorn.api.models.SubRunData
import io.ycvk.acorn.api.models.ThreadFile
import io.ycvk.acorn.api.models.View
import io.ycvk.acorn.core.sse.StepStatus
import io.ycvk.acorn.core.sse.ToolStep
import io.ycvk.acorn.api.models.ToolStep as StoredStep
import io.ycvk.acorn.data.repository.FileView
import io.ycvk.acorn.data.repository.fileView
import java.util.UUID

sealed class ChatMessage {
    abstract val id: String

    data class User(
        val text: String,
        val files: List<FileView> = emptyList(),
        override val id: String = "user_${UUID.randomUUID()}",
    ) : ChatMessage()

    data class Assistant(
        val text: String,
        val reasoning: String? = null,
        val steps: List<ToolStep> = emptyList(),
        val files: List<FileView> = emptyList(),
        val views: List<View> = emptyList(),
        /** Tasks the run handed to sub-agents, each at its latest attempt. */
        val subRuns: List<SubRunData> = emptyList(),
        override val id: String = "assistant_${UUID.randomUUID()}",
    ) : ChatMessage()

    /**
     * The input of a run the backend woke: a commitment, a watch, the
     * morning briefing, a goal or a follow-up. [summary] is its short form,
     * null for an input shown in full.
     */
    data class Wake(
        val text: String,
        val source: WakeSource,
        val summary: WakeSummary? = wakeSummary(source, text),
        override val id: String = "wake_${UUID.randomUUID()}",
    ) : ChatMessage()
}

/**
 * An owner message that waits for the thread's run, which takes it before its
 * next step. [queuedId] is the server's id for it, null while it is on its way.
 */
data class QueuedChat(
    val text: String,
    val files: List<FileView> = emptyList(),
    val queuedId: String? = null,
    val key: String = "queued_${UUID.randomUUID()}",
)

/** Maps a stored thread message to its chat row, keyed by the message's [id] so a reload keeps each row. */
fun chatMessageFrom(
    role: Message.Role,
    text: String,
    steps: List<StoredStep> = emptyList(),
    files: List<ThreadFile> = emptyList(),
    views: List<View> = emptyList(),
    subRuns: List<SubRunData> = emptyList(),
    id: String = UUID.randomUUID().toString(),
): ChatMessage = when (role) {
    Message.Role.user -> ChatMessage.User(text, files.map(::fileView), id = id)
    Message.Role.wake -> ChatMessage.Wake(text, wakeSource(text), id = id)
    Message.Role.assistant -> ChatMessage.Assistant(
        text,
        steps = steps.mapIndexed { index, step -> toolStep(index, step) },
        files = files.map(::fileView),
        views = views,
        subRuns = subRuns,
        id = id,
    )
    // System / tool messages render as assistant bubbles so the
    // history reads top-to-bottom without gaps.
    Message.Role.system, Message.Role.tool -> ChatMessage.Assistant(text, id = id)
}

/** [messages] with [view] in place of the view with its id, wherever a reply shows it. */
fun withView(messages: List<ChatMessage>, view: View): List<ChatMessage> = messages.map { message ->
    if (message is ChatMessage.Assistant && message.views.any { it.id == view.id }) {
        message.copy(views = message.views.map { if (it.id == view.id) view else it })
    } else {
        message
    }
}

enum class WakeSource { Commitment, Watch, Briefing, Night, Wander, FollowUp, Goal }

/** What woke a run, from the prefix the backend gives its input. */
fun wakeSource(input: String): WakeSource = when {
    input.startsWith("[night ") -> WakeSource.Night
    input.startsWith("[wander ") -> WakeSource.Wander
    input.startsWith("[watch") -> WakeSource.Watch
    input.startsWith("[briefing") -> WakeSource.Briefing
    input.startsWith("[follow_up ") -> WakeSource.FollowUp
    input.startsWith("[goal ") -> WakeSource.Goal
    else -> WakeSource.Commitment
}

/** A wake input in short: [title] beside its icon, [body] the gist, [detail] what else matters, if anything. */
data class WakeSummary(val title: String, val body: String, val detail: String? = null)

private val goalInput = Regex("""^\[goal #\d+, run (\d+) of (\d+)] Objective: (.*?)\n(The last run stopped when Acorn restarted; check what it left unfinished\.\n)?Keep working toward the objective""", RegexOption.DOT_MATCHES_ALL)
private val followUpInput = Regex("""^\[follow_up #\d+, set [^\]]+] (.*)\nWhat happened: (.*)$""", RegexOption.DOT_MATCHES_ALL)

/** The short form of a goal's or a follow-up's run input as the backend writes it, or null to show the input in full. */
fun wakeSummary(source: WakeSource, input: String): WakeSummary? = when (source) {
    WakeSource.Goal -> goalInput.find(input)?.destructured?.let { (run, of, objective, restarted) ->
        WakeSummary("继续目标 · 第 $run/$of 轮", objective.trim(), "上一轮因 Acorn 重启中断".takeIf { restarted.isNotEmpty() })
    }
    WakeSource.FollowUp -> followUpInput.find(input)?.destructured?.let { (task, happened) ->
        WakeSummary("等到结果", task.trim(), happenedText(happened.trim()))
    }
    else -> null
}

private val happenings: List<Pair<Regex, (MatchResult.Destructured) -> String>> = listOf(
    Regex("""^(job \S+ .+) without printing "(.*)"$""") to { (end, text) -> "${happenedText(end)}，没有输出「$text」" },
    Regex("""^job (\S+) exited with code (-?\d+)$""") to { (job, code) -> "任务 $job 已结束，退出码 $code" },
    Regex("""^job (\S+) printed "(.*)"$""") to { (job, text) -> "任务 $job 输出了「$text」" },
    Regex("""^job (\S+) is no longer on the workstation$""") to { (job) -> "工作机上已没有任务 $job" },
    Regex("""^job (\S+) is stopped(?:: (.*))?$""") to { (job, note) -> "任务 $job 已停止" + note.ifEmpty { null }?.let { "：$it" }.orEmpty() },
    Regex("""^job (\S+) is lost(?:: (.*))?$""") to { (job, note) -> "任务 $job 在工作机重启时中断" + note.ifEmpty { null }?.let { "：$it" }.orEmpty() },
    Regex("""^directory (.+) exists$""") to { (path) -> "目录 $path 已出现" },
    Regex("""^(.+) is ready: (\d+) bytes, unchanged since the previous check$""") to { (path, size) -> "$path 已就绪（$size 字节）" },
    Regex("""^its deadline (.+?) passed; no workstation is configured to check it$""") to { (at) -> "到了期限 $at，没有工作机可以检查" },
    Regex("""^its deadline (.+?) passed; checking failed: (.*)$""") to { (at, error) -> "到了期限 $at，检查失败：$error" },
    Regex("""^its deadline (.+?) passed; job (\S+) is still running and has not printed "(.*)"$""") to { (at, job, text) -> "到了期限 $at，任务 $job 还在运行，还没输出「$text」" },
    Regex("""^its deadline (.+?) passed; job (\S+) is still running$""") to { (at, job) -> "到了期限 $at，任务 $job 还在运行" },
    Regex("""^its deadline (.+?) passed; (.+) does not exist yet$""") to { (at, path) -> "到了期限 $at，$path 还不存在" },
    Regex("""^its deadline (.+?) passed; (.+) is still being written \((\d+) bytes\)$""") to { (at, path, size) -> "到了期限 $at，$path 还在写入（$size 字节）" },
)

/** What woke a follow-up, in Chinese for the outcomes the backend writes; anything else as written. */
fun happenedText(happened: String): String =
    happenings.firstNotNullOfOrNull { (pattern, say) -> pattern.find(happened)?.let { say(it.destructured) } } ?: happened

/** A stored step; ids only need to be unique within the message. */
fun toolStep(index: Int, step: StoredStep): ToolStep = ToolStep(
    id = "step_$index",
    toolName = step.toolName,
    summary = step.summary.orEmpty(),
    status = when (step.outcome) {
        StoredStep.Outcome.running -> StepStatus.Running
        StoredStep.Outcome.succeeded -> StepStatus.Succeeded
        StoredStep.Outcome.failed -> StepStatus.Failed
    },
    error = step.error,
)
