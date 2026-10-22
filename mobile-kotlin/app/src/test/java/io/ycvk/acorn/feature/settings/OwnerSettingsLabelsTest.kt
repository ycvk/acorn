package io.ycvk.acorn.feature.settings

import io.ycvk.acorn.api.models.OwnerSettingName
import io.ycvk.acorn.api.models.QuietHours
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class OwnerSettingsLabelsTest {
    @Test
    fun `every setting says what it does`() {
        OwnerSettingName.entries.forEach {
            val copy = settingCopy(it)
            assertTrue(copy.title.isNotBlank() && copy.description.isNotBlank())
        }
    }

    @Test
    fun `empty times and windows read as off`() {
        assertEquals("关闭", dailyTimeLabel(""))
        assertEquals("08:00", dailyTimeLabel("08:00"))
        assertEquals("无", wanderLabel(emptyList()))
        assertEquals("10:00、15:00", wanderLabel(listOf("10:00", "15:00")))
        assertEquals("关闭", quietHoursLabel(QuietHours(start = "", end = "")))
        assertEquals("23:00–08:00", quietHoursLabel(QuietHours(start = "23:00", end = "08:00")))
    }

    @Test
    fun `budgets read in 万 and zero means no cap or off`() {
        assertEquals("950", tokensLabel(950))
        assertEquals("10 万", tokensLabel(100_000))
        assertEquals("1.2 万", tokensLabel(12_000))
        assertEquals("5 万", tokensLabel(50_000))
        assertEquals("不限", tokenBudgetLabel(0))
        assertEquals("关闭", wakeLimitLabel(0))
        assertEquals("20 次", wakeLimitLabel(20))
    }

    @Test
    fun `long rule lists are shortened`() {
        assertEquals("无", patternsLabel(emptyList()))
        assertEquals("mcp__*、skill_write", patternsLabel(listOf("mcp__*", "skill_write")))
        assertEquals("mcp__*、skill_write 等 4 项", patternsLabel(listOf("mcp__*", "skill_write", "skill_import", "skill_retire")))
    }

    @Test
    fun `tool suggestions put names that start with the text first and skip taken ones`() {
        val names = listOf("knowledge_write", "shell", "skill_import", "skill_write", "web_fetch")
        assertEquals(listOf("skill_import", "skill_write"), toolSuggestions(names, "skill", taken = emptyList()))
        assertEquals(listOf("knowledge_write", "skill_write"), toolSuggestions(names, "write", taken = emptyList()))
        assertEquals(listOf("web_fetch", "knowledge_write"), toolSuggestions(names, "w", taken = listOf("skill_write")))
        assertEquals(listOf("skill_import"), toolSuggestions(names, "skill", taken = listOf("skill_write")))
    }

    @Test
    fun `rules that block or ask for approval cannot name ask_operator`() {
        val names = listOf("ask_operator", "shell")
        assertEquals(listOf("shell"), ruleTools(names, OwnerSettingName.approval_require))
        assertEquals(listOf("shell"), ruleTools(names, OwnerSettingName.approval_block))
        assertEquals(names, ruleTools(names, OwnerSettingName.approval_allow))
    }

    @Test
    fun `the phone's zone comes first`() {
        val all = listOf("Asia/Shanghai", "Asia/Singapore", "UTC", "Europe/London")
        assertEquals(listOf("Asia/Singapore", "Asia/Shanghai", "Europe/London"), zoneChoices(all, "Asia/Singapore", ""))
        assertEquals(listOf("Asia/Shanghai"), zoneChoices(all, "Asia/Singapore", "shang"))
    }

    @Test
    fun `a time field holds four digits and reads them as a time of day`() {
        assertEquals("0730", clockDigits("07:30"))
        assertEquals("", clockDigits(""))
        assertEquals("07:30", clockFromDigits("0730"))
        assertEquals("23:59", clockFromDigits("2359"))
        assertNull(clockFromDigits("073"))
        assertNull(clockFromDigits("2500"))
        assertNull(clockFromDigits("0760"))
    }
}
