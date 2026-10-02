package io.ycvk.acorn.core.push

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PushSettingsTest {

    @Test
    fun `missing lists every blank setting`() {
        val settings = PushSettings(projectId = "p", appId = "", apiKey = " ", senderId = "1")
        assertEquals(listOf("acorn.firebase.appId", "acorn.firebase.apiKey"), settings.missing())
        assertTrue(PushSettings("p", "a", "k", "1").missing().isEmpty())
    }

    @Test
    fun `not configured label names the missing settings`() {
        val label = pushStatusLabel(PushStatus.NotConfigured(listOf("acorn.firebase.apiKey")))
        assertTrue(label.contains("acorn.firebase.apiKey"))
        assertEquals("on", pushStatusLabel(PushStatus.Registered))
        assertTrue(pushStatusLabel(PushStatus.Failed("timeout")).contains("timeout"))
    }

    @Test
    fun `thread id comes from the notification extras`() {
        val extras = mapOf(EXTRA_THREAD_ID to " session_1 ")
        assertEquals("session_1", threadIdFromExtras { extras[it] })
        assertNull(threadIdFromExtras { null })
        assertNull(threadIdFromExtras { "  " })
    }
}
