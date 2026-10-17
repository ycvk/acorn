package io.ycvk.acorn.core.ui

import io.ycvk.acorn.api.infrastructure.ClientError
import io.ycvk.acorn.api.infrastructure.ClientException
import org.junit.Assert.assertEquals
import org.junit.Test

class ErrorsTest {
    private fun conflict(code: String) = ClientException(
        "conflict", 409,
        ClientError<Any>(body = """{"error":{"code":"$code","message":"m"}}""", statusCode = 409),
    )

    @Test
    fun aBusyThreadSaysToWaitForTheRun() {
        assertEquals("Acorn 还在这个对话里工作，等这一轮结束再删", userMessage(conflict("thread_busy")))
    }

    @Test
    fun anActionDecidedElsewhereSaysSo() {
        assertEquals("这件事已经处理过了", userMessage(conflict("pending_action_already_decided")))
    }

    @Test
    fun otherConflictsAskToRefresh() {
        assertEquals("内容刚被改过，刷新后再试", userMessage(conflict("memory_changed")))
    }
}
