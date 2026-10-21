package io.ycvk.acorn.feature.live

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import dagger.hilt.android.qualifiers.ApplicationContext
import io.ycvk.acorn.MainActivity
import io.ycvk.acorn.R
import io.ycvk.acorn.core.push.EXTRA_THREAD_ID
import io.ycvk.acorn.core.push.PushManager
import io.ycvk.acorn.core.sse.RunStatus
import io.ycvk.acorn.core.ui.plainSnippet
import javax.inject.Inject
import javax.inject.Singleton

/** One Live Update: a message being sent, or a run of this phone that is running. */
data class LiveItem(val id: Int, val title: String, val status: LiveStatus, val runId: String?, val threadId: String?)

/** The Live Updates to show: messages being sent first, then the runs started here that are running. */
fun liveItems(runs: Map<String, FollowedRun>, sending: List<PendingSend>): List<LiveItem> =
    sending.map { LiveItem(sendNotificationId(it.key), it.title, Sending, runId = null, threadId = null) } +
        runs.values.filter { it.live && it.running }.map { run ->
            LiveItem(liveNotificationId(run.runId), run.title, if (run.stopping) Stopping else liveStatus(run.state), run.runId, run.threadId)
        }

fun sendNotificationId(key: String): Int = "send:$key".hashCode()

fun liveNotificationId(runId: String): Int = "live:$runId".hashCode()

fun endNotificationId(runId: String): Int = "done:$runId".hashCode()

/** What the notification left when a run ends while the app is away: the reply's opening, or why it failed. */
data class Ending(val title: String, val text: String)

/** The ending of a run that completed or failed; null for one that waits on the owner or that the owner stopped. */
fun ending(run: FollowedRun): Ending? = when (run.state.runStatus) {
    RunStatus.Completed -> Ending(run.title, plainSnippet(run.state.assistantText).take(ENDING_CHARS).ifBlank { "回复好了" })
    RunStatus.Failed -> if (run.stopping) null else Ending("没能完成：${run.title}", run.state.failure ?: "服务器没有给出原因")
    else -> null
}

private const val ENDING_CHARS = 240

/**
 * Builds the Live Updates of runs started from this phone, and leaves a
 * notification when one ends while the app is away.
 */
@Singleton
class LiveNotifications @Inject constructor(
    @ApplicationContext private val context: Context,
    private val visibility: AppVisibility,
) : RunEndings {
    private val manager = context.getSystemService(NotificationManager::class.java)

    fun createChannel() {
        manager.createNotificationChannel(
            NotificationChannel(CHANNEL_ID, "正在处理", NotificationManager.IMPORTANCE_LOW).apply {
                description = "你从这台手机发出的请求，Acorn 处理时显示它做到哪一步"
            },
        )
    }

    /** A Live Update: ongoing, asking to be promoted to a status bar chip, with a stop for a run. */
    fun live(item: LiveItem): Notification {
        val builder = NotificationCompat.Builder(context, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_acorn)
            .setColor(ContextCompat.getColor(context, R.color.acorn))
            .setContentTitle(item.title)
            .setContentText(item.status.text)
            .setShortCriticalText(item.status.chip)
            .setStyle(NotificationCompat.ProgressStyle().setProgressIndeterminate(true))
            .setOngoing(true)
            .setRequestPromotedOngoing(true)
            .setOnlyAlertOnce(true)
            .setSilent(true)
            .setCategory(NotificationCompat.CATEGORY_PROGRESS)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
        item.threadId?.let { builder.setContentIntent(openThread(it)) }
        if (item.status != Stopping) item.runId?.let { builder.addAction(0, "停止", stopRun(it)) }
        return builder.build()
    }

    /** Leaves the reply's opening, or why the run failed, unless the owner is looking at the app. */
    override fun ended(run: FollowedRun) {
        if (visibility.inFront || !hasPermission()) return
        val ending = ending(run) ?: return
        val notification = NotificationCompat.Builder(context, PushManager.CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_acorn)
            .setColor(ContextCompat.getColor(context, R.color.acorn))
            .setContentTitle(ending.title)
            .setContentText(ending.text)
            .setStyle(NotificationCompat.BigTextStyle().bigText(ending.text))
            .setAutoCancel(true)
            .setContentIntent(openThread(run.threadId))
            .build()
        manager.notify(endNotificationId(run.runId), notification)
    }

    /** Says that a stop asked from the notification did not reach the server. */
    fun stopFailed(title: String, reason: String) {
        if (!hasPermission()) return
        val notification = NotificationCompat.Builder(context, PushManager.CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_acorn)
            .setContentTitle("没能停下：$title")
            .setContentText(reason)
            .setAutoCancel(true)
            .build()
        manager.notify("stop:$title".hashCode(), notification)
    }

    private fun openThread(threadId: String): PendingIntent {
        val intent = Intent(context, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP
            putExtra(EXTRA_THREAD_ID, threadId)
        }
        return PendingIntent.getActivity(context, "open:$threadId".hashCode(), intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
    }

    private fun stopRun(runId: String): PendingIntent {
        val intent = Intent(context, RunFollowService::class.java).apply {
            action = RunFollowService.ACTION_STOP
            putExtra(RunFollowService.EXTRA_RUN_ID, runId)
        }
        return PendingIntent.getService(context, "stop:$runId".hashCode(), intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
    }

    private fun hasPermission(): Boolean =
        Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU ||
            ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED

    companion object {
        const val CHANNEL_ID = "acorn_live"
    }
}
