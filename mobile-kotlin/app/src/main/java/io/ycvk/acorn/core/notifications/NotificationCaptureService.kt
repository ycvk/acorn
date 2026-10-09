package io.ycvk.acorn.core.notifications

import android.app.Notification
import android.content.pm.PackageManager
import android.service.notification.NotificationListenerService
import android.service.notification.StatusBarNotification
import android.util.Log
import dagger.hilt.android.AndroidEntryPoint
import java.security.MessageDigest
import javax.inject.Inject

@AndroidEntryPoint
class NotificationCaptureService : NotificationListenerService() {
    @Inject lateinit var uploader: NotificationUploader

    override fun onListenerConnected() {
        super.onListenerConnected()
        uploader.retry()
    }

    override fun onNotificationPosted(sbn: StatusBarNotification) {
        val notification = sbn.notification
        val app = try {
            packageManager.getApplicationLabel(packageManager.getApplicationInfo(sbn.packageName, 0)).toString()
        } catch (e: PackageManager.NameNotFoundException) {
            Log.e("AcornNotifications", "Could not identify notification app", e)
            return
        }
        val extras = notification.extras
        val text = extras.getCharSequence(Notification.EXTRA_BIG_TEXT)
            ?: extras.getCharSequence(Notification.EXTRA_TEXT)
        val key = MessageDigest.getInstance("SHA-256").digest(sbn.key.toByteArray(Charsets.UTF_8))
            .joinToString("") { "%02x".format(it) }
        uploader.capture(
            CapturedNotification(
                key = key, packageName = sbn.packageName, app = app.takeCodePoints(256),
                title = extras.getCharSequence(Notification.EXTRA_TITLE)?.toString().orEmpty().takeCodePoints(200),
                text = text?.toString().orEmpty().takeCodePoints(2000), postedAt = sbn.postTime,
            ),
            ongoing = sbn.isOngoing,
            summary = notification.flags and Notification.FLAG_GROUP_SUMMARY != 0,
        )
    }
}
