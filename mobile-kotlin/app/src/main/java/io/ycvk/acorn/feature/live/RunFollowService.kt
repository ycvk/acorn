package io.ycvk.acorn.feature.live

import android.app.ForegroundServiceStartNotAllowedException
import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import android.util.Log
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import dagger.hilt.android.AndroidEntryPoint
import dagger.hilt.android.qualifiers.ApplicationContext
import io.ycvk.acorn.api.apis.ClientApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.ui.userMessage
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import javax.inject.Inject
import javax.inject.Singleton

/** Starts the Live Update service when it is not running. */
@Singleton
class ServiceLiveUpdates @Inject constructor(@ApplicationContext private val context: Context) : LiveUpdates {
    /** Set while the service is up, so a later request does not start it again from the background. */
    @Volatile
    var shown = false

    override fun ensureShown() {
        if (shown) return
        try {
            ContextCompat.startForegroundService(context, Intent(context, RunFollowService::class.java))
        } catch (e: IllegalStateException) {
            // The system refuses a foreground service to an app in the background; the run goes on without its Live Update.
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S && e is ForegroundServiceStartNotAllowedException) {
                Log.w("LiveUpdates", "Live Update not shown", e)
                return
            }
            throw e
        }
    }
}

/**
 * Keeps the app following the runs started from this phone while they run,
 * and shows each as a Live Update. It stops once nothing is being sent or
 * running.
 */
@AndroidEntryPoint
class RunFollowService : Service() {
    @Inject lateinit var follower: RunFollower
    @Inject lateinit var notifications: LiveNotifications
    @Inject lateinit var liveUpdates: ServiceLiveUpdates
    @Inject lateinit var authController: AuthController

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val manager by lazy { getSystemService(NotificationManager::class.java) }
    private val posted = mutableSetOf<Int>()
    private var inForeground = false
    private var lastStartId = 0

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        liveUpdates.shown = true
        scope.launch {
            combine(follower.runs, follower.sending, ::liveItems).collect(::render)
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        lastStartId = startId
        liveUpdates.shown = true
        if (intent?.action == ACTION_STOP) intent.getStringExtra(EXTRA_RUN_ID)?.let(::stop)
        val items = liveItems(follower.runs.value, follower.sending.value)
        // A service started as a foreground service must become one, even if what it was started for already ended.
        if (items.isEmpty() && !inForeground) {
            goForeground(PLACEHOLDER_ID, notifications.live(LiveItem(PLACEHOLDER_ID, "Acorn", Sending, null, null)))
        }
        render(items)
        return START_NOT_STICKY
    }

    private fun render(items: List<LiveItem>) {
        if (items.isEmpty()) {
            liveUpdates.shown = false
            ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
            inForeground = false
            posted.forEach(manager::cancel)
            posted.clear()
            stopSelfResult(lastStartId)
            return
        }
        val first = items.first()
        goForeground(first.id, notifications.live(first))
        items.drop(1).forEach { manager.notify(it.id, notifications.live(it)) }
        val shown = items.map { it.id }.toSet()
        (posted - shown).forEach(manager::cancel)
        posted.clear()
        posted += shown
    }

    private fun goForeground(id: Int, notification: android.app.Notification) {
        ServiceCompat.startForeground(this, id, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
        inForeground = true
    }

    /** Asks the server to stop a run, as the chat screen's stop does; the run's events end its Live Update. */
    private fun stop(runId: String) {
        val profile = (authController.authState.value as? AuthState.Connected)?.profile ?: return
        val title = follower.runs.value[runId]?.title ?: "Acorn"
        follower.stopping(runId, true)
        scope.launch {
            try {
                withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    ClientApi(basePath = profile.serverUrl).clientInterruptRun(runId)
                }
            } catch (e: Exception) {
                follower.stopping(runId, false)
                notifications.stopFailed(title, userMessage(e))
            }
        }
    }

    override fun onDestroy() {
        scope.cancel()
        liveUpdates.shown = false
        super.onDestroy()
    }

    companion object {
        const val ACTION_STOP = "io.ycvk.acorn.live.STOP"
        const val EXTRA_RUN_ID = "run_id"
        private val PLACEHOLDER_ID = "live:placeholder".hashCode()
    }
}
