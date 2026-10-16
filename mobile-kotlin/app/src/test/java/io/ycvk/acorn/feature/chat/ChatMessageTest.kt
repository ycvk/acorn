package io.ycvk.acorn.feature.chat

import io.ycvk.acorn.api.models.Message
import io.ycvk.acorn.api.models.ThreadFile
import io.ycvk.acorn.data.repository.photoName
import java.time.OffsetDateTime
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
    fun `capture input becomes a card without the header and with its image path`() {
        val input = "[capture] shared from the owner's phone\nSubject: Tokio 2.0\nLink: https://tokio.rs\nImage: attachments/2026/10/ab12.png (image/png, 1.2 MiB)"
        val message = chatMessageFrom(Message.Role.capture, input) as ChatMessage.Capture
        assertEquals("Subject: Tokio 2.0\nLink: https://tokio.rs", message.text)
        assertEquals("attachments/2026/10/ab12.png", message.image)
        val textOnly = chatMessageFrom(Message.Role.capture, "[capture] shared from the owner's phone\nText:\nhello") as ChatMessage.Capture
        assertEquals("Text:\nhello", textOnly.text)
        assertEquals(null, textOnly.image)
    }

    @Test
    fun `assistant, system and tool messages render as assistant`() {
        listOf(Message.Role.assistant, Message.Role.system, Message.Role.tool).forEach { role ->
            assertTrue(chatMessageFrom(role, "x") is ChatMessage.Assistant)
        }
    }

    @Test
    fun `files ride on the owner's message and the agent's reply`() {
        val book = ThreadFile("file_1", "红楼梦.epub", "application/epub+zip", 2_400_000, ThreadFile.Origin.agent, OffsetDateTime.parse("2026-10-17T08:00:00Z"))
        val photo = ThreadFile("file_2", "photo.jpg", "image/jpeg", 800_000, ThreadFile.Origin.owner, OffsetDateTime.parse("2026-10-17T08:00:00Z"))
        val reply = chatMessageFrom(Message.Role.assistant, "发给你了", files = listOf(book)) as ChatMessage.Assistant
        assertEquals(listOf(FileView("file_1", "红楼梦.epub", "application/epub+zip", 2_400_000)), reply.files)
        assertEquals(false, reply.files.single().isImage)
        val asked = chatMessageFrom(Message.Role.user, "", files = listOf(photo)) as ChatMessage.User
        assertTrue(asked.files.single().isImage)
    }

    @Test
    fun `sizes and photo names read naturally`() {
        assertEquals("2.3 MB", formatBytes(2_400_000))
        assertEquals("12 KB", formatBytes(12_345))
        assertEquals("512 B", formatBytes(512))
        assertEquals("", formatBytes(-1))
        assertEquals("IMG_0001.jpg", photoName("IMG_0001.HEIC"))
        assertEquals("截图.jpg", photoName("截图.png"))
        assertEquals("photo.jpg", photoName(".png"))
    }

    @org.junit.Test
    fun thinkingWakeSources() {
        org.junit.Assert.assertEquals(WakeSource.Night, wakeSource("[night 2026-10-09] night reflection"))
        org.junit.Assert.assertEquals(WakeSource.Wander, wakeSource("[wander 2026-10-09 15:00] idle time"))
    }
}
