package io.ycvk.acorn.core.push

import android.app.NotificationManager
import android.content.Context
import dagger.hilt.android.qualifiers.ApplicationContext
import io.ycvk.acorn.feature.approvals.ApprovalNotifications
import io.ycvk.acorn.feature.live.AppVisibility
import io.ycvk.acorn.feature.live.LiveNotifications
import javax.inject.Inject
import javax.inject.Singleton

/** One of the app's notifications in the shade: what cancels it, its channel, and when it was last posted. */
data class ShownNotification(val tag: String?, val id: Int, val channelId: String, val postedAt: Long)

fun NotificationManager.shown(): List<ShownNotification> =
    activeNotifications.map { ShownNotification(it.tag, it.id, it.notification.channelId, it.postTime) }

/** Tags a notification with the conversation it is about, so opening the conversation clears it. */
fun threadTag(threadId: String): String = "thread:$threadId"

/**
 * What entering the app clears: every notification that only says something
 * happened, whichever channel the system or the app showed it on. The
 * channels in [kept] hold the ones that end on their own terms.
 */
fun clearedOnEntry(shown: List<ShownNotification>, kept: Set<String>): List<ShownNotification> =
    shown.filter { it.channelId !in kept }

/** What opening the conversation [threadId] clears. */
fun clearedByThread(shown: List<ShownNotification>, threadId: String): List<ShownNotification> =
    shown.filter { it.tag == threadTag(threadId) }

/**
 * Whether the screen already shows what a push about [threadId] announces:
 * its conversation is [open]. A push that opens a page in the agent's
 * browser is still shown, since the tap is how the owner gets there.
 */
fun onScreen(threadId: String?, browserUrl: String?, open: String?): Boolean =
    threadId != null && threadId == open && browserUrl.isNullOrBlank()

/**
 * Takes notifications down once the app shows the owner the same news.
 * Entering the app clears all of them but approvals, which stay until they
 * are decided, and Live Updates, which stay while their run runs. One that
 * arrives while the app is open leaves when its conversation opens.
 */
@Singleton
class NotificationShade @Inject constructor(
    @ApplicationContext context: Context,
    private val visibility: AppVisibility,
) {
    private val manager = context.getSystemService(NotificationManager::class.java)

    @Volatile
    private var openThread: String? = null

    /** Call when one of the app's screens comes on display. */
    fun appEntered() {
        cancel(clearedOnEntry(manager.shown(), KEPT_CHANNELS))
    }

    /** Tells which conversation is on screen, or null when none is. */
    fun showThread(threadId: String?) {
        openThread = threadId
        if (threadId != null) cancel(clearedByThread(manager.shown(), threadId))
    }

    /** Whether the owner is looking at what a push about [threadId] announces. */
    fun covers(threadId: String?, browserUrl: String?): Boolean =
        visibility.inFront && onScreen(threadId, browserUrl, openThread)

    private fun cancel(notifications: List<ShownNotification>) {
        notifications.forEach { manager.cancel(it.tag, it.id) }
    }

    private companion object {
        val KEPT_CHANNELS = setOf(ApprovalNotifications.CHANNEL_ID, LiveNotifications.CHANNEL_ID)
    }
}
