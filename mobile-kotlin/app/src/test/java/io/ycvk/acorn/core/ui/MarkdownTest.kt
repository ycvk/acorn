package io.ycvk.acorn.core.ui

import androidx.compose.ui.text.LinkAnnotation
import io.ycvk.acorn.core.theme.LightColors
import org.junit.Assert.assertEquals
import org.junit.Test

class MarkdownTest {
    private fun inline(markdown: String) = buildInline(parser.parse(markdown).firstChild, LightColors).text

    private fun links(markdown: String): List<String> {
        val text = inline(markdown)
        return text.getLinkAnnotations(0, text.length).map { (it.item as LinkAnnotation.Url).url }
    }

    @Test
    fun `web links and images open`() {
        assertEquals(
            listOf("https://example.com/a", "http://example.com/i.png", "http://www.example.com/"),
            links("[site](https://example.com/a) ![chart](http://example.com/i.png) www.example.com"),
        )
    }

    @Test
    fun `other links keep their words and open nothing`() {
        val markdown = "[run](javascript:alert(1)) [app](intent://x#Intent;end) [file](file:///etc/hosts) " +
            "[data](content://media/external/1) ![pic](file:///sdcard/a.png) owner@example.com"
        assertEquals(emptyList<String>(), links(markdown))
        assertEquals("run app file data [图片] pic owner@example.com", inline(markdown).text)
    }
}
