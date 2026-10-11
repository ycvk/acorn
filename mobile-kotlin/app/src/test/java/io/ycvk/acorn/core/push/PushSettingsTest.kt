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
    fun `status detail names the missing settings and the failure`() {
        val detail = pushStatusDetail(PushStatus.NotConfigured(listOf("acorn.firebase.apiKey")))
        assertTrue(detail!!.contains("acorn.firebase.apiKey"))
        assertEquals("已开启", pushStatusLabel(PushStatus.Registered))
        assertNull(pushStatusDetail(PushStatus.Registered))
        assertEquals("timeout", pushStatusDetail(PushStatus.Failed("timeout")))
    }

    @Test
    fun `thread id comes from the notification extras`() {
        val extras = mapOf(EXTRA_THREAD_ID to " session_1 ")
        assertEquals("session_1", threadIdFromExtras { extras[it] })
        assertNull(threadIdFromExtras { null })
        assertNull(threadIdFromExtras { "  " })
    }
}
