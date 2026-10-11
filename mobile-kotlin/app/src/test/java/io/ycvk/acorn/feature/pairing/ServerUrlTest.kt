package io.ycvk.acorn.feature.pairing

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
}
