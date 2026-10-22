package io.ycvk.acorn.feature.settings

import io.ycvk.acorn.api.models.OwnerSettingName
import io.ycvk.acorn.api.models.QuietHours

/** What each setting is called on the settings page and what it does. */
data class SettingCopy(val title: String, val description: String)

fun settingCopy(name: OwnerSettingName): SettingCopy = when (name) {
    OwnerSettingName.owner_timezone -> SettingCopy("时区", "显示时间、按天计算预算、理解“明早八点”这类说法，都按这个时区。")
    OwnerSettingName.briefing_at -> SettingCopy("早安卡", "每天这个时间整理早安卡并推送摘要；关闭后不写。")
    OwnerSettingName.thinking_night_at -> SettingCopy("夜思", "每天这个时间回顾变化的记忆、念头和关切；关闭后不夜思。")
    OwnerSettingName.thinking_wander_at -> SettingCopy("游思", "每个时刻推进一件到期的关切，最多 6 个时刻。")
    OwnerSettingName.notify_quiet_hours -> SettingCopy("免打扰", "这段时间里的推送等到结束时再发。")
    OwnerSettingName.notify_max_per_hour -> SettingCopy("每小时推送上限", "任一小时里最多推送几条，超出的会失败。")
    OwnerSettingName.wake_daily_limit -> SettingCopy("自主唤醒次数", "Acorn 每天自己醒来的次数上限：约定、游思、夜思和追踪；0 表示不自己醒来。")
    OwnerSettingName.wake_daily_tokens -> SettingCopy("自主唤醒 token", "自己醒来的 run 每天能用的 token；0 表示不限。")
    OwnerSettingName.memory_daily_tokens -> SettingCopy("记忆整理 token", "后台从对话里整理记忆每天能用的 token；0 表示不限，用完的留到明天。")
    OwnerSettingName.news_daily_tokens -> SettingCopy("新闻整理 token", "后台给新闻打分归类每天能用的 token；0 表示不限，用完的留到明天。")
    OwnerSettingName.approval_block -> SettingCopy("一律拒绝", "这些工具的调用直接拒绝，优先于其他规则。")
    OwnerSettingName.approval_require -> SettingCopy("每次等我批准", "这些工具每次调用都在手机上等你批准。")
    OwnerSettingName.approval_allow -> SettingCopy("直接执行", "这些工具直接执行，不经审查；其他规则命中的仍按其他规则。")
}

/** A time of day, or 关闭 when empty. */
fun dailyTimeLabel(at: String): String = at.ifEmpty { "关闭" }

fun wanderLabel(times: List<String>): String = if (times.isEmpty()) "无" else times.joinToString("、")

fun quietHoursLabel(quiet: QuietHours): String = if (quiet.start.isEmpty()) "关闭" else "${quiet.start}–${quiet.end}"

/** 12000 → 1.2 万; smaller counts as they are. */
fun tokensLabel(n: Int): String = when {
    n < 10_000 -> "$n"
    n % 10_000 == 0 -> "${n / 10_000} 万"
    else -> String.format(java.util.Locale.ROOT, "%.1f 万", n / 10_000.0).replace(".0 万", " 万")
}

/** A token budget: 不限 for zero. */
fun tokenBudgetLabel(n: Int): String = if (n == 0) "不限" else tokensLabel(n)

/** The wake count budget: 关闭 for zero. */
fun wakeLimitLabel(n: Int): String = if (n == 0) "关闭" else "$n 次"

fun patternsLabel(patterns: List<String>): String = when {
    patterns.isEmpty() -> "无"
    patterns.size <= 2 -> patterns.joinToString("、")
    else -> patterns.take(2).joinToString("、") + " 等 ${patterns.size} 项"
}

/**
 * The tools a rule can name. ask_operator already waits for the owner, so
 * the rules that block or ask for approval cannot name it.
 */
fun ruleTools(names: List<String>, rule: OwnerSettingName): List<String> = when (rule) {
    OwnerSettingName.approval_block, OwnerSettingName.approval_require -> names.filterNot { it == "ask_operator" }
    else -> names
}

/**
 * Tool names the typed text could mean for a rule: names starting with it,
 * then names containing it, without those the rule already holds.
 */
fun toolSuggestions(names: List<String>, typed: String, taken: List<String>, limit: Int = 8): List<String> {
    val text = typed.trim()
    val open = names.filterNot { it in taken }
    if (text.isEmpty()) return open.take(limit)
    val starts = open.filter { it.startsWith(text, ignoreCase = true) }
    val contains = open.filter { it.contains(text, ignoreCase = true) && it !in starts }
    return (starts + contains).take(limit)
}

/** Time zones for the picker: the phone's first, then the others matching the search. */
fun zoneChoices(all: Collection<String>, phone: String, query: String): List<String> {
    val text = query.trim()
    val matching = all.filter { it.contains('/') && (text.isEmpty() || it.contains(text, ignoreCase = true)) }.sorted()
    return if (text.isEmpty() || phone.contains(text, ignoreCase = true)) listOf(phone) + matching.filterNot { it == phone } else matching
}

/** The digits a time field holds for "HH:MM": "07:30" → "0730". */
fun clockDigits(time: String): String = time.filter(Char::isDigit).take(4)

/** "HH:MM" from the four digits of a time field, or null until they make a time of day. */
fun clockFromDigits(digits: String): String? {
    if (digits.length != 4 || !digits.all(Char::isDigit)) return null
    val hour = digits.take(2).toInt()
    val minute = digits.drop(2).toInt()
    return if (hour in 0..23 && minute in 0..59) digits.take(2) + ":" + digits.drop(2) else null
}
