package io.ycvk.acorn.feature.approvals

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import dagger.hilt.android.AndroidEntryPoint
import dagger.hilt.android.qualifiers.ApplicationContext
import io.ycvk.acorn.MainActivity
import io.ycvk.acorn.R
import io.ycvk.acorn.api.apis.ClientApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.api.infrastructure.ClientException
import io.ycvk.acorn.api.models.DecidePendingActionRequest
import io.ycvk.acorn.api.models.PendingActionDetail
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.auth.SecureStore
import io.ycvk.acorn.core.push.EXTRA_ACTION_ID
import io.ycvk.acorn.core.push.EXTRA_THREAD_ID
import io.ycvk.acorn.core.ui.userMessage
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import javax.inject.Inject
import javax.inject.Singleton

/** What an approval push carries; the app loads the action itself to show current state. */
data class PendingActionPush(
    val actionId: String,
    val threadId: String,
    val title: String,
    val body: String,
)

/** Reads an FCM data payload; null when it does not announce a pending action. */
fun pendingActionPush(data: Map<String, String>): PendingActionPush? {
    if (data["type"] != "pending_action") return null
    val actionId = data["action_id"]?.takeIf { it.isNotBlank() } ?: return null
    return PendingActionPush(actionId, data["thread_id"].orEmpty(), data["title"].orEmpty(), data["body"].orEmpty())
}

/** The decisions a notification can offer in place: a tool approval's accept and decline. */
fun notificationDecisions(detail: PendingActionDetail): List<Pair<String, DecidePendingActionRequest.Decision>> {
    if (detail.kind != PendingActionDetail.Kind.tool_approval) return emptyList()
    val ids = detail.options.map { it.id }
    if (!ids.containsAll(listOf("accept", "decline"))) return emptyList()
    return listOf("decline" to DecidePendingActionRequest.Decision.decline, "accept" to DecidePendingActionRequest.Decision.accept)
}

/**
 * Shows pending actions as notifications with their decisions attached and
 * sends a decision made from one. Each action has one notification, keyed by
 * its id, so deciding anywhere can clear it.
 */
@Singleton
class ApprovalNotifications @Inject constructor(
    @ApplicationContext private val context: Context,
    private val secureStore: SecureStore,
) {
    private val manager = context.getSystemService(NotificationManager::class.java)

    fun createChannel() {
        manager.createNotificationChannel(
            NotificationChannel(CHANNEL_ID, "需要你确认", NotificationManager.IMPORTANCE_HIGH).apply {
                description = "Acorn 停下来等你批准或回答时提醒你"
            },
        )
    }

    /**
     * Loads the action and shows it if it still waits on the owner. Runs on
     * the FCM worker thread. When the server can't be reached the push's own
     * text is shown, so the owner still learns something is waiting.
     */
    fun show(push: PendingActionPush) {
        if (!hasPermission()) return
        val profile = secureStore.getConnection() ?: return
        val detail = try {
            api(profile).clientGetPendingAction(push.actionId)
        } catch (e: ClientException) {
            // Already decided, or gone with its thread: nothing waits any more.
            if (e.statusCode == 404 || e.statusCode == 409) return
            notify(push.actionId, push.threadId, "Acorn 在等你确认", push.title, kind = null, decisions = emptyList())
            return
        } catch (_: Exception) {
            notify(push.actionId, push.threadId, "Acorn 在等你确认", push.title, kind = null, decisions = emptyList())
            return
        }
        if (detail.status != PendingActionDetail.Status.pending) return
        notify(
            actionId = detail.actionId,
            threadId = detail.threadId,
            title = approvalTitle(detail.title, detail.toolName),
            body = approvalBody(detail.body, detail.toolName, detail.toolSummary) ?: kindLabel(detail.kind.value),
            kind = kindLabel(detail.kind.value),
            decisions = notificationDecisions(detail),
        )
    }

    fun cancel(actionId: String) {
        manager.cancel(notificationId(actionId))
    }

    /** Sends a decision chosen on a notification and replaces the notification with the outcome. */
    fun decide(actionId: String, threadId: String, optionId: String, decision: DecidePendingActionRequest.Decision) {
        val profile = secureStore.getConnection() ?: return
        try {
            api(profile).clientDecidePendingAction(actionId, DecidePendingActionRequest(decision = decision, selectedOptionId = optionId))
            val done = if (decision == DecidePendingActionRequest.Decision.accept) "已批准，Acorn 会继续" else "已拒绝"
            manager.notify(
                notificationId(actionId),
                base(threadId, actionId).setContentTitle(done).setTimeoutAfter(OUTCOME_TIMEOUT_MS).setSilent(true).build(),
            )
        } catch (e: ClientException) {
            if (e.statusCode == 409) {
                manager.notify(notificationId(actionId), base(threadId, actionId).setContentTitle("这个操作已经处理过了").setTimeoutAfter(OUTCOME_TIMEOUT_MS).setSilent(true).build())
                return
            }
            failed(actionId, threadId, e)
        } catch (e: Exception) {
            failed(actionId, threadId, e)
        }
    }

    private fun failed(actionId: String, threadId: String, e: Exception) {
        manager.notify(
            notificationId(actionId),
            base(threadId, actionId)
                .setContentTitle("没能提交你的决定")
                .setContentText(userMessage(e))
                .setStyle(NotificationCompat.BigTextStyle().bigText("${userMessage(e)}。点开在 App 里处理。"))
                .build(),
        )
    }

    private fun notify(
        actionId: String,
        threadId: String,
        title: String,
        body: String,
        kind: String?,
        decisions: List<Pair<String, DecidePendingActionRequest.Decision>>,
    ) {
        val builder = base(threadId, actionId)
            .setContentTitle(title)
            .setContentText(body)
            .setStyle(NotificationCompat.BigTextStyle().bigText(body))
            .setCategory(NotificationCompat.CATEGORY_REMINDER)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
        kind?.let { builder.setSubText(it) }
        decisions.forEach { (optionId, decision) ->
            val label = if (decision == DecidePendingActionRequest.Decision.accept) "批准" else "拒绝"
            builder.addAction(0, label, decisionIntent(actionId, threadId, optionId, decision))
        }
        manager.notify(notificationId(actionId), builder.build())
    }

    private fun base(threadId: String, actionId: String): NotificationCompat.Builder =
        NotificationCompat.Builder(context, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_acorn)
            .setColor(ContextCompat.getColor(context, R.color.acorn))
            .setAutoCancel(true)
            .setContentIntent(openIntent(threadId, actionId))

    private fun openIntent(threadId: String, actionId: String): PendingIntent {
        val intent = Intent(context, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP
            putExtra(EXTRA_THREAD_ID, threadId)
            putExtra(EXTRA_ACTION_ID, actionId)
        }
        return PendingIntent.getActivity(context, notificationId(actionId), intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
    }

    private fun decisionIntent(actionId: String, threadId: String, optionId: String, decision: DecidePendingActionRequest.Decision): PendingIntent {
        val intent = Intent(context, ApprovalDecisionReceiver::class.java).apply {
            putExtra(EXTRA_ACTION_ID, actionId)
            putExtra(EXTRA_THREAD_ID, threadId)
            putExtra(EXTRA_OPTION_ID, optionId)
            putExtra(EXTRA_DECISION, decision.value)
        }
        // One request code per action and decision keeps the two buttons' intents apart.
        return PendingIntent.getBroadcast(context, (actionId + decision.value).hashCode(), intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
    }

    private fun api(profile: ConnectionProfile): ClientApi {
        ApiClient.accessToken = profile.accessToken
        return ClientApi(basePath = profile.serverUrl)
    }

    private fun hasPermission(): Boolean =
        Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU ||
            ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED

    companion object {
        const val CHANNEL_ID = "acorn_approvals"
        const val EXTRA_OPTION_ID = "option_id"
        const val EXTRA_DECISION = "decision"
        private const val OUTCOME_TIMEOUT_MS = 4_000L

        fun notificationId(actionId: String): Int = actionId.hashCode()
    }
}

/** Receives 批准 / 拒绝 from a notification and sends the decision without opening the app. */
@AndroidEntryPoint
class ApprovalDecisionReceiver : BroadcastReceiver() {
    @Inject lateinit var notifications: ApprovalNotifications

    override fun onReceive(context: Context, intent: Intent) {
        val actionId = intent.getStringExtra(EXTRA_ACTION_ID) ?: return
        val threadId = intent.getStringExtra(EXTRA_THREAD_ID).orEmpty()
        val optionId = intent.getStringExtra(ApprovalNotifications.EXTRA_OPTION_ID) ?: return
        val decision = DecidePendingActionRequest.Decision.entries.firstOrNull { it.value == intent.getStringExtra(ApprovalNotifications.EXTRA_DECISION) } ?: return
        val pending = goAsync()
        scope.launch {
            try {
                notifications.decide(actionId, threadId, optionId, decision)
            } finally {
                pending.finish()
            }
        }
    }

    private companion object {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    }
}
