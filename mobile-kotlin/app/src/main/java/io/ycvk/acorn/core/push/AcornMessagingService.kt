package io.ycvk.acorn.core.push

import io.ycvk.acorn.feature.approvals.ApprovalNotifications
import io.ycvk.acorn.feature.approvals.pendingActionPush
import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage
import dagger.hilt.android.AndroidEntryPoint
import javax.inject.Inject

/**
 * Receives FCM callbacks. In the background the system shows notifications
 * itself and puts the data payload into the launch intent; in the foreground
 * the app shows them through [PushManager]. A pending action arrives as data
 * only and is always shown by [ApprovalNotifications], with its decisions.
 */
@AndroidEntryPoint
class AcornMessagingService : FirebaseMessagingService() {
    @Inject lateinit var pushManager: PushManager
    @Inject lateinit var approvals: ApprovalNotifications

    override fun onNewToken(token: String) {
        pushManager.onNewToken(token)
    }

    override fun onMessageReceived(message: RemoteMessage) {
        pendingActionPush(message.data)?.let {
            approvals.show(it)
            return
        }
        val notification = message.notification ?: return
        pushManager.show(
            title = notification.title.orEmpty(),
            body = notification.body.orEmpty(),
            threadId = message.data[EXTRA_THREAD_ID],
            browserUrl = message.data[EXTRA_BROWSER_URL],
        )
    }
}
