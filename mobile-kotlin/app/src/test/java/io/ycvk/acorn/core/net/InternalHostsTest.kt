package io.ycvk.acorn.core.net

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class InternalHostsTest {
    @Test
    fun `hosts on the owner's own networks are internal`() {
        listOf(
            "10.0.0.1", "10.255.255.255",
            "172.16.0.1", "172.31.255.255",
            "192.168.1.20",
            "100.64.0.1", "100.101.102.103", "100.127.255.255",
            "169.254.10.1",
            "127.0.0.1", "127.5.5.5", "localhost", "LOCALHOST", "localhost.",
            "::1", "[::1]", "fd12::1", "fc00::1", "fe80::1", "febf::1", "::ffff:192.168.1.20",
            "acorn.local", "foo.local.", "bar.ts.net", "acorn.tail1234.ts.net",
        ).forEach { assertTrue(it, isInternalHost(it)) }
    }

    @Test
    fun `every other host is public`() {
        listOf(
            "172.15.0.1", "172.32.0.1", "192.169.0.1", "100.63.255.255", "100.128.0.1", "169.253.0.1", "11.0.0.1", "8.8.8.8",
            "256.0.0.1", "10.0.0", "fec0::1", "fe7f::1", "2606:4700::1111", "::", "::ffff:8.8.8.8",
            "acorn.example.com", "local", "ts.net", "acorn.local.example.com", "ts.net.example.com", "10.0.0.1.nip.io", "nas", "nas.home.arpa",
        ).forEach { assertFalse(it, isInternalHost(it)) }
    }
}
