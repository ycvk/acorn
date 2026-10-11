package io.ycvk.acorn.feature.threads

import io.ycvk.acorn.api.models.Thread
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.time.OffsetDateTime

class ThreadPreviewTest {
    private fun thread(preview: String?, role: Thread.PreviewRole?) = Thread(
        id = "t",
        title = "t",
        createdAt = OffsetDateTime.now(),
        updatedAt = OffsetDateTime.now(),
        state = Thread.State.completed,
        preview = preview,
        previewRole = role,
    )

    @Test
    fun `preview reads as plain text and marks the owner's own message`() {
        assertEquals("找到了三条", threadPreview(thread("找到了**三**条", Thread.PreviewRole.assistant)))
        assertEquals("你：帮我查一下", threadPreview(thread("帮我查一下", Thread.PreviewRole.user)))
        assertNull(threadPreview(thread(null, null)))
    }
}
