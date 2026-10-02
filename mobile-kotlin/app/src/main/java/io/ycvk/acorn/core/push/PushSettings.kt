package io.ycvk.acorn.core.push

/**
 * Firebase client settings baked into the build. Push stays off unless all
 * four are present.
 */
data class PushSettings(
    val projectId: String,
    val appId: String,
    val apiKey: String,
    val senderId: String,
) {
    /** Names of the build settings that are missing, in a stable order. */
    fun missing(): List<String> = buildList {
        if (projectId.isBlank()) add("acorn.firebase.projectId")
        if (appId.isBlank()) add("acorn.firebase.appId")
        if (apiKey.isBlank()) add("acorn.firebase.apiKey")
        if (senderId.isBlank()) add("acorn.firebase.senderId")
    }
}

/** Intent extra carrying the thread a notification belongs to. */
const val EXTRA_THREAD_ID = "thread_id"

/**
 * Returns the thread to open for a notification tap, or null. [extra] reads one
 * intent extra; FCM puts data payload keys there when the system shows the
 * notification, and the app's own notifications use the same key.
 */
fun threadIdFromExtras(extra: (String) -> String?): String? =
    extra(EXTRA_THREAD_ID)?.trim()?.takeIf { it.isNotEmpty() }

/** What the app knows about push notifications for this device. */
sealed interface PushStatus {
    data class NotConfigured(val missing: List<String>) : PushStatus
    data object PermissionDenied : PushStatus
    data object Registering : PushStatus
    data object Registered : PushStatus
    data class Failed(val message: String) : PushStatus
}

/** One-line description of [status] for the settings screen. */
fun pushStatusLabel(status: PushStatus): String = when (status) {
    is PushStatus.NotConfigured -> "not configured in this build (missing ${status.missing.joinToString()})"
    PushStatus.PermissionDenied -> "notifications are turned off for Acorn in system settings"
    PushStatus.Registering -> "registering…"
    PushStatus.Registered -> "on"
    is PushStatus.Failed -> "registration failed: ${status.message}"
}
