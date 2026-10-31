package io.ycvk.acorn.feature.threads

import io.ycvk.acorn.core.ui.Load
import org.junit.Assert.assertEquals
import org.junit.Test

class ThreadsContentTest {

    @Test
    fun `loading without threads shows progress, not the empty state`() {
        assertEquals(ThreadsContent.Loading, threadsContent(Load.Loading, threadCount = 0))
    }

    @Test
    fun `failed load without threads shows the error`() {
        assertEquals(
            ThreadsContent.Failed("connection refused"),
            threadsContent(Load.Failed("connection refused"), threadCount = 0),
        )
    }

    @Test
    fun `empty state only after a successful empty load`() {
        assertEquals(ThreadsContent.Empty, threadsContent(Load.Loaded(Unit), threadCount = 0))
    }

    @Test
    fun `threads on screen stay visible while reloading or after a failed reload`() {
        assertEquals(ThreadsContent.Items, threadsContent(Load.Loading, threadCount = 2))
        assertEquals(ThreadsContent.Items, threadsContent(Load.Failed("x"), threadCount = 2))
        assertEquals(ThreadsContent.Items, threadsContent(Load.Loaded(Unit), threadCount = 2))
    }
}
