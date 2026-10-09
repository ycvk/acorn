package io.ycvk.acorn.core.notifications

import com.squareup.moshi.Moshi
import com.squareup.moshi.kotlin.reflect.KotlinJsonAdapterFactory
import io.ycvk.acorn.core.auth.ConnectionProfile
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import java.util.UUID

data class CapturedNotification(
    val key: String,
    val packageName: String,
    val app: String,
    val title: String,
    val text: String,
    val postedAt: Long,
)

data class QueuedNotification(val id: String, val notification: CapturedNotification)

data class NotificationQueueState(
    val owner: String? = null,
    val items: List<QueuedNotification> = emptyList(),
    val dropped: Int = 0,
)

fun ConnectionProfile.notificationOwner(): String = "${serverUrl.trimEnd('/')}\n$deviceId"

/** A private, atomic queue. Every operation preserves ownership and batch IDs. */
class NotificationQueue(private val file: File) {
    private val adapter = Moshi.Builder().addLast(KotlinJsonAdapterFactory()).build()
        .adapter(NotificationQueueState::class.java)
    private var loaded: NotificationQueueState? = null

    @Synchronized
    fun snapshot(): NotificationQueueState {
        val state = state()
        return state.copy(items = state.items.toList())
    }

    @Synchronized
    fun selectOwner(owner: String?) {
        if (state().owner != owner) save(NotificationQueueState(owner = owner))
    }

    @Synchronized
    fun enqueue(owner: String, item: CapturedNotification) {
        val state = state()
        if (state.owner != owner) return
        if (state.items.any { it.notification.key == item.key && it.notification.postedAt == item.postedAt }) return
        val items = state.items + QueuedNotification(UUID.randomUUID().toString(), item)
        val overflow = (items.size - MAX_ITEMS).coerceAtLeast(0)
        save(state.copy(items = items.takeLast(MAX_ITEMS), dropped = state.dropped + overflow))
    }

    @Synchronized
    fun batch(owner: String, allowed: Set<String>): List<QueuedNotification> {
        val state = state()
        if (state.owner != owner) return emptyList()
        val retained = state.items.filter { it.notification.packageName in allowed }
        if (retained.size != state.items.size) save(state.copy(items = retained))
        return retained.take(100)
    }

    @Synchronized
    fun acknowledge(owner: String, ids: Set<String>) {
        val state = state()
        if (state.owner == owner) save(state.copy(items = state.items.filterNot { it.id in ids }))
    }

    private fun state(): NotificationQueueState {
        loaded?.let { return it }
        val result = if (file.exists()) {
            adapter.fromJson(file.readText()) ?: throw IOException("Notification queue is empty or invalid")
        } else NotificationQueueState()
        loaded = result
        return result
    }

    private fun save(state: NotificationQueueState) {
        val temporary = File(file.parentFile, "${file.name}.new")
        try {
            FileOutputStream(temporary).use { stream ->
                stream.write(adapter.toJson(state).toByteArray(Charsets.UTF_8))
                stream.fd.sync()
            }
            Files.move(temporary.toPath(), file.toPath(), StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING)
            loaded = state
        } finally {
            Files.deleteIfExists(temporary.toPath())
        }
    }

    companion object { const val MAX_ITEMS = 500 }
}
