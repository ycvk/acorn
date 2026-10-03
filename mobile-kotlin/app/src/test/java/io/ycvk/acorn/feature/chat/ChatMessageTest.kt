package io.ycvk.acorn.feature.chat

import io.ycvk.acorn.api.models.Message
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ChatMessageTest {

    @Test
    fun `wake input is its own row, not the owner's bubble`() {
        assertTrue(chatMessageFrom(Message.Role.wake, "[commitment #1, made 2026-10-02 13:55] read X") is ChatMessage.Wake)
        assertTrue(chatMessageFrom(Message.Role.user, "remind me") is ChatMessage.User)
    }

    @Test
    fun `wake cards name what woke the run`() {
        assertEquals(WakeSource.Commitment, (chatMessageFrom(Message.Role.wake, "[commitment #1, made 2026-10-02 13:55] read X") as ChatMessage.Wake).source)
        assertEquals(WakeSource.Watch, (chatMessageFrom(Message.Role.wake, "[watch #2 phone price] 1 new") as ChatMessage.Wake).source)
        assertEquals(WakeSource.Briefing, (chatMessageFrom(Message.Role.wake, "[briefing 2026-10-04] morning briefing") as ChatMessage.Wake).source)
        assertEquals(WakeSource.Commitment, wakeSource("anything else"))
    }

    @Test
    fun `capture input becomes a card without the header and with file names`() {
        val input = "[capture] shared from the owner's phone\nSubject: Tokio 2.0\nLink: https://tokio.rs\nImage: attachments/2026/10/ab12.png (image/png, 1.2 MiB)"
        val message = chatMessageFrom(Message.Role.capture, input)
        assertTrue(message is ChatMessage.Capture)
        assertEquals("Subject: Tokio 2.0\nLink: https://tokio.rs\nImage: ab12.png", (message as ChatMessage.Capture).text)
    }

    @Test
    fun `assistant, system and tool messages render as assistant`() {
        listOf(Message.Role.assistant, Message.Role.system, Message.Role.tool).forEach { role ->
            assertTrue(chatMessageFrom(role, "x") is ChatMessage.Assistant)
        }
    }
}
