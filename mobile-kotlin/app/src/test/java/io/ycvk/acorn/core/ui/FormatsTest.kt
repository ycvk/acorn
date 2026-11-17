package io.ycvk.acorn.core.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class FormatsTest {
    @Test
    fun `sizes read naturally, and an unknown one says nothing`() {
        assertEquals("2.3 MB", formatBytes(2_400_000))
        assertEquals("1.5 MB", formatBytes(1024L * 1024 * 3 / 2))
        assertEquals("12 KB", formatBytes(12_345))
        assertEquals("2 KB", formatBytes(2048))
        assertEquals("512 B", formatBytes(512))
        assertEquals("", formatBytes(-1))
    }
}
