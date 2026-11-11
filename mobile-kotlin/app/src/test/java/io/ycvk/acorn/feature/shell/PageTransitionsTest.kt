package io.ycvk.acorn.feature.shell

import androidx.compose.ui.geometry.Rect
import org.junit.Assert.assertEquals
import org.junit.Test

class PageTransitionsTest {
    private val chat = Page.Chat("thread_1")
    private val story = Page.Story(7)

    @Test
    fun `a deeper page is pushed and the one underneath is popped back to`() {
        assertEquals(PageMove.Push, pageMove(null, Page.Settings, listOf(Page.Settings)))
        assertEquals(PageMove.Push, pageMove(Page.Settings, Page.Persona, listOf(Page.Settings, Page.Persona)))
        // The page left behind is no longer in the stack.
        assertEquals(PageMove.Pop, pageMove(Page.Persona, Page.Settings, listOf(Page.Settings)))
        assertEquals(PageMove.Pop, pageMove(Page.Settings, null, emptyList()))
        // A back gesture holds the pop while the page it leaves is still on the stack.
        assertEquals(PageMove.Pop, pageMove(Page.Persona, Page.Settings, listOf(Page.Settings, Page.Persona)))
    }

    @Test
    fun `a page that takes another's place comes forward as that one leaves`() {
        assertEquals(PageMove.Pop, pageMove(Page.Memory("a"), Page.Memory("b"), listOf(Page.Memory("b"))))
    }

    @Test
    fun `a story grows out of Today and shrinks back, and moves like any page otherwise`() {
        assertEquals(PageMove.Grow, pageMove(null, story, listOf(story)))
        assertEquals(PageMove.Shrink, pageMove(story, null, emptyList()))
        assertEquals(PageMove.Shrink, pageMove(story, null, listOf(story)))
        assertEquals(PageMove.Push, pageMove(story, chat, listOf(story, chat)))
        assertEquals(PageMove.Pop, pageMove(chat, story, listOf(story)))
    }

    @Test
    fun `the tabs are the bottom of the stack`() {
        assertEquals(0, depthOf(null, listOf(story, chat)))
        assertEquals(1, depthOf(story, listOf(story, chat)))
        assertEquals(2, depthOf(chat, listOf(story, chat)))
        assertEquals(Int.MAX_VALUE, depthOf(Page.Settings, listOf(story, chat)))
    }

    @Test
    fun `a back gesture holds the pop where the finger is`() {
        assertEquals(0f, dragFraction(0f, 1080f), 0f)
        assertEquals(0.25f, dragFraction(270f, 1080f), 1e-6f)
        assertEquals(0.5f, dragFraction(540f, 1080f), 1e-6f)
        // The last of the way is left for letting go.
        assertEquals(0.96f, dragFraction(1080f, 1080f), 1e-6f)
        assertEquals(0.96f, dragFraction(5000f, 1080f), 1e-6f)
        assertEquals(0f, dragFraction(300f, 0f), 0f)
    }

    @Test
    fun `a move a gesture carried part way takes the rest of its time, and never snaps`() {
        assertEquals(360, moveMillis(PageMove.Pop, 0f))
        assertEquals(180, moveMillis(PageMove.Pop, 0.5f))
        assertEquals(140, moveMillis(PageMove.Pop, 0.9f))
        assertEquals(140, moveMillis(PageMove.Pop, 1f))
        assertEquals(420, moveMillis(PageMove.Push, 0f))
        assertEquals(460, moveMillis(PageMove.Grow, 0f))
        assertEquals(230, moveMillis(PageMove.Shrink, 0.5f))
    }

    private val card = Rect(left = 40f, top = 900f, right = 1040f, bottom = 1300f)

    @Test
    fun `a page that has not grown shows through its card, from under its bar`() {
        val frame = containerFrame(card, width = 1080f, height = 2400f, bodyTop = 200f, cardCorner = 50f, grown = 0f)
        val scale = 1000f / 1080f
        assertEquals(scale, frame.scale, 1e-4f)
        assertEquals(40f, frame.x, 1e-3f)
        // The window opens where the page's own content starts, and that point sits on the card's top.
        assertEquals(200f, frame.clipTop, 1e-3f)
        assertEquals(900f, frame.y + frame.clipTop * frame.scale, 1e-2f)
        assertEquals(400f, frame.clipHeight * frame.scale, 1e-2f)
        assertEquals(50f, frame.corner * frame.scale, 1e-3f)
    }

    @Test
    fun `a grown page fills the screen`() {
        assertEquals(
            ContainerFrame(scale = 1f, x = 0f, y = 0f, clipTop = 0f, clipHeight = 2400f, corner = 0f),
            containerFrame(card, width = 1080f, height = 2400f, bodyTop = 200f, cardCorner = 50f, grown = 1f),
        )
        assertEquals(
            containerFrame(card, 1080f, 2400f, 200f, 50f, grown = 1f),
            containerFrame(card, 1080f, 2400f, 200f, 50f, grown = 1.4f),
        )
    }

    @Test
    fun `half way the window is half way between the card and the screen`() {
        val frame = containerFrame(card, width = 1080f, height = 2400f, bodyTop = 200f, cardCorner = 50f, grown = 0.5f)
        assertEquals(20f, frame.x, 1e-3f)
        assertEquals(1040f / 1080f, frame.scale, 1e-4f)
        // Top edge of the window on screen, and its height there.
        assertEquals(450f, frame.y + frame.clipTop * frame.scale, 1e-2f)
        assertEquals(1850f - 450f, frame.clipHeight * frame.scale, 1e-2f)
    }
}
