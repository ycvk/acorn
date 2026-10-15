package io.ycvk.acorn.feature.persona

import org.junit.Assert.assertEquals
import org.junit.Test
import java.time.OffsetDateTime

class PersonaLabelsTest {
    @Test
    fun `a draft saves only when it changes the persona within the bound`() {
        assertEquals(DraftState.Ready, draftState("你是小橡。", "你是 Acorn。"))
        assertEquals(DraftState.Unchanged, draftState("  你是 Acorn。\n", "你是 Acorn。"))
        assertEquals(DraftState.Empty, draftState(" \n\t", "你是 Acorn。"))
        // A CJK character is three UTF-8 bytes, so the bound is far fewer characters than 8192.
        assertEquals(DraftState.Ready, draftState("界".repeat(MAX_PERSONA_BYTES / 3), "x"))
        assertEquals(DraftState.TooLong, draftState("界".repeat(MAX_PERSONA_BYTES / 3 + 1), "x"))
    }

    @Test
    fun `the size counts trimmed UTF-8 bytes`() {
        assertEquals("0.0 / 8 KB", draftSize("   "))
        assertEquals("3.0 / 8 KB", draftSize("界".repeat(1024)))
    }

    @Test
    fun `a revision names who saved it from where`() {
        assertEquals("默认人格", personaSource("release", null, "device_a"))
        assertEquals("你在这台手机上改的", personaSource("owner", "device_a", "device_a"))
        assertEquals("你在另一台设备上改的", personaSource("owner", "device_b", "device_a"))
        assertEquals("你在服务器上改的", personaSource("owner", null, "device_a"))
    }

    @Test
    fun `revision meta joins the source and how long ago`() {
        val now = OffsetDateTime.parse("2026-10-15T12:00:00Z")
        assertEquals("第 3 版", revisionTitle(3))
        assertEquals("默认人格 · 2 小时前", revisionMeta("release", null, null, now.minusHours(2), now))
    }
}
