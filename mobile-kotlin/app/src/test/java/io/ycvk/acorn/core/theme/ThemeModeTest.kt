package io.ycvk.acorn.core.theme

import org.junit.Assert.assertEquals
import org.junit.Test

class ThemeModeTest {
    @Test
    fun `only the system mode follows the system`() {
        for (systemDark in listOf(false, true)) {
            assertEquals(systemDark, ThemeMode.System.isDark(systemDark))
            assertEquals(false, ThemeMode.Light.isDark(systemDark))
            assertEquals(true, ThemeMode.Dark.isDark(systemDark))
        }
        assertEquals(listOf("跟随系统", "浅色", "深色"), ThemeMode.entries.map { it.label })
    }
}
