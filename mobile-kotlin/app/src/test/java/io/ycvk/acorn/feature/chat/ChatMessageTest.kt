package io.ycvk.acorn.feature.chat

import io.ycvk.acorn.api.models.Message
import org.junit.Assert.assertTrue
import org.junit.Test

class ChatMessageTest {

    @Test
    fun `wake input is its own row, not the owner's bubble`() {
        assertTrue(chatMessageFrom(Message.Role.wake, "[commitment #1, made 2026-10-02 13:55] read X") is ChatMessage.Wake)
        assertTrue(chatMessageFrom(Message.Role.user, "remind me") is ChatMessage.User)
    }

    @Test
    fun `assistant, system and tool messages render as assistant`() {
        listOf(Message.Role.assistant, Message.Role.system, Message.Role.tool).forEach { role ->
            assertTrue(chatMessageFrom(role, "x") is ChatMessage.Assistant)
        }
    }
}
