package io.ycvk.acorn.core.sse

import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.ui.userMessage
import io.ycvk.acorn.data.repository.BrowserRepository
import okhttp3.OkHttpClient
import org.junit.Assert.assertEquals
import org.junit.Test
import java.net.InetAddress
import java.net.ServerSocket
import java.util.concurrent.CompletableFuture
import java.util.concurrent.TimeUnit
import kotlin.concurrent.thread

class StreamFailureTest {
    private val revoked = """{"error":{"code":"device_revoked","message":"device has been revoked"}}"""

    /** A server that answers one request with [status] and a JSON [body]; its address. */
    private fun answering(status: String, body: String): String {
        val server = ServerSocket(0, 1, InetAddress.getLoopbackAddress())
        thread(isDaemon = true) {
            server.use {
                it.accept().use { socket ->
                    // Reads the request up to the blank line after its headers.
                    socket.getInputStream().bufferedReader().lineSequence().takeWhile { line -> line.isNotEmpty() }.count()
                    val bytes = body.toByteArray()
                    socket.getOutputStream().apply {
                        write("HTTP/1.1 $status\r\nContent-Type: application/json\r\nContent-Length: ${bytes.size}\r\nConnection: close\r\n\r\n".toByteArray())
                        write(bytes)
                        flush()
                    }
                }
            }
        }
        return "http://127.0.0.1:${server.localPort}"
    }

    private fun profile(serverUrl: String) = ConnectionProfile(serverUrl, "device_1", "token")

    @Test
    fun `a run stream refused for a revoked device asks to pair again`() {
        val failed = CompletableFuture<Throwable>()
        RunEventStreamClient(OkHttpClient()).streamRunEvents(
            profile(answering("403 Forbidden", revoked)), "run_1", 0,
            onEvent = {}, onError = { failed.complete(it) }, onClosed = { failed.complete(IllegalStateException("closed")) },
        )
        assertEquals("这台设备的授权已失效，需要重新配对", userMessage(failed.get(10, TimeUnit.SECONDS)))
    }

    @Test
    fun `a browser stream refused for a revoked device asks to pair again`() {
        val failed = CompletableFuture<Throwable>()
        BrowserRepository(OkHttpClient()).frames(
            profile(answering("403 Forbidden", revoked)), "tab_1", 400, 800,
            onFrame = {}, onError = { failed.complete(it) }, onClosed = { failed.complete(IllegalStateException("closed")) },
        )
        assertEquals("这台设备的授权已失效，需要重新配对", userMessage(failed.get(10, TimeUnit.SECONDS)))
    }

    @Test
    fun `a stream the server fails reads as a server error`() {
        val failed = CompletableFuture<Throwable>()
        RunEventStreamClient(OkHttpClient()).streamRunEvents(
            profile(answering("503 Service Unavailable", """{"error":{"code":"execution_not_ready","message":"not ready"}}""")), "run_1", 0,
            onEvent = {}, onError = { failed.complete(it) }, onClosed = { failed.complete(IllegalStateException("closed")) },
        )
        assertEquals("服务器出错了（503），稍后再试", userMessage(failed.get(10, TimeUnit.SECONDS)))
    }
}
