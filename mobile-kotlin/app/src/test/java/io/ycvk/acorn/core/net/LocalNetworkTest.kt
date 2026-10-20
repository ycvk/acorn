package io.ycvk.acorn.core.net

import java.net.InetAddress
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class LocalNetworkTest {
    private fun at(literal: String) = listOf(InetAddress.getByName(literal))

    @Test
    fun `servers on the LAN need nearby devices`() {
        assertTrue(isLocalNetworkHost("192.168.1.20", at("192.168.1.20")))
        assertTrue(isLocalNetworkHost("10.0.2.2", at("10.0.2.2")))
        assertTrue(isLocalNetworkHost("172.20.0.5", at("172.20.0.5")))
        assertTrue(isLocalNetworkHost("169.254.10.1", at("169.254.10.1")))
        assertTrue(isLocalNetworkHost("fd12::1", at("fd12::1")))
        assertTrue(isLocalNetworkHost("fe80::1", at("fe80::1")))
        assertTrue(isLocalNetworkHost("acorn.local", emptyList()))
        assertTrue(isLocalNetworkHost("nas.home.arpa", at("192.168.0.9")))
    }

    @Test
    fun `public servers and the phone itself do not`() {
        assertFalse(isLocalNetworkHost("acorn.example.com", at("93.184.215.14")))
        assertFalse(isLocalNetworkHost("2606:4700::1111", at("2606:4700::1111")))
        assertFalse(isLocalNetworkHost("127.0.0.1", at("127.0.0.1")))
        assertFalse(isLocalNetworkHost("172.32.0.1", at("172.32.0.1")))
        assertFalse(isLocalNetworkHost("unresolved.example", emptyList()))
    }
}
