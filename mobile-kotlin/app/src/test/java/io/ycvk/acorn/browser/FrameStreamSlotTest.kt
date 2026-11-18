package io.ycvk.acorn.browser

import io.ycvk.acorn.feature.browser.FrameStreamSlot
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class FrameStreamSlotTest {
    private class Stream(val number: Long) : AutoCloseable {
        var closed = false

        override fun close() {
            closed = true
        }
    }

    private val slot = FrameStreamSlot()
    private val streams = mutableListOf<Stream>()

    private fun open() = slot.open { number -> Stream(number).also(streams::add) }

    @Test
    fun `opening a stream closes the one before it`() {
        open()
        open()
        assertEquals(listOf(true, false), streams.map { it.closed })
    }

    @Test
    fun `the end a closed stream reports leaves the stream after it open`() {
        open()
        // A new screen size or a new start opens the same tab again.
        open()
        assertFalse(slot.ended(streams[0].number))
        assertTrue(slot.isOpen)
        assertTrue(slot.ended(streams[1].number))
        assertFalse(slot.isOpen)
    }

    @Test
    fun `a stream closed when the screen goes has no end to report`() {
        open()
        slot.close()
        assertTrue(streams.single().closed)
        assertFalse(slot.ended(streams.single().number))
    }
}
