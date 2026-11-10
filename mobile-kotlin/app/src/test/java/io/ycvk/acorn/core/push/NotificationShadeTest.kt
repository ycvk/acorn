package io.ycvk.acorn.core.push

import io.ycvk.acorn.feature.approvals.ApprovalNotifications
import io.ycvk.acorn.feature.live.LiveNotifications
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class NotificationShadeTest {
    private val kept = setOf(ApprovalNotifications.CHANNEL_ID, LiveNotifications.CHANNEL_ID)

    // What the system shows for a push that arrives while the app is away.
    private val systemShown = ShownNotification("FCM-Notification:8123", 0, "fcm_fallback_notification_channel", 1_000)
    private val foregroundPush = ShownNotification(threadTag("thread_1"), 42, PushManager.CHANNEL_ID, 2_000)
    private val runEnding = ShownNotification(null, 7, PushManager.CHANNEL_ID, 3_000)
    private val approval = ShownNotification(null, ApprovalNotifications.notificationId("action_1"), ApprovalNotifications.CHANNEL_ID, 4_000)
    private val liveUpdate = ShownNotification(null, 9, LiveNotifications.CHANNEL_ID, 5_000)
    private val all = listOf(systemShown, foregroundPush, runEnding, approval, liveUpdate)

    @Test
    fun `entering the app clears every notification but approvals and Live Updates`() {
        assertEquals(listOf(systemShown, foregroundPush, runEnding), clearedOnEntry(all, kept))
    }

    @Test
    fun `opening a conversation clears the notifications about it`() {
        assertEquals(listOf(foregroundPush), clearedByThread(all, "thread_1"))
        assertTrue(clearedByThread(all, "thread_2").isEmpty())
    }

    @Test
    fun `a push about the conversation on screen has nothing to add`() {
        assertTrue(onScreen("thread_1", null, open = "thread_1"))
        assertTrue(onScreen("thread_1", "", open = "thread_1"))
        assertFalse(onScreen("thread_1", null, open = "thread_2"))
        assertFalse(onScreen("thread_1", null, open = null))
        assertFalse(onScreen(null, null, open = null))
    }

    @Test
    fun `a push that opens a browser page is shown over its own conversation`() {
        assertFalse(onScreen("thread_1", "https://example.com/login", open = "thread_1"))
    }
}
