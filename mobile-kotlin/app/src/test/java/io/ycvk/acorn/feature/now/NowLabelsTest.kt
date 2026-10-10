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
    fun `wake label names today and tomorrow in the phone's zone`() {
        assertEquals("Today 09:00", wakeLabel(OffsetDateTime.parse("2026-10-11T01:00:00Z"), today, shanghai))
        assertEquals("Tomorrow 07:30", wakeLabel(OffsetDateTime.parse("2026-10-11T23:30:00Z"), today, shanghai))
        assertEquals("10/14 09:00", wakeLabel(OffsetDateTime.parse("2026-10-14T01:00:00Z"), today, shanghai))
        assertEquals("10/10 23:00", wakeLabel(OffsetDateTime.parse("2026-10-10T15:00:00Z"), today, shanghai))
    }

    @Test
    fun `interval label uses the largest whole unit`() {
        assertEquals("every 30m", intervalLabel(1_800))
        assertEquals("every 2h", intervalLabel(7_200))
        assertEquals("every 1d", intervalLabel(86_400))
        assertEquals("every 90m", intervalLabel(5_400))
    }
}
