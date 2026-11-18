package io.ycvk.acorn.core.push

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.w3c.dom.Element
import java.io.File
import javax.xml.parsers.DocumentBuilderFactory

class NotificationTapTest {
    private val extras = mapOf(
        EXTRA_THREAD_ID to " thread_1 ",
        EXTRA_ACTION_ID to "action_1",
        EXTRA_BROWSER_URL to "https://example.com/login",
    )

    @Test
    fun `a tap through the notification alias opens its thread, action and page`() {
        assertEquals(ThreadLink("thread_1", "action_1", "https://example.com/login"), notificationLink(NOTIFICATION_TAP) { extras[it] })
        assertEquals(ThreadLink("thread_1"), notificationLink(NOTIFICATION_TAP) { mapOf(EXTRA_THREAD_ID to "thread_1", EXTRA_ACTION_ID to " ")[it] })
        assertNull(notificationLink(NOTIFICATION_TAP) { mapOf(EXTRA_THREAD_ID to "  ")[it] })
    }

    @Test
    fun `a start of the launcher opens nothing, whatever extras it carries`() {
        assertNull(notificationLink("io.ycvk.acorn.MainActivity") { extras[it] })
        assertNull(notificationLink(null) { extras[it] })
    }

    @Test
    fun `the alias is declared, not exported, and takes the click action of a push`() {
        val manifest = DocumentBuilderFactory.newInstance().apply { isNamespaceAware = true }
            .newDocumentBuilder().parse(File("src/main/AndroidManifest.xml"))
        val android = "http://schemas.android.com/apk/res/android"
        fun Element.attr(name: String) = getAttributeNS(android, name)
        fun className(name: String) = if (name.startsWith(".")) "io.ycvk.acorn$name" else name
        val aliases = manifest.getElementsByTagName("activity-alias")
        val alias = (0 until aliases.length).map { aliases.item(it) as Element }.single { className(it.attr("name")) == NOTIFICATION_TAP }
        assertEquals("false", alias.attr("exported"))
        assertEquals("io.ycvk.acorn.MainActivity", className(alias.attr("targetActivity")))
        val actions = alias.getElementsByTagName("action")
        assertEquals(listOf(ACTION_OPEN_NOTIFICATION), (0 until actions.length).map { (actions.item(it) as Element).attr("name") })
        val categories = alias.getElementsByTagName("category")
        assertEquals(listOf("android.intent.category.DEFAULT"), (0 until categories.length).map { (categories.item(it) as Element).attr("name") })
    }
}
