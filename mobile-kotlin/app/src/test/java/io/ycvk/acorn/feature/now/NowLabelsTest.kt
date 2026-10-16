package io.ycvk.acorn.feature.now

import org.junit.Assert.assertEquals
import org.junit.Test
import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.ZoneId

class NowLabelsTest {
    private val shanghai = ZoneId.of("Asia/Shanghai")
    private val today = LocalDate.of(2026, 10, 11)

    @Test
    fun `wake time names today and tomorrow in the phone's zone`() {
        assertEquals(WakeTime("今天", "09:00"), wakeTime(OffsetDateTime.parse("2026-10-11T01:00:00Z"), today, shanghai))
        assertEquals(WakeTime("明天", "07:30"), wakeTime(OffsetDateTime.parse("2026-10-11T23:30:00Z"), today, shanghai))
        assertEquals(WakeTime("10月14日", "09:00"), wakeTime(OffsetDateTime.parse("2026-10-14T01:00:00Z"), today, shanghai))
        assertEquals(WakeTime("10月10日", "23:00"), wakeTime(OffsetDateTime.parse("2026-10-10T15:00:00Z"), today, shanghai))
    }

    @Test
    fun `interval label uses the largest whole unit`() {
        assertEquals("每 30 分钟", intervalLabel(1_800))
        assertEquals("每 2 小时", intervalLabel(7_200))
        assertEquals("每天", intervalLabel(86_400))
        assertEquals("每 2 天", intervalLabel(172_800))
        assertEquals("每 90 分钟", intervalLabel(5_400))
    }

    @Test
    fun `briefing sections follow the headings and drop empty ones`() {
        val body = "周日，08:00。\n\n## 今天的安排\n\n- 没有到期的约定\n- **10:00** 开会\n\n## 追踪\n\n"
        assertEquals(
            listOf(
                BriefingSection(null, "周日，08:00。"),
                BriefingSection("今天的安排", "没有到期的约定 10:00 开会"),
            ),
            briefingSections(body),
        )
    }
}
