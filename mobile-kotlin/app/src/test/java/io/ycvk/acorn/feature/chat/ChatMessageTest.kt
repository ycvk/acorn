package io.ycvk.acorn.feature.chat

import io.ycvk.acorn.api.models.Message
import io.ycvk.acorn.api.models.ThreadFile
import io.ycvk.acorn.data.repository.FileView
import io.ycvk.acorn.data.repository.photoName
import java.time.OffsetDateTime
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
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
        assertEquals(WakeSource.FollowUp, wakeSource("[follow_up #3, set 2026-11-03 16:20] 下载完就整理\nWhat happened: job job-ab12cd34 exited with code 0"))
        assertEquals(WakeSource.Goal, wakeSource("[goal #2, run 1 of 10] Objective: 下完整本书"))
        assertEquals(WakeSource.Commitment, wakeSource("anything else"))
    }

    @Test
    fun `a goal's run shows its round and objective, and says when a restart cut the last one off`() {
        val tail = "Keep working toward the objective from where this conversation left off. When it is reached, call goal with done and what was achieved."
        val wake = chatMessageFrom(Message.Role.wake, "[goal #2, run 3 of 10] Objective: 下完整本书\n并整理进知识库\n$tail") as ChatMessage.Wake
        assertEquals(WakeSummary("继续目标 · 第 3/10 轮", "下完整本书\n并整理进知识库"), wake.summary)
        val restarted = "[goal #2, run 4 of 10] Objective: 下完整本书\nThe last run stopped when Acorn restarted; check what it left unfinished.\n$tail"
        assertEquals(WakeSummary("继续目标 · 第 4/10 轮", "下完整本书", "上一轮因 Acorn 重启中断"), wakeSummary(WakeSource.Goal, restarted))
        assertNull(wakeSummary(WakeSource.Goal, "[goal #2, run 4 of 10] something else"))
    }

    @Test
    fun `a follow-up's run shows its task and what happened`() {
        val wake = chatMessageFrom(Message.Role.wake, "[follow_up #3, set 2026-11-03 16:20] 下载完就整理\nWhat happened: job job-ab12cd34 exited with code 0") as ChatMessage.Wake
        assertEquals(WakeSummary("等到结果", "下载完就整理", "任务 job-ab12cd34 已结束，退出码 0"), wake.summary)
        assertNull(wakeSummary(WakeSource.FollowUp, "[follow_up #3] 下载完就整理"))
        assertNull((chatMessageFrom(Message.Role.wake, "[watch #2 phone price] 1 new") as ChatMessage.Wake).summary)
    }

    @Test
    fun `what woke a follow-up reads in Chinese, and as written when it is something else`() {
        mapOf(
            "job job-1 printed \"100%\"" to "任务 job-1 输出了「100%」",
            "job job-1 exited with code 2 without printing \"DONE\"" to "任务 job-1 已结束，退出码 2，没有输出「DONE」",
            "job job-1 is stopped: ran past 24h without printing \"DONE\"" to "任务 job-1 已停止：ran past 24h，没有输出「DONE」",
            "job job-1 is lost" to "任务 job-1 在工作机重启时中断",
            "job job-1 is no longer on the workstation" to "工作机上已没有任务 job-1",
            "directory books/go exists" to "目录 books/go 已出现",
            "books/go.epub is ready: 2048 bytes, unchanged since the previous check" to "books/go.epub 已就绪（2048 字节）",
            "its deadline 2026-11-04 16:20 passed; job job-1 is still running" to "到了期限 2026-11-04 16:20，任务 job-1 还在运行",
            "its deadline 2026-11-04 16:20 passed; job job-1 is still running and has not printed \"50%\"" to "到了期限 2026-11-04 16:20，任务 job-1 还在运行，还没输出「50%」",
            "its deadline 2026-11-04 16:20 passed; books/go.epub does not exist yet" to "到了期限 2026-11-04 16:20，books/go.epub 还不存在",
            "its deadline 2026-11-04 16:20 passed; books/go.epub is still being written (1024 bytes)" to "到了期限 2026-11-04 16:20，books/go.epub 还在写入（1024 字节）",
            "its deadline 2026-11-04 16:20 passed; checking failed: connection refused" to "到了期限 2026-11-04 16:20，检查失败：connection refused",
            "its deadline 2026-11-04 16:20 passed; no workstation is configured to check it" to "到了期限 2026-11-04 16:20，没有工作机可以检查",
            "something new" to "something new",
        ).forEach { (happened, said) -> assertEquals(happened, said, happenedText(happened)) }
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

    @Test
    fun `a stored message keeps its row key across reloads and edits`() {
        for (role in Message.Role.entries) {
            assertEquals("msg_1", chatMessageFrom(role, "x", id = "msg_1").id)
        }
        val reply = chatMessageFrom(Message.Role.assistant, "x", id = "msg_2") as ChatMessage.Assistant
        assertEquals("msg_2", reply.copy(text = "y").id)
    }
}
