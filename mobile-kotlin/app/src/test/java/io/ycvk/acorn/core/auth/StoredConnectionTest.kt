package io.ycvk.acorn.core.auth

import org.junit.Assert.assertEquals
import org.junit.Test

class StoredConnectionTest {
    private fun paired(serverUrl: String) = ConnectionProfile(serverUrl, "device_1", "token")

    @Test
    fun `a stored pairing to a public host over http is refused`() {
        assertEquals(
            StoredConnection.Refused("公网上的服务器要用 https://，http:// 只用于局域网、Tailscale 和本机的地址"),
            storedConnection(paired("http://acorn.example.com")),
        )
        assertEquals(StoredConnection.Refused("公网上的服务器要用 https://，http:// 只用于局域网、Tailscale 和本机的地址"), storedConnection(paired("http://93.184.215.14:8080")))
    }

    @Test
    fun `a stored pairing over https or to an internal host is kept`() {
        listOf("https://acorn.example.com", "http://192.168.1.20:8080", "http://100.101.102.103:8080", "http://acorn.tail1234.ts.net:8080", "http://10.0.2.2:8090").forEach {
            assertEquals(it, StoredConnection.Usable(paired(it)), storedConnection(paired(it)))
        }
        assertEquals(StoredConnection.None, storedConnection(null))
    }
}
