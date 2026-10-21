package io.ycvk.acorn.feature.views

import io.ycvk.acorn.api.models.ClientViewShownEvent
import io.ycvk.acorn.api.models.View
import io.ycvk.acorn.api.models.ViewShownData
import io.ycvk.acorn.core.sse.ChatState
import io.ycvk.acorn.core.sse.RunEventPacket
import io.ycvk.acorn.core.sse.RunEventProjection
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.time.OffsetDateTime

class ViewTreeTest {

    private val view = View(
        id = "view_1",
        title = "eino 最近的版本",
        catalog = "acorn.views.v1",
        components = listOf(
            mapOf("id" to "root", "component" to "List", "items" to mapOf("path" to "/books"), "template" to "title"),
            mapOf("id" to "title", "component" to "Text", "text" to mapOf("path" to "name")),
            mapOf("id" to "lost", "component" to "Text"),
            mapOf("component" to "Text"),
        ),
        `data` = mapOf(
            "books" to listOf(mapOf("name" to "小重山"), mapOf("name" to 42.0)),
            "a/b" to mapOf("~k" to listOf(10.0, 20.5)),
        ),
        createdAt = OffsetDateTime.parse("2026-10-21T06:00:00Z"),
    )

    @Test
    fun `paths resolve against the data or the list item they are drawn for`() {
        val tree = viewTree(view)
        val root = tree.root!!
        val items = tree.list(root, "items", emptyList())
        assertEquals(2, items.size)
        val title = tree.component(root.props["template"] as String)!!
        assertEquals("小重山", tree.text(title, "text", listOf(items[0])))
        assertEquals("42", tree.text(title, "text", listOf(items[1])))
        assertNull("a relative path outside a list finds nothing", tree.text(title, "text", emptyList()))
        assertEquals(20.5, tree.resolve(mapOf("path" to "/a~1b/~0k/1"), emptyList()))
        assertEquals("written out", tree.resolve("written out", emptyList()))
        assertEquals(mapOf("name" to "小重山"), tree.resolve(mapOf("path" to ""), listOf(items[0])))
    }

    @Test
    fun `a component without an id or type is left out`() {
        val tree = viewTree(view)
        assertEquals("Text", tree.component("lost")?.type)
        assertNull(tree.component(null))
    }

    @Test
    fun `json pointers follow rfc 6901`() {
        val doc = mapOf("xs" to listOf("a", "b"))
        assertEquals("b", lookup(doc, "/xs/1"))
        assertNull(lookup(doc, "/xs/01"))
        assertNull(lookup(doc, "/xs/2"))
        assertNull(lookup(doc, "/missing"))
        assertNull(lookup(doc, "xs"))
        assertEquals(doc, lookup(doc, ""))
    }

    @Test
    fun `numbers read as people write them`() {
        assertEquals("42", displayText(42.0))
        assertEquals("180.5", displayText(180.5))
        assertEquals("-3", displayText(-3.0))
        assertNull(displayText(listOf(1)))
        assertEquals("120 stars", valueLabel(120.0, "stars"))
        assertEquals("1.5", valueLabel(1.5, null))
    }

    @Test
    fun `chart points and table rows keep only what fits`() {
        assertEquals(
            listOf(ChartPoint("9 月", 120.0)),
            chartPoints(listOf(mapOf("label" to "9 月", "value" to 120.0), mapOf("label" to "x"), "y")),
        )
        assertEquals(
            listOf(listOf("v0.9.21", ""), listOf("v0.9.20", "3")),
            tableRows(listOf(listOf("v0.9.21", null), listOf("v0.9.20", 3.0), "not a row"), 2),
        )
    }

    @Test
    fun `columns are as wide as their text within limits`() {
        assertEquals(72, columnWidth(listOf("a")).value.toInt())
        assertEquals(111, columnWidth(listOf("summarization")).value.toInt())
        assertEquals(240, columnWidth(listOf("长".repeat(40))).value.toInt())
    }

    @Test
    fun `tables fit the card by wrapping their widest columns first`() {
        assertEquals(listOf(100f, 200f), fitColumns(listOf(50f, 100f), 300f))
        // 72 + 72 + 230 wants 374 in 300: only the wide column gives way.
        assertEquals(listOf(72f, 72f, 156f), fitColumns(listOf(72f, 72f, 230f), 300f))
        // Two wide columns share the cut by how far each is above the minimum.
        assertEquals(listOf(135f, 165f), fitColumns(listOf(160f, 240f), 300f))
        // Columns that are all narrow already keep their widths and scroll.
        assertEquals(listOf(100f, 100f, 100f, 100f), fitColumns(listOf(100f, 100f, 100f, 100f), 300f))
        // When wrapping is not enough, the wide columns stop at the minimum.
        assertEquals(listOf(120f, 120f, 100f), fitColumns(listOf(200f, 200f, 100f), 300f))
    }

    @Test
    fun `a replayed stream shows a view once`() {
        val packet = RunEventPacket.ViewShown(
            ClientViewShownEvent(eventId = "e1", runId = "run-1", seq = 4, ts = view.createdAt, type = null, data = ViewShownData(view)),
        )
        val projection = RunEventProjection()
        val once = projection.apply(ChatState(), packet)
        val twice = projection.apply(once, packet)
        assertEquals(listOf("view_1"), twice.views.map { it.id })
    }
}
