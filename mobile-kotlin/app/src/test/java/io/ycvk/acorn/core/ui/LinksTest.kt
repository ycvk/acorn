package io.ycvk.acorn.core.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class LinksTest {
    @Test
    fun `only http and https links open`() {
        assertEquals("https://example.com/a?b=1", webLink("https://example.com/a?b=1"))
        assertEquals("http://192.168.1.20:8080/", webLink("HTTP://192.168.1.20:8080"))
        listOf(
            "javascript:alert(1)",
            "JavaScript:alert(document.cookie)",
            "intent://scan/#Intent;scheme=zxing;package=com.google.zxing.client.android;end",
            "file:///data/data/io.ycvk.acorn/files/connection",
            "content://io.ycvk.acorn.files/cache/a.txt",
            "mailto:owner@example.com",
            "/relative/path",
            "https://",
        ).forEach { assertNull(it, webLink(it)) }
    }

    @Test
    fun `what opens is the address as checked`() {
        // The system would read " https" as the scheme of the address as written.
        assertEquals("https://example.com/", webLink(" https://example.com"))
    }

    @Test
    fun `a link is named by its host without www`() {
        assertEquals("chinanews.com.cn", linkHost("https://www.chinanews.com.cn/sh/2026/11-09/10728761.shtml"))
        assertEquals("daily.ai225.com", linkHost("https://daily.ai225.com/daily/2026-11-10"))
        assertEquals("github.com", linkHost("https://www.github.com/login"))
    }

    @Test
    fun `a link without a host to name is shown as it is`() {
        assertEquals("about:blank", linkHost("about:blank"))
        assertEquals("not a link", linkHost("not a link"))
        assertEquals("https://www./", linkHost("https://www./"))
    }
}
