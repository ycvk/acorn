package io.ycvk.acorn.feature.views

import io.ycvk.acorn.api.models.ClientViewShownEvent
import io.ycvk.acorn.api.models.ClientViewUpdatedEvent
import io.ycvk.acorn.api.models.View
import io.ycvk.acorn.api.models.ViewActionSent
import io.ycvk.acorn.api.models.ViewShownData
import io.ycvk.acorn.core.sse.ChatState
import io.ycvk.acorn.core.sse.RunEventPacket
import io.ycvk.acorn.core.sse.RunEventProjection
import io.ycvk.acorn.feature.chat.ChatMessage
import io.ycvk.acorn.feature.chat.withView
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
        assertEquals(56, columnWidth(listOf("a")).value.toInt())
        assertEquals(111, columnWidth(listOf("summarization")).value.toInt())
        assertEquals(240, columnWidth(listOf("长".repeat(40))).value.toInt())
    }

    @Test
    fun `tables fit the card by wrapping their widest columns first`() {
        assertEquals(listOf(100f, 200f), fitColumns(listOf(50f, 100f), 300f))
        // 72 + 72 + 230 wants 374 in 300: only the wide column gives way.
        assertEquals(listOf(72f, 72f, 156f), fitColumns(listOf(72f, 72f, 230f), 300f))
        // Two wide columns share the cut by how far each is above the minimum.
        assertEquals(listOf(121f, 171f), fitColumns(listOf(146f, 246f), 292f))
        // When wrapping is not enough, the wide columns stop at the minimum and the table scrolls.
        assertEquals(listOf(96f, 96f, 96f), fitColumns(listOf(196f, 196f, 96f), 250f))
        // Columns at the minimum already keep their widths.
        assertEquals(listOf(96f, 96f, 96f, 96f), fitColumns(listOf(96f, 96f, 96f, 96f), 300f))
        // The four-column comparison the real model wrote fits a phone once its long columns wrap.
        assertEquals(336f, fitColumns(listOf(69f, 56f, 240f, 210f), 336f).sum(), 0.01f)
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

    @Test
    fun `an action is keyed by its component and list items, and a sent one is known by that key`() {
        val sentAt = OffsetDateTime.parse("2026-10-21T07:00:00Z")
        val sent = view.copy(
            actions = listOf(
                ViewActionSent(componentId = "save", scope = listOf(1, 0), createdAt = sentAt),
                ViewActionSent(componentId = "pick", scope = emptyList(), createdAt = sentAt, selected = listOf("b")),
            ),
        )
        assertEquals(setOf("save/1/0", "pick"), sentActions(sent).keys)
        assertEquals(listOf("b"), sentActions(sent)["pick"]?.selected)
        assertEquals("view_1:save/1/0", ViewActionPress("view_1", "save", listOf(1, 0)).id)
        assertEquals(emptyMap<String, ViewActionSent>(), sentActions(view))
    }

    @Test
    fun `picking keeps one option unless the picker takes several`() {
        assertEquals(listOf("b"), toggleChoice(listOf("a"), "b", multiple = false))
        assertEquals(listOf("a", "b"), toggleChoice(listOf("a"), "b", multiple = true))
        assertEquals(emptyList<String>(), toggleChoice(listOf("a"), "a", multiple = false))
        assertEquals(listOf("b"), toggleChoice(listOf("a", "b"), "a", multiple = true))
        val options = choiceOptions(listOf(mapOf("id" to "a", "label" to "写摘要"), mapOf("id" to "b"), "c"))
        assertEquals(listOf(ChoiceOption("a", "写摘要")), options)
    }

    @Test
    fun `an updated view is redrawn in the live reply and in the reply that showed it`() {
        val updated = view.copy(title = "eino 最近的版本（已整理）")
        val packet = RunEventPacket.ViewUpdated(
            ClientViewUpdatedEvent(eventId = "e2", runId = "run-2", seq = 6, ts = view.createdAt, type = null, data = ViewShownData(updated)),
        )
        val live = RunEventProjection().apply(ChatState(views = listOf(view)), packet)
        assertEquals(listOf(updated.title), live.views.map { it.title })
        // A view of an earlier reply is not added to the live one.
        assertEquals(emptyList<View>(), RunEventProjection().apply(ChatState(), packet).views)

        val messages = listOf(ChatMessage.User("读一下"), ChatMessage.Assistant("读完了", views = listOf(view)), ChatMessage.User("整理成笔记"))
        val redrawn = withView(messages, updated)
        assertEquals(updated.title, (redrawn[1] as ChatMessage.Assistant).views.single().title)
        assertEquals(messages[0], redrawn[0])
        assertEquals(messages[2], redrawn[2])
    }
}
