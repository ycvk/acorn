package io.ycvk.acorn.feature.knowledge

import io.ycvk.acorn.api.models.KnowledgeNoteSummary
import io.ycvk.acorn.core.ui.Load
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
        assertEquals(KnowledgeContent.Loading, knowledgeContent(Load.Loading, ""))
        assertEquals(KnowledgeContent.Failed("boom"), knowledgeContent(Load.Failed("boom"), ""))
        assertEquals(KnowledgeContent.Items(listOf(note)), knowledgeContent(Load.Loaded(listOf(note)), "tokio"))
    }

    @Test
    fun `an empty result reads differently with and without a query`() {
        assertEquals(KnowledgeContent.Empty, knowledgeContent(Load.Loaded(emptyList()), "  "))
        assertEquals(KnowledgeContent.NoMatches, knowledgeContent(Load.Loaded(emptyList()), "rust"))
    }

    @Test
    fun `a deleted note leaves the list it was in`() {
        val other = note.copy(path = "inbox/rust.md", title = "Rust")
        assertEquals(Load.Loaded(listOf(other)), withoutNote(Load.Loaded(listOf(note, other)), "inbox/tokio.md"))
        assertEquals(Load.Loading, withoutNote(Load.Loading, "inbox/tokio.md"))
    }
}
