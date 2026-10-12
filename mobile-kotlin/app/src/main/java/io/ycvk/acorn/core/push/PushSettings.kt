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

/** Intent extra carrying the pending action a notification announces. */
const val EXTRA_ACTION_ID = "action_id"

/** Intent extra carrying a page a notification opens in the agent's browser. */
const val EXTRA_BROWSER_URL = "browser_url"

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

/** Short state of [status] for the settings row. */
fun pushStatusLabel(status: PushStatus): String = when (status) {
    is PushStatus.NotConfigured -> "这个版本没有配置推送"
    PushStatus.PermissionDenied -> "系统设置里关闭了通知"
    PushStatus.Registering -> "正在注册…"
    PushStatus.Registered -> "已开启"
    is PushStatus.Failed -> "注册失败"
}

/** What the owner needs to know beyond the label, or null. */
fun pushStatusDetail(status: PushStatus): String? = when (status) {
    is PushStatus.NotConfigured -> "缺少 ${status.missing.joinToString()}"
    PushStatus.PermissionDenied -> "在系统设置里允许 Acorn 发送通知后，回到这里重试。"
    is PushStatus.Failed -> if ("MISSING_INSTANCEID_SERVICE" in status.message || "SERVICE_NOT_AVAILABLE" in status.message) {
        "这台设备没有可用的 Google Play 服务，收不到推送。（${status.message}）"
    } else {
        status.message
    }
    PushStatus.Registering, PushStatus.Registered -> null
}
