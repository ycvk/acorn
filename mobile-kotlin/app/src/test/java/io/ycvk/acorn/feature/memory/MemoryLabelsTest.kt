package io.ycvk.acorn.feature.memory

import io.ycvk.acorn.api.models.MemoryEvidence
import io.ycvk.acorn.api.models.MemorySummary
import org.junit.Assert.assertEquals
import org.junit.Test
import java.time.OffsetDateTime
import java.time.ZoneOffset

class MemoryLabelsTest {
    private val at = OffsetDateTime.parse("2026-10-11T08:00:00Z")
    private val fact = MemorySummary(
        id = "m_1",
        kind = MemorySummary.Kind.fact,
        content = "每天早上喝咖啡",
        state = "current",
        revision = 1,
        pinned = false,
        needsReview = false,
        updatedAt = at,
    )

    private fun evidence(kind: String, speaker: String, notePath: String? = null, toolName: String? = null) = MemoryEvidence(
        quote = "q",
        relation = MemoryEvidence.Relation.supports,
        sourceKind = kind,
        speaker = speaker,
        recordedAt = at,
        notePath = notePath,
        toolName = toolName,
    )

    @Test
    fun `evidence names where it came from in the owner's terms`() {
        assertEquals("你在对话里说", evidenceSource(evidence("message", "owner")))
        assertEquals("Acorn 在对话里说", evidenceSource(evidence("message", "assistant")))
        assertEquals("你在 App 里改的", evidenceSource(evidence("request", "owner")))
        assertEquals("笔记 inbox/tokio.md", evidenceSource(evidence("knowledge", "assistant", notePath = "inbox/tokio.md")))
        assertEquals("读取网页的结果", evidenceSource(evidence("event", "tool", toolName = "web_fetch")))
    }

    @Test
    fun `marks show a kept memory and one in question`() {
        assertEquals(emptyList<String>(), memoryMarks(fact))
        assertEquals(listOf("你让它记住的"), memoryMarks(fact.copy(pinned = true)))
        assertEquals(listOf("有矛盾"), memoryMarks(fact.copy(state = "disputed", needsReview = true)))
        assertEquals(listOf("待确认"), memoryMarks(fact.copy(needsReview = true)))
    }

    @Test
    fun `meta adds the period a memory holds for`() {
        val now = at.plusDays(2)
        assertEquals("2 天前更新", memoryMeta(fact, now, ZoneOffset.UTC))
        assertEquals(
            "2 天前更新  ·  从2026年10月1日起",
            memoryMeta(fact.copy(validFrom = OffsetDateTime.parse("2026-10-01T00:00:00Z")), now, ZoneOffset.UTC),
        )
    }

    @Test
    fun `an empty list reads differently when narrowed`() {
        val empty = MemoryListLoad.Loaded(emptyList(), hasMore = false)
        assertEquals(MemoryListContent.Empty, memoryListContent(empty, " ", null, 50))
        assertEquals(MemoryListContent.NoMatches, memoryListContent(empty, "茶", null, 50))
        assertEquals(MemoryListContent.NoMatches, memoryListContent(empty, "", MemorySummary.Kind.insight, 50))
    }

    @Test
    fun `more loads until the server limit`() {
        val page = MemoryListLoad.Loaded(listOf(fact), hasMore = true)
        assertEquals(MemoryListContent.Items(listOf(fact), canLoadMore = true), memoryListContent(page, "", null, 150))
        assertEquals(MemoryListContent.Items(listOf(fact), canLoadMore = false), memoryListContent(page, "", null, 200))
    }
}
