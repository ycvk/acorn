package io.ycvk.acorn.core.ui

import java.time.DayOfWeek
import java.time.Duration
import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter

private val clock = DateTimeFormatter.ofPattern("HH:mm")

fun weekdayName(day: DayOfWeek): String = when (day) {
    DayOfWeek.MONDAY -> "周一"
    DayOfWeek.TUESDAY -> "周二"
    DayOfWeek.WEDNESDAY -> "周三"
    DayOfWeek.THURSDAY -> "周四"
    DayOfWeek.FRIDAY -> "周五"
    DayOfWeek.SATURDAY -> "周六"
    DayOfWeek.SUNDAY -> "周日"
}

fun clockTime(at: OffsetDateTime, zone: ZoneId): String = at.atZoneSameInstant(zone).format(clock)

/** A list timestamp: "14:05" today, then "昨天", a weekday within the week, or a date. */
fun listTime(at: OffsetDateTime, today: LocalDate, zone: ZoneId): String {
    val local = at.atZoneSameInstant(zone)
    val date = local.toLocalDate()
    return when {
        date == today -> local.format(clock)
        date == today.minusDays(1) -> "昨天"
        date.isAfter(today.minusDays(7)) -> weekdayName(date.dayOfWeek)
        date.year == today.year -> "${date.monthValue}月${date.dayOfMonth}日"
        else -> "${date.year}年${date.monthValue}月${date.dayOfMonth}日"
    }
}

/** How long ago: "刚刚", "5 分钟前", "3 小时前", "2 天前", or a date. */
fun ago(at: OffsetDateTime, now: OffsetDateTime): String {
    val minutes = Duration.between(at, now).toMinutes()
    return when {
        minutes < 1 -> "刚刚"
        minutes < 60 -> "$minutes 分钟前"
        minutes < 1440 -> "${minutes / 60} 小时前"
        minutes < 10080 -> "${minutes / 1440} 天前"
        else -> "${at.monthValue}月${at.dayOfMonth}日"
    }
}

enum class DayGroup(val label: String) { Today("今天"), Yesterday("昨天"), Week("最近 7 天"), Earlier("更早") }

fun dayGroup(at: OffsetDateTime, today: LocalDate, zone: ZoneId): DayGroup {
    val date = at.atZoneSameInstant(zone).toLocalDate()
    return when {
        date == today -> DayGroup.Today
        date == today.minusDays(1) -> DayGroup.Yesterday
        date.isAfter(today.minusDays(7)) -> DayGroup.Week
        else -> DayGroup.Earlier
    }
}

private val headingMarker = Regex("""(^|\s)#{1,6}\s+""")
private val listMarker = Regex("""(^|\s)(?:[-*+]|\d+\.)\s+""")
private val quoteMarker = Regex("""(^|\s)>\s+""")
private val emphasis = Regex("""\*\*|__|~~|`""")
private val link = Regex("""\[([^\]]*)]\([^)]*\)""")

/**
 * Flattens a markdown snippet for a one-paragraph preview. A leading copy of
 * [title] is dropped since the title is already shown above it.
 */
fun plainSnippet(snippet: String, title: String = ""): String {
    val flat = snippet
        .replace(link, "$1")
        .replace(headingMarker, "$1")
        .replace(listMarker, "$1")
        .replace(quoteMarker, "$1")
        .replace(emphasis, "")
        .replace(Regex("""\s+"""), " ")
        .trim()
    return if (title.isNotBlank() && flat.startsWith(title.trim())) flat.removePrefix(title.trim()).trimStart(' ', '，', ',', '。', ':', '：') else flat
}
