package io.ycvk.acorn.core.push

import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage
import dagger.hilt.android.AndroidEntryPoint
import javax.inject.Inject

/**
 * Receives FCM callbacks. In the background the system shows notifications
 * itself and puts the data payload into the launch intent; in the foreground
 * the app shows them through [PushManager].
 */
@AndroidEntryPoint
class AcornMessagingService : FirebaseMessagingService() {
    @Inject lateinit var pushManager: PushManager

    override fun onNewToken(token: String) {
        pushManager.onNewToken(token)
    }

    override fun onMessageReceived(message: RemoteMessage) {
        val notification = message.notification ?: return
        pushManager.show(
            title = notification.title.orEmpty(),
            body = notification.body.orEmpty(),
            threadId = message.data[EXTRA_THREAD_ID],
        )
    }
}
