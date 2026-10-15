package io.ycvk.acorn.core.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class RevisionsTest {
    private fun same(text: String) = DiffLine(DiffKind.Same, text)
    private fun added(text: String) = DiffLine(DiffKind.Added, text)
    private fun removed(text: String) = DiffLine(DiffKind.Removed, text)

    @Test
    fun `identical text has no changed lines`() {
        assertEquals(listOf(same("a"), same("b")), lineDiff("a\nb", "a\nb"))
    }

    @Test
    fun `a changed line shows as removed then added between unchanged ones`() {
        assertEquals(
            listOf(same("你是 Acorn。"), removed("说话简短。"), added("说话随意点。"), same("用中文回答。")),
            lineDiff("你是 Acorn。\n说话简短。\n用中文回答。", "你是 Acorn。\n说话随意点。\n用中文回答。"),
        )
    }

    @Test
    fun `lines only on one side are added or removed`() {
        assertEquals(listOf(same("a"), added("b"), same("c")), lineDiff("a\nc", "a\nb\nc"))
        assertEquals(listOf(removed("a"), same("b")), lineDiff("a\nb", "b"))
        assertEquals(listOf(removed(""), added("x")), lineDiff("", "x"))
    }
}
