package io.ycvk.acorn.core.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class LinksTest {
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
