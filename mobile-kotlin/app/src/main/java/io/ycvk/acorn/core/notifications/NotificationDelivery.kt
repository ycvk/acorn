package io.ycvk.acorn.core.notifications

import io.ycvk.acorn.core.auth.ConnectionProfile
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

/** Sends a stable batch and removes only its acknowledged IDs. */
class NotificationDelivery(
    private val queue: NotificationQueue,
    private val profile: () -> ConnectionProfile?,
    private val allowed: () -> Set<String>,
    private val hasAccess: () -> Boolean,
    private val send: suspend (ConnectionProfile, List<CapturedNotification>) -> Unit,
) {
    private val lock = Mutex()

    suspend fun flush() = lock.withLock {
        while (hasAccess()) {
            val connection = profile() ?: return@withLock
            val owner = connection.notificationOwner()
            val batch = queue.batch(owner, allowed())
            if (batch.isEmpty()) return@withLock
            if (profile() != connection) return@withLock
            send(connection, batch.map { it.notification })
            queue.acknowledge(owner, batch.map { it.id }.toSet())
        }
    }
}

/** One timer from the first request; arrivals during the delay keep that deadline. */
class NotificationBatcher(
    private val scope: CoroutineScope,
    private val delayMillis: Long = 60_000,
    private val upload: suspend () -> Unit,
) {
    private var job: Job? = null
    private var marker: Any? = null
    private var uploading = false
    private var requestedDuringUpload = false

    @Synchronized
    fun request(immediate: Boolean = false) {
        if (job?.isActive == true) {
            if (uploading) requestedDuringUpload = true
            return
        }
        val run = Any()
        marker = run
        val next = scope.launch(start = CoroutineStart.LAZY) {
            try {
                if (!immediate) delay(delayMillis)
                do {
                    synchronized(this@NotificationBatcher) {
                        uploading = true
                        requestedDuringUpload = false
                    }
                    upload()
                    val again = synchronized(this@NotificationBatcher) {
                        if (marker !== run) false
                        else if (requestedDuringUpload) true
                        else {
                            uploading = false
                            job = null
                            false
                        }
                    }
                } while (again)
            } finally {
                synchronized(this@NotificationBatcher) {
                    if (marker === run) {
                        uploading = false
                        job = null
                    }
                }
            }
        }
        job = next
        next.start()
    }

    @Synchronized
    fun cancel() {
        marker = null
        job?.cancel()
        job = null
        uploading = false
        requestedDuringUpload = false
    }
}
