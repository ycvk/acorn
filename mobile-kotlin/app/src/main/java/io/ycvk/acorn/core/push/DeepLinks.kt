package io.ycvk.acorn.core.push

import android.content.Context
import android.content.Intent
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import javax.inject.Inject
import javax.inject.Singleton

/**
 * A thread to open, and the pending action in it to bring up, if any. With
 * [browserUrl] the agent's browser opens that page over the thread, so going
 * back from it lands in the conversation.
 */
data class ThreadLink(val threadId: String, val actionId: String? = null, val browserUrl: String? = null)

/**
 * The activity alias a notification tap arrives through. It is not exported,
 * so only this app starts it: the notifications it builds itself, and the
 * ones FCM shows for it, whose click_action is [ACTION_OPEN_NOTIFICATION].
 */
const val NOTIFICATION_TAP = "io.ycvk.acorn.NotificationTap"

const val ACTION_OPEN_NOTIFICATION = "io.ycvk.acorn.OPEN_NOTIFICATION"

/**
 * The thread a start of the app opens, from the start's [component] and its
 * extras read by [extra]. Any app can start the launcher with extras, so only
 * a start through [NOTIFICATION_TAP] carries a thread, a pending action or a
 * browser page.
 */
fun notificationLink(component: String?, extra: (String) -> String?): ThreadLink? {
    if (component != NOTIFICATION_TAP) return null
    val threadId = extra(EXTRA_THREAD_ID)?.trim()?.takeIf { it.isNotEmpty() } ?: return null
    return ThreadLink(
        threadId = threadId,
        actionId = extra(EXTRA_ACTION_ID)?.takeIf { it.isNotBlank() },
        browserUrl = extra(EXTRA_BROWSER_URL)?.takeIf { it.isNotBlank() },
    )
}

/** What a tap on a notification the app builds opens: [threadId], and in it [actionId] or [browserUrl]. */
fun notificationTap(context: Context, threadId: String?, actionId: String? = null, browserUrl: String? = null): Intent =
    Intent(ACTION_OPEN_NOTIFICATION).setClassName(context, NOTIFICATION_TAP).apply {
        flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP
        threadId?.let { putExtra(EXTRA_THREAD_ID, it) }
        actionId?.let { putExtra(EXTRA_ACTION_ID, it) }
        browserUrl?.let { putExtra(EXTRA_BROWSER_URL, it) }
    }

/**
 * Hands a thread to open from a notification tap (MainActivity) to the shell,
 * which consumes it once the device is connected.
 */
@Singleton
class DeepLinks @Inject constructor() {
    private val _threadToOpen = MutableStateFlow<ThreadLink?>(null)
    val threadToOpen: StateFlow<ThreadLink?> = _threadToOpen.asStateFlow()

    fun openThread(link: ThreadLink) {
        _threadToOpen.value = link
    }

    fun consumed() {
        _threadToOpen.value = null
    }
}
