package io.ycvk.acorn.core.notifications

import android.content.Context
import androidx.core.app.NotificationManagerCompat
import dagger.hilt.android.qualifiers.ApplicationContext
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.data.repository.PhoneNotificationRepository
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import javax.inject.Inject
import javax.inject.Singleton

data class NotificationUploadStatus(val pending: Int = 0, val uploading: Boolean = false, val error: String? = null, val dropped: Int = 0)

@Singleton
class NotificationUploader @Inject constructor(
    @ApplicationContext private val context: Context,
    private val auth: AuthController,
    private val queue: NotificationQueue,
    private val preferences: NotificationPreferences,
    private val repository: PhoneNotificationRepository,
    private val delivery: NotificationDelivery,
) {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO.limitedParallelism(1))
    private val _status = MutableStateFlow(NotificationUploadStatus())
    val status = _status.asStateFlow()
    private val batcher = NotificationBatcher(scope) {
        perform {
            publish(uploading = true)
            delivery.flush()
            publish()
        }
    }

    fun initialize() {
        scope.launch {
            auth.authState.collectLatest { state ->
                if (state is AuthState.Loading) return@collectLatest
                batcher.cancel()
                repository.cancel()
                perform {
                    val owner = (state as? AuthState.Connected)?.profile?.notificationOwner()
                    queue.selectOwner(owner)
                    publish()
                    if (owner != null) batcher.request(immediate = true)
                }
            }
        }
    }

    fun capture(notification: CapturedNotification, ongoing: Boolean, summary: Boolean) {
        scope.launch {
            auth.authState.filter { it !is AuthState.Loading }.first()
            val connection = (auth.authState.value as? AuthState.Connected)?.profile ?: return@launch
            if (!hasAccess() || !shouldCaptureNotification(notification.packageName, context.packageName, ongoing, summary, preferences.allowed.value)) return@launch
            perform {
                val owner = connection.notificationOwner()
                queue.selectOwner(owner)
                queue.enqueue(owner, notification)
                publish()
                batcher.request()
            }
        }
    }

    fun retry() {
        scope.launch {
            perform {
                publish()
                if (hasAccess()) batcher.request(immediate = true)
            }
        }
    }

    fun setAllowed(packageName: String, enabled: Boolean) {
        scope.launch {
            perform {
                preferences.setAllowed(packageName, enabled)
                (auth.authState.value as? AuthState.Connected)?.profile?.let {
                    queue.batch(it.notificationOwner(), preferences.allowed.value)
                }
                publish()
            }
        }
    }

    fun hasAccess(): Boolean = NotificationManagerCompat.getEnabledListenerPackages(context).contains(context.packageName)

    private fun publish(uploading: Boolean = false) {
        val queue = queue.snapshot()
        _status.value = NotificationUploadStatus(queue.items.size, uploading, dropped = queue.dropped)
    }

    private suspend fun perform(action: suspend () -> Unit) {
        try { action() }
        catch (e: CancellationException) { throw e }
        catch (e: Exception) {
            _status.value = _status.value.copy(uploading = false, error = e.message ?: e.javaClass.simpleName)
        }
    }
}
