package io.ycvk.acorn.core.notifications

import android.content.Context
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import java.io.IOException
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
class NotificationPreferences @Inject constructor(@ApplicationContext context: Context) {
    private val prefs = context.getSharedPreferences("notification_capture", Context.MODE_PRIVATE)
    private val _allowed = MutableStateFlow(prefs.getStringSet("packages", emptySet()).orEmpty().toSet())
    val allowed = _allowed.asStateFlow()

    fun setAllowed(packageName: String, enabled: Boolean) {
        val packages = if (enabled) _allowed.value + packageName else _allowed.value - packageName
        if (!prefs.edit().putStringSet("packages", packages).commit()) throw IOException("Could not save the notification app selection")
        _allowed.value = packages
    }
}

fun shouldCaptureNotification(packageName: String, ownPackage: String, ongoing: Boolean, summary: Boolean, allowed: Set<String>): Boolean =
    packageName != ownPackage && !ongoing && !summary && packageName in allowed

fun String.takeCodePoints(limit: Int): String =
    if (codePointCount(0, length) <= limit) this else substring(0, offsetByCodePoints(0, limit))
