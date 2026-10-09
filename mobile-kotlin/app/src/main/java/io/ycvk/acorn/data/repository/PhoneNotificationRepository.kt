package io.ycvk.acorn.data.repository

import io.ycvk.acorn.api.apis.PhoneNotificationsApi
import io.ycvk.acorn.api.models.PhoneNotificationBatch
import io.ycvk.acorn.api.models.PhoneNotificationInput
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.notifications.CapturedNotification
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.Call
import okhttp3.OkHttpClient
import java.time.Instant
import java.time.ZoneOffset
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
class PhoneNotificationRepository @Inject constructor() {
    private val client = OkHttpClient.Builder().callTimeout(30, TimeUnit.SECONDS).build()
    private val active = AtomicReference<Call?>(null)

    suspend fun send(profile: ConnectionProfile, notifications: List<CapturedNotification>) = withContext(Dispatchers.IO) {
        val api = PhoneNotificationsApi(profile.serverUrl, Call.Factory { request ->
            client.newCall(request).also { active.set(it) }
        }).apply { accessTokenProvider = { profile.accessToken } }
        try {
            api.clientPostPhoneNotifications(PhoneNotificationBatch(notifications.map {
                PhoneNotificationInput(
                    key = it.key, `package` = it.packageName, app = it.app,
                    title = it.title, text = it.text,
                    postedAt = Instant.ofEpochMilli(it.postedAt).atOffset(ZoneOffset.UTC),
                )
            }))
            Unit
        } finally { active.set(null) }
    }

    fun cancel() { active.get()?.cancel() }
}
