package io.ycvk.acorn.core.push

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import com.google.firebase.FirebaseApp
import com.google.firebase.FirebaseOptions
import com.google.firebase.messaging.FirebaseMessaging
import dagger.hilt.android.qualifiers.ApplicationContext
import io.ycvk.acorn.BuildConfig
import io.ycvk.acorn.MainActivity
import io.ycvk.acorn.R
import io.ycvk.acorn.api.apis.DevicesApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.api.models.PushTokenRequest
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.auth.SecureStore
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.tasks.await
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Owns push notifications: initializes Firebase from build settings, sends the
 * FCM token to the paired server, and shows notifications while the app is in
 * the foreground. All network work runs on [Dispatchers.IO].
 */
@Singleton
class PushManager @Inject constructor(
    @ApplicationContext private val context: Context,
    private val secureStore: SecureStore,
) {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val settings = PushSettings(
        projectId = BuildConfig.FIREBASE_PROJECT_ID,
        appId = BuildConfig.FIREBASE_APP_ID,
        apiKey = BuildConfig.FIREBASE_API_KEY,
        senderId = BuildConfig.FIREBASE_SENDER_ID,
    )
    private val _status = MutableStateFlow<PushStatus>(PushStatus.NotConfigured(settings.missing()))
    val status: StateFlow<PushStatus> = _status.asStateFlow()

    /** Called once from Application.onCreate. */
    fun initialize() {
        if (settings.missing().isNotEmpty()) return
        if (FirebaseApp.getApps(context).isEmpty()) {
            FirebaseApp.initializeApp(
                context,
                FirebaseOptions.Builder()
                    .setProjectId(settings.projectId)
                    .setApplicationId(settings.appId)
                    .setApiKey(settings.apiKey)
                    .setGcmSenderId(settings.senderId)
                    .build(),
            )
        }
        val manager = context.getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(
            NotificationChannel(CHANNEL_ID, "Acorn", NotificationManager.IMPORTANCE_HIGH),
        )
    }

    /** Fetches the FCM token and registers it for the paired device. */
    fun register(profile: ConnectionProfile) {
        if (settings.missing().isNotEmpty()) return
        if (!hasPermission()) {
            _status.value = PushStatus.PermissionDenied
            return
        }
        _status.value = PushStatus.Registering
        scope.launch {
            try {
                val token = FirebaseMessaging.getInstance().token.await()
                upload(profile, token)
                _status.value = PushStatus.Registered
            } catch (e: Exception) {
                _status.value = PushStatus.Failed(e.message ?: e.javaClass.simpleName)
            }
        }
    }

    /** FCM rotated the token; register it if this device is paired. */
    fun onNewToken(token: String) {
        val profile = secureStore.getConnection() ?: return
        scope.launch {
            try {
                upload(profile, token)
                _status.value = PushStatus.Registered
            } catch (e: Exception) {
                _status.value = PushStatus.Failed(e.message ?: e.javaClass.simpleName)
            }
        }
    }

    /** Shows a notification received while the app is in the foreground. */
    fun show(title: String, body: String, threadId: String?) {
        if (!hasPermission()) return
        val intent = Intent(context, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP
            threadId?.let { putExtra(EXTRA_THREAD_ID, it) }
        }
        val pending = PendingIntent.getActivity(
            context,
            threadId.hashCode(),
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val notification = NotificationCompat.Builder(context, CHANNEL_ID)
            .setSmallIcon(R.mipmap.ic_launcher)
            .setContentTitle(title)
            .setContentText(body)
            .setStyle(NotificationCompat.BigTextStyle().bigText(body))
            .setAutoCancel(true)
            .setContentIntent(pending)
            .build()
        context.getSystemService(NotificationManager::class.java)
            .notify(System.currentTimeMillis().toInt(), notification)
    }

    private fun upload(profile: ConnectionProfile, token: String) {
        ApiClient.accessToken = profile.accessToken
        DevicesApi(basePath = profile.serverUrl).clientSetPushToken(PushTokenRequest(token = token))
    }

    private fun hasPermission(): Boolean =
        Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU ||
            ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) ==
            PackageManager.PERMISSION_GRANTED

    companion object {
        const val CHANNEL_ID = "acorn_default"
    }
}
