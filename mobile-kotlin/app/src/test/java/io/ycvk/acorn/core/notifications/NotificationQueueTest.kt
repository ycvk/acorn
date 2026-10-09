package io.ycvk.acorn.core.notifications

import com.squareup.moshi.Moshi
import com.squareup.moshi.kotlin.reflect.KotlinJsonAdapterFactory
import io.ycvk.acorn.core.auth.ConnectionProfile
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.IOException

@OptIn(ExperimentalCoroutinesApi::class)
class NotificationQueueTest {
    @get:Rule val folder = TemporaryFolder()
    private val connection = ConnectionProfile("https://acorn.example", "phone", "token")
    private val owner = connection.notificationOwner()
    private fun item(key: String, pkg: String = "bank") = CapturedNotification(key, pkg, "Bank", "Transfer", "100", 1234)

    @Test fun persistsDeduplicatesAndAcknowledgesOnlyTheSentBatch() {
        val file = folder.newFile()
        file.delete()
        val queue = NotificationQueue(file)
        queue.selectOwner(owner)
        queue.enqueue(owner, item("one"))
        queue.enqueue(owner, item("one"))
        val sent = queue.batch(owner, setOf("bank"))
        queue.enqueue(owner, item("two"))
        queue.acknowledge(owner, sent.map { it.id }.toSet())
        val restored = NotificationQueue(file).snapshot()
        assertEquals(listOf("two"), restored.items.map { it.notification.key })
    }

    @Test fun ownerChangesAndUnselectedAppsRemovePendingContent() {
        val queue = NotificationQueue(folder.root.resolve("queue.json"))
        queue.selectOwner(owner)
        queue.enqueue(owner, item("bank"))
        queue.enqueue(owner, item("mail", "mail"))
        assertEquals(listOf("mail"), queue.batch(owner, setOf("mail")).map { it.notification.key })
        queue.selectOwner("other device")
        queue.enqueue(owner, item("stale callback"))
        assertTrue(queue.snapshot().items.isEmpty())
        queue.selectOwner(null)
        assertNull(queue.snapshot().owner)
    }

    @Test fun fullQueueEvictsTheOldestAndReportsIt() {
        val file = folder.root.resolve("queue.json")
        val state = NotificationQueueState(owner, (0 until 500).map { QueuedNotification("id$it", item("$it")) })
        val adapter = Moshi.Builder().addLast(KotlinJsonAdapterFactory()).build().adapter(NotificationQueueState::class.java)
        file.writeText(adapter.toJson(state))
        val queue = NotificationQueue(file)
        queue.enqueue(owner, item("500"))
        assertEquals(500, queue.snapshot().items.size)
        assertEquals("1", queue.snapshot().items.first().notification.key)
        assertEquals(1, queue.snapshot().dropped)
    }

    @Test fun failedUploadKeepsTheBatchAndRetryDrainsNewArrivals() = runTest {
        val queue = NotificationQueue(folder.root.resolve("queue.json"))
        queue.selectOwner(owner)
        queue.enqueue(owner, item("one"))
        var fail = true
        val sent = mutableListOf<String>()
        val delivery = NotificationDelivery(queue, { connection }, { setOf("bank") }, { true }) { _, batch ->
            if (fail) throw IOException("offline")
            sent.addAll(batch.map { it.key })
            if (batch.first().key == "one") queue.enqueue(owner, item("two"))
        }
        try { delivery.flush(); fail("expected failure") } catch (_: IOException) { }
        assertEquals(1, queue.snapshot().items.size)
        fail = false
        delivery.flush()
        assertEquals(listOf("one", "two"), sent)
        assertTrue(queue.snapshot().items.isEmpty())
    }

    @Test fun uploadNeverMovesAnOldBatchToANewConnection() = runTest {
        val queue = NotificationQueue(folder.root.resolve("queue.json"))
        queue.selectOwner(owner)
        queue.enqueue(owner, item("one"))
        var current: ConnectionProfile? = connection
        val sentTo = mutableListOf<ConnectionProfile>()
        val delivery = NotificationDelivery(queue, { current }, { setOf("bank") }, { true }) { profile, _ ->
            sentTo += profile
            current = ConnectionProfile("https://other.example", "other", "other-token")
            queue.selectOwner(current!!.notificationOwner())
        }
        delivery.flush()
        assertEquals(listOf(connection), sentTo)
        assertTrue(queue.snapshot().items.isEmpty())
    }

    @Test fun continuousArrivalsKeepTheFirstUploadDeadline() = runTest {
        var calls = 0
        val batcher = NotificationBatcher(backgroundScope) { calls++ }
        batcher.request()
        runCurrent()
        repeat(5) { advanceTimeBy(10_000); batcher.request(); runCurrent() }
        assertEquals(0, calls)
        advanceTimeBy(10_000)
        runCurrent()
        assertEquals(1, calls)
    }

    @Test fun arrivalAsAnUploadFinishesSchedulesAnotherPass() = runTest {
        var calls = 0
        lateinit var batcher: NotificationBatcher
        batcher = NotificationBatcher(backgroundScope) {
            calls++
            if (calls == 1) batcher.request()
        }
        batcher.request(immediate = true)
        runCurrent()
        assertEquals(2, calls)
    }

    @Test fun filterAndUnicodeLimitsMatchServerContract() {
        assertTrue(shouldCaptureNotification("bank", "acorn", false, false, setOf("bank")))
        assertFalse(shouldCaptureNotification("acorn", "acorn", false, false, setOf("acorn")))
        assertFalse(shouldCaptureNotification("bank", "acorn", true, false, setOf("bank")))
        assertFalse(shouldCaptureNotification("bank", "acorn", false, true, setOf("bank")))
        assertFalse(shouldCaptureNotification("bank", "acorn", false, false, emptySet()))
        assertEquals("😀😀", "😀😀😀".takeCodePoints(2))
    }
}
