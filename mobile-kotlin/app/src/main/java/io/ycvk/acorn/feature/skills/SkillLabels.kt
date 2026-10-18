package io.ycvk.acorn.feature.skills

import io.ycvk.acorn.api.models.SkillAuthor
import io.ycvk.acorn.api.models.SkillFileDiff
import io.ycvk.acorn.api.models.SkillPackageServer
import io.ycvk.acorn.api.models.SkillPackageTrigger
import io.ycvk.acorn.core.ui.ago
import io.ycvk.acorn.core.ui.clockTime
import java.time.OffsetDateTime
import java.time.ZoneId
import java.util.Locale

/** The server shows the content of text files up to this size. */
const val MAX_SHOWN_FILE_BYTES = 64 * 1024L

/** Where a skill stands for the next conversation. */
enum class SkillStanding { InUse, NotListed, Retired, Invalid }

fun skillStanding(retired: Boolean, eligible: Boolean, problem: String?): SkillStanding = when {
    !problem.isNullOrBlank() -> SkillStanding.Invalid
    retired -> SkillStanding.Retired
    eligible -> SkillStanding.InUse
    else -> SkillStanding.NotListed
}

fun standingLabel(standing: SkillStanding): String = when (standing) {
    SkillStanding.InUse -> "在用"
    SkillStanding.NotListed -> "暂不可用"
    SkillStanding.Retired -> "已停用"
    SkillStanding.Invalid -> "解析不了"
}

/** Who wrote a revision, in the owner's terms. */
fun authorLabel(author: SkillAuthor): String = when (author) {
    SkillAuthor.agent -> "Acorn 改的"
    SkillAuthor.owner -> "你改的"
    SkillAuthor.release -> "随版本发布"
}

fun skillRevisionTitle(revision: Long): String = "第 $revision 版"

fun skillRevisionMeta(author: SkillAuthor, at: OffsetDateTime, now: OffsetDateTime): String = "${authorLabel(author)} · ${ago(at, now)}"

/** Why the next conversation leaves a skill in use out, one phrase per reason the server gives. */
fun disabledReasonLabel(reason: String): String {
    val code = reason.substringBefore(':')
    val items = reason.substringAfter(':', "").split(',').filter { it.isNotBlank() }.joinToString("、")
    return when (code) {
        "missing_secrets" -> "缺少密钥 $items"
        "missing_required_tools" -> "缺少工具 $items"
        "files_need_workstation" -> "附带文件需要工作机"
        else -> reason
    }
}

/** How the package's MCP server runs; a retired skill's server does not run. */
fun serverStateLabel(state: SkillPackageServer.State?, error: String?): String {
    val detail = error?.takeIf { it.isNotBlank() }
    return when (state) {
        SkillPackageServer.State.ready -> "正在运行"
        SkillPackageServer.State.failed -> "启动失败" + (detail?.let { "：$it" } ?: "")
        SkillPackageServer.State.stopped -> if (detail != null) "工作机还在跑旧的一版，下一次用到这个技能时换成这一版" else "没在运行，下一次用到这个技能时启动"
        SkillPackageServer.State.missing_secrets -> "缺少密钥，设置后下一次用到这个技能时启动"
        SkillPackageServer.State.off -> "服务器没有配置工作机，不会运行"
        SkillPackageServer.State.unknown -> "工作机没有回应" + (detail?.let { "：$it" } ?: "")
        null -> "技能已停用，不会运行"
    }
}

fun deliverLabel(deliver: SkillPackageTrigger.Deliver): String = when (deliver) {
    SkillPackageTrigger.Deliver.immediate -> "有新情况时叫醒 Acorn"
    SkillPackageTrigger.Deliver.digest -> "新情况放进早安卡"
}

/** When and how a trigger fires next; a retired skill's triggers do not fire. */
fun triggerStateLabel(trigger: SkillPackageTrigger, now: OffsetDateTime, zone: ZoneId = ZoneId.systemDefault()): String {
    val next = trigger.nextAt?.let { nextTime(it, now, zone) }
    val failures = trigger.failures ?: 0
    val lastError = trigger.lastError?.takeIf { it.isNotBlank() }?.let { "：$it" } ?: ""
    return when (trigger.state) {
        SkillPackageTrigger.State.scheduled -> "下次 ${next ?: "—"}" + if (failures > 0) "，最近连续失败 $failures 次$lastError" else ""
        SkillPackageTrigger.State.failing -> "连续失败 $failures 次，${next ?: "—"} 再试$lastError"
        SkillPackageTrigger.State.unscheduled -> "服务端会在下一轮排上日程"
        SkillPackageTrigger.State.missing_secrets -> "缺少密钥，不会运行"
        SkillPackageTrigger.State.off -> "服务器没有配置工作机，不会运行"
        null -> "技能已停用，不会运行"
    }
}

/** "今天 10:30", "明天 08:00" or "10月20日 09:00". */
fun nextTime(at: OffsetDateTime, now: OffsetDateTime, zone: ZoneId = ZoneId.systemDefault()): String {
    val day = at.atZoneSameInstant(zone).toLocalDate()
    val today = now.atZoneSameInstant(zone).toLocalDate()
    val prefix = when (day) {
        today -> "今天"
        today.plusDays(1) -> "明天"
        else -> "${day.monthValue}月${day.dayOfMonth}日"
    }
    return "$prefix ${clockTime(at, zone)}"
}

fun fileSize(bytes: Long): String = when {
    bytes < 1024 -> "$bytes B"
    bytes < 1024 * 1024 -> String.format(Locale.ROOT, "%.1f KB", bytes / 1024.0)
    else -> String.format(Locale.ROOT, "%.1f MB", bytes / (1024.0 * 1024.0))
}

/** Why a file's content is not on the page. */
fun hiddenFileReason(size: Long): String =
    if (size > MAX_SHOWN_FILE_BYTES) "超过 64 KB，手机上不显示内容" else "不是文本文件，手机上不显示内容"

fun fileChangeLabel(change: SkillFileDiff.Change): String = when (change) {
    SkillFileDiff.Change.added -> "新增"
    SkillFileDiff.Change.removed -> "删除"
    SkillFileDiff.Change.modified -> "修改"
}
