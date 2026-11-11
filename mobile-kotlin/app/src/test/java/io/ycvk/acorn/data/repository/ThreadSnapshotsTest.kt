package io.ycvk.acorn.data.repository

import io.ycvk.acorn.api.models.Message
import io.ycvk.acorn.api.models.MessageContent
import io.ycvk.acorn.core.auth.ConnectionProfile
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File
import java.time.OffsetDateTime

class ThreadSnapshotsTest {
    @get:Rule val folder = TemporaryFolder()
    private val phone = ConnectionProfile("https://acorn.example", "phone", "token")
    private val dir: File get() = File(folder.root, "threads")

    private fun message(id: String, text: String) = Message(
        id = id,
        threadId = "thread_1",
        role = Message.Role.assistant,
        content = MessageContent(type = MessageContent.Type.text, text = text),
        createdAt = OffsetDateTime.parse("2026-11-10T08:00:00Z"),
    )

    @Test
    fun `a conversation is read back as the server gave it, by another start of the app too`() {
        val messages = listOf(message("m1", "周五有三班直飞"), message("m2", "最便宜的是 9:05 那班"))
        ThreadSnapshots(dir).write(phone, "thread_1", "订机票", messages)

        val later = ThreadSnapshots(dir)
        assertNull(later.remembered(phone, "thread_1"))
        val copy = later.read(phone, "thread_1")!!
        assertEquals("订机票", copy.title)
        assertEquals(messages, copy.messages)
        // Read once, it is at hand without the disk.
        assertEquals(copy, later.remembered(phone, "thread_1"))
        assertNull(later.read(phone, "thread_2"))
    }

    @Test
    fun `the server's next answer replaces the copy`() {
        val snapshots = ThreadSnapshots(dir)
        snapshots.write(phone, "thread_1", null, listOf(message("m1", "旧")))
        snapshots.write(phone, "thread_1", "标题", listOf(message("m1", "旧"), message("m2", "新")))

        val copy = ThreadSnapshots(dir).read(phone, "thread_1")!!
        assertEquals("标题", copy.title)
        assertEquals(listOf("m1", "m2"), copy.messages.map { it.id })
    }

    @Test
    fun `a copy kept for another server or device is no copy`() {
        ThreadSnapshots(dir).write(phone, "thread_1", "t", listOf(message("m1", "a")))

        val other = ConnectionProfile("https://other.example", "phone", "token")
        assertNull(ThreadSnapshots(dir).read(other, "thread_1"))
        // It is gone, not waiting for the first server to come back.
        assertNull(ThreadSnapshots(dir).read(phone, "thread_1"))
    }

    @Test
    fun `a copy that cannot be read is removed`() {
        dir.mkdirs()
        File(dir, "thread_1.json").writeText("""{"owner": 3, "messages": "no""")

        assertNull(ThreadSnapshots(dir).read(phone, "thread_1"))
        assertFalse(File(dir, "thread_1.json").exists())
    }

    @Test
    fun `a deleted conversation's copy goes, from memory and from the phone`() {
        val snapshots = ThreadSnapshots(dir)
        snapshots.write(phone, "thread_1", "t", listOf(message("m1", "a")))
        snapshots.write(phone, "thread_2", "t", listOf(message("m2", "b")))

        snapshots.forget("thread_1")

        assertNull(snapshots.remembered(phone, "thread_1"))
        assertNull(ThreadSnapshots(dir).read(phone, "thread_1"))
        assertEquals(listOf("m2"), ThreadSnapshots(dir).read(phone, "thread_2")!!.messages.map { it.id })
    }

    @Test
    fun `copies of conversations the server no longer lists go`() {
        val snapshots = ThreadSnapshots(dir)
        for (id in listOf("thread_1", "thread_2", "thread_3")) snapshots.write(phone, id, "t", listOf(message("m", id)))

        snapshots.keepOnly(setOf("thread_2", "thread_9"))

        assertNull(snapshots.remembered(phone, "thread_1"))
        assertEquals(listOf("thread_2.json"), dir.list()!!.toList())
        assertEquals(setOf("a", "c"), unlisted(copies = setOf("a", "b", "c"), listed = setOf("b", "d")))
    }

    @Test
    fun `disconnecting leaves no conversation on the phone`() {
        val snapshots = ThreadSnapshots(dir)
        snapshots.write(phone, "thread_1", "t", listOf(message("m1", "a")))
        snapshots.write(phone, "thread_2", "t", listOf(message("m2", "b")))

        snapshots.clear()

        assertNull(snapshots.remembered(phone, "thread_1"))
        assertTrue(dir.list()!!.isEmpty())
    }

    @Test
    fun `only the conversations opened most recently are kept`() {
        val snapshots = ThreadSnapshots(dir)
        repeat(ThreadSnapshots.ON_DISK + 5) { index ->
            snapshots.write(phone, "thread_$index", "t", listOf(message("m", "x")))
            File(dir, "thread_$index.json").setLastModified(1_000_000L + index * 1_000L)
        }
        snapshots.write(phone, "thread_last", "t", listOf(message("m", "x")))

        val kept = dir.list()!!.toSet()
        assertEquals(ThreadSnapshots.ON_DISK, kept.size)
        assertTrue("thread_last.json" in kept)
        assertFalse("thread_0.json" in kept)
        assertTrue("thread_${ThreadSnapshots.ON_DISK + 4}.json" in kept)
    }

    @Test
    fun `an id a file name cannot hold still gets a file of its own`() {
        assertEquals("thread_1", ThreadSnapshots.name("thread_1"))
        assertEquals("a_b_c", ThreadSnapshots.name("a/b:c"))
        val snapshots = ThreadSnapshots(dir)
        snapshots.write(phone, "../escape", "t", listOf(message("m1", "a")))
        assertEquals(listOf("___escape.json"), dir.list()!!.toList())
        assertEquals("t", ThreadSnapshots(dir).read(phone, "../escape")!!.title)
    }
}
