package io.ycvk.acorn.core.net

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test

class ServerUrlTest {
    @Test
    fun `server address needs an http scheme and a host`() {
        assertNull(serverUrlProblem("https://acorn.example.com"))
        assertNull(serverUrlProblem("http://10.0.2.2:8090"))
        assertNotNull(serverUrlProblem("http:"))
        assertNotNull(serverUrlProblem("acorn.example.com"))
    }

    @Test
    fun `plain http is taken only for an internal host`() {
        listOf(
            "http://192.168.1.20:8080", "http://100.101.102.103:8080", "http://acorn.tail1234.ts.net:8080",
            "http://acorn.local:8080", "http://[fd12::1]:8080", "http://localhost:8080", "http://127.0.0.1:8080",
        ).forEach { assertNull(it, serverUrlProblem(it)) }
        val public = "公网上的服务器要用 https://，http:// 只用于局域网、Tailscale 和本机的地址"
        assertEquals(public, serverUrlProblem("http://acorn.example.com"))
        assertEquals(public, serverUrlProblem("http://93.184.215.14:8080"))
        assertEquals(public, serverUrlProblem("http://172.32.0.1:8080"))
        assertNull(serverUrlProblem("https://93.184.215.14:8443"))
    }
}
