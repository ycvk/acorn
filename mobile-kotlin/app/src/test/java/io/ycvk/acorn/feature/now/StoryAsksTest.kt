package io.ycvk.acorn.feature.now

import org.junit.Assert.assertEquals
import org.junit.Test

class StoryAsksTest {
    private val unanswered = UnansweredAsk(41, StoryAsk.Discuss, "ask-1")

    @Test
    fun `the same ask made again keeps its client id`() {
        assertEquals("ask-1", askClientId(unanswered, 41, StoryAsk.Discuss) { "new" })
    }

    @Test
    fun `another ask gets a new client id`() {
        assertEquals("new", askClientId(null, 41, StoryAsk.Discuss) { "new" })
        assertEquals("new", askClientId(unanswered, 41, StoryAsk.Follow) { "new" })
        assertEquals("new", askClientId(unanswered, 42, StoryAsk.Discuss) { "new" })
    }
}
