package io.ycvk.acorn.feature.knowledge

import io.ycvk.acorn.api.models.KnowledgeNoteSummary
import org.junit.Assert.assertEquals
import org.junit.Test
import java.time.OffsetDateTime

class KnowledgeContentTest {
    private val note = KnowledgeNoteSummary(
        path = "inbox/tokio.md",
        title = "Tokio",
        tags = emptyList(),
        snippet = "Async runtime.",
        updatedAt = OffsetDateTime.parse("2026-10-03T01:00:00Z"),
    )

    @Test
    fun `list area follows the load`() {
        assertEquals(KnowledgeContent.Loading, knowledgeContent(KnowledgeLoad.Loading, ""))
        assertEquals(KnowledgeContent.Failed("boom"), knowledgeContent(KnowledgeLoad.Failed("boom"), ""))
        assertEquals(KnowledgeContent.Items(listOf(note)), knowledgeContent(KnowledgeLoad.Loaded(listOf(note)), "tokio"))
    }

    @Test
    fun `an empty result reads differently with and without a query`() {
        assertEquals(KnowledgeContent.Empty, knowledgeContent(KnowledgeLoad.Loaded(emptyList()), "  "))
        assertEquals(KnowledgeContent.NoMatches, knowledgeContent(KnowledgeLoad.Loaded(emptyList()), "rust"))
    }

    @Test
    fun `attachment embeds become placeholders`() {
        assertEquals(
            "Photo:\n*Attachment: ab12.png*\nend",
            withAttachmentPlaceholders("Photo:\n![[attachments/2026/10/ab12.png]]\nend"),
        )
        assertEquals("[[link]] stays", withAttachmentPlaceholders("[[link]] stays"))
    }
}
