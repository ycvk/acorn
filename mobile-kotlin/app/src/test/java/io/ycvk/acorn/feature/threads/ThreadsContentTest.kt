package io.ycvk.acorn.feature.threads

import org.junit.Assert.assertEquals
import org.junit.Test

class ThreadsContentTest {

    @Test
    fun `loading without threads shows progress, not the empty state`() {
        assertEquals(ThreadsContent.Loading, threadsContent(ThreadsLoad.Loading, threadCount = 0))
    }

    @Test
    fun `failed load without threads shows the error`() {
        assertEquals(
            ThreadsContent.Failed("connection refused"),
            threadsContent(ThreadsLoad.Failed("connection refused"), threadCount = 0),
        )
    }

    @Test
    fun `empty state only after a successful empty load`() {
        assertEquals(ThreadsContent.Empty, threadsContent(ThreadsLoad.Loaded, threadCount = 0))
    }

    @Test
    fun `threads on screen stay visible while reloading or after a failed reload`() {
        assertEquals(ThreadsContent.Items, threadsContent(ThreadsLoad.Loading, threadCount = 2))
        assertEquals(ThreadsContent.Items, threadsContent(ThreadsLoad.Failed("x"), threadCount = 2))
        assertEquals(ThreadsContent.Items, threadsContent(ThreadsLoad.Loaded, threadCount = 2))
    }
}
