package io.ycvk.acorn.feature.memory

import io.ycvk.acorn.api.models.MemorySummary
import io.ycvk.acorn.api.models.Portrait
import io.ycvk.acorn.api.models.PortraitEvidence
import io.ycvk.acorn.api.models.PortraitLine
import io.ycvk.acorn.core.ui.Load
import org.junit.Assert.assertEquals
import org.junit.Test
import java.time.OffsetDateTime

class PortraitSectionTest {
    private val at = OffsetDateTime.parse("2026-11-05T03:00:00Z")
    private val line = PortraitLine(
        id = 1,
        content = "在用 Go 写个人 agent Acorn",
        evidence = listOf(PortraitEvidence(excerpt = "我在写 Acorn", sourceKind = "message", recordedAt = at, threadId = "thread_1")),
    )

    @Test
    fun `the portrait shows on the unfiltered list once it has lines`() {
        val loaded = Load.Loaded(Portrait(lines = listOf(line), updatedAt = at))
        assertEquals(PortraitSection.Lines(listOf(line)), portraitSection(loaded, "", null))
        assertEquals(PortraitSection.Hidden, portraitSection(loaded, "Go", null))
        assertEquals(PortraitSection.Hidden, portraitSection(loaded, "", MemorySummary.Kind.fact))
        assertEquals(PortraitSection.Hidden, portraitSection(Load.Loaded(Portrait(lines = emptyList())), "", null))
        assertEquals(PortraitSection.Hidden, portraitSection(Load.Loading, "", null))
        assertEquals(PortraitSection.Failed("网络不通"), portraitSection(Load.Failed("网络不通"), "", null))
    }

    @Test
    fun `a line says how many of the owner's words it rests on`() {
        assertEquals("依据 1 条你说过的话", portraitBasis(line))
        assertEquals("依据已被忘掉", portraitBasis(line.copy(evidence = emptyList())))
    }
}
