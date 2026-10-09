package io.ycvk.acorn.feature.share

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream

class SharedContentTest {
    private val send = "android.intent.action.SEND"

    @Test
    fun `text shares keep text and subject`() {
        assertEquals(
            SharedContent("https://tokio.rs", "Tokio", null),
            parseShare(send, "text/plain", "  https://tokio.rs ", " Tokio ", null),
        )
    }

    @Test
    fun `image shares keep the stream and any text`() {
        assertEquals(
            SharedContent("look", null, "content://media/1"),
            parseShare(send, "image/jpeg", "look", "", "content://media/1"),
        )
    }

    @Test
    fun `other shares are not taken`() {
        assertNull(parseShare("android.intent.action.SEND_MULTIPLE", "image/png", null, null, "content://x"))
        assertNull(parseShare(send, "application/pdf", "x", null, "content://x"))
        assertNull(parseShare(send, "text/plain", "  ", null, null))
        assertNull(parseShare(send, "image/png", null, null, null))
        assertNull(parseShare(send, null, "x", null, null))
    }

    @Test
    fun `the note follows the shared text`() {
        assertEquals("https://tokio.rs\n\nread this weekend", captureText("https://tokio.rs", " read this weekend "))
        assertEquals("only a note", captureText(null, "only a note"))
        assertEquals("https://tokio.rs", captureText("https://tokio.rs", "  "))
        assertNull(captureText(null, ""))
    }

    @Test
    fun `copying stops past the limit`() {
        val out = ByteArrayOutputStream()
        assertTrue(copyWithin(ByteArrayInputStream(ByteArray(10)), out, 10))
        assertEquals(10, out.size())
        assertFalse(copyWithin(ByteArrayInputStream(ByteArray(11)), ByteArrayOutputStream(), 10))
    }
}
