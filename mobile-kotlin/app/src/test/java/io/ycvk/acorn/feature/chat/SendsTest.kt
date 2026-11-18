package io.ycvk.acorn.feature.chat

import org.junit.Assert.assertEquals
import org.junit.Test

class SendsTest {
    private val photo = listOf("k1")
    private val unanswered = UnansweredSend("thread_1", "看看这张", listOf("k1"), "c1", mapOf("k1" to "file_1"))

    @Test
    fun `the same message sent again keeps its client id and its uploads`() {
        assertEquals(SendIdentity("c1", mapOf("k1" to "file_1")), sendIdentity(unanswered, "thread_1", "看看这张", photo) { "new" })
        // Failed before its thread existed, it repeats in the thread made then.
        assertEquals("c1", sendIdentity(unanswered.copy(threadId = null), "thread_1", "看看这张", photo) { "new" }.clientId)
    }

    @Test
    fun `a changed message is a new send`() {
        val fresh = SendIdentity("new", emptyMap())
        assertEquals(fresh, sendIdentity(null, "thread_1", "看看这张", photo) { "new" })
        assertEquals(fresh, sendIdentity(unanswered, "thread_1", "看看这个", photo) { "new" })
        assertEquals(fresh, sendIdentity(unanswered, "thread_1", "看看这张", emptyList()) { "new" })
        assertEquals(fresh, sendIdentity(unanswered, "thread_2", "看看这张", photo) { "new" })
    }
}
