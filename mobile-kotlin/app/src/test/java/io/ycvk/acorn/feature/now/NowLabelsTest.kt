package io.ycvk.acorn.feature.now

import io.ycvk.acorn.api.models.NowBriefing
import io.ycvk.acorn.api.models.NowBriefingStory
import io.ycvk.acorn.api.models.NowFollowUp
import io.ycvk.acorn.api.models.NowGoal
import io.ycvk.acorn.api.models.NowPanel
import io.ycvk.acorn.api.models.NowResponse
import io.ycvk.acorn.api.models.NowStoryPiece
import io.ycvk.acorn.api.models.NowWatch
import io.ycvk.acorn.api.models.View
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.ZoneId

class NowLabelsTest {
    private val shanghai = ZoneId.of("Asia/Shanghai")
    private val today = LocalDate.of(2026, 10, 11)

    @Test
    fun `wake time names today and tomorrow in the phone's zone`() {
        assertEquals(WakeTime("今天", "09:00"), wakeTime(OffsetDateTime.parse("2026-10-11T01:00:00Z"), today, shanghai))
        assertEquals(WakeTime("明天", "07:30"), wakeTime(OffsetDateTime.parse("2026-10-11T23:30:00Z"), today, shanghai))
        assertEquals(WakeTime("10月14日", "09:00"), wakeTime(OffsetDateTime.parse("2026-10-14T01:00:00Z"), today, shanghai))
        assertEquals(WakeTime("10月10日", "23:00"), wakeTime(OffsetDateTime.parse("2026-10-10T15:00:00Z"), today, shanghai))
    }

    @Test
    fun `a goal shows the runs it took of the most it may take`() {
        val goal = NowGoal(id = 3, threadId = "thread_books", threadTitle = "整理 Go 书", objective = "下完整本书", runs = 4, maxRuns = 10, createdAt = OffsetDateTime.parse("2026-10-11T01:00:00Z"))
        assertEquals("已用 4/10 轮", goalProgress(goal))
    }

    @Test
    fun `a follow-up says what it waits for and when it wakes regardless`() {
        val followUp = NowFollowUp(
            id = 7, threadId = "thread_build", threadTitle = "编译 acorn", task = "编译完看结果", waitsFor = NowFollowUp.WaitsFor.job_end,
            deadlineAt = OffsetDateTime.parse("2026-10-12T06:00:00Z"), createdAt = OffsetDateTime.parse("2026-10-11T01:00:00Z"), jobId = "job-0a1b2c3d",
        )
        assertEquals("等任务 job-0a1b2c3d 结束", followUpCondition(followUp))
        assertEquals("等任务 job-0a1b2c3d 输出「50%」", followUpCondition(followUp.copy(waitsFor = NowFollowUp.WaitsFor.job_output, outputContains = "50%")))
        assertEquals("等 books/go.epub 就绪", followUpCondition(followUp.copy(waitsFor = NowFollowUp.WaitsFor.path, jobId = null, path = "books/go.epub")))
        assertEquals("最晚明天 14:00", followUpDeadline(followUp, today, shanghai))
    }

    @Test
    fun `a panel names its conversation and when its content last changed`() {
        val view = View(
            id = "view_1",
            title = "eino 最新版本",
            catalog = "acorn.views.v1",
            components = emptyList(),
            `data` = emptyMap(),
            createdAt = OffsetDateTime.parse("2026-10-21T01:00:00Z"),
        )
        val now = OffsetDateTime.parse("2026-10-21T09:00:00Z")
        val panel = NowPanel(threadId = "thread_repos", threadTitle = "仓库版本", view = view, updatedAt = now.minusMinutes(3))
        assertEquals("仓库版本 · 3 分钟前更新", panelMeta(panel, now))
        assertEquals("仓库版本 · 刚刚更新", panelMeta(panel.copy(updatedAt = now), now))
    }

    @Test
    fun `a news source names its section`() {
        val watch = NowWatch(
            id = 1, name = "AI225", kind = NowWatch.Kind.rss, target = "https://daily.ai225.com/rss.xml",
            mode = NowWatch.Mode.news, status = NowWatch.Status.active, intervalSeconds = 3600,
            nextCheckAt = OffsetDateTime.parse("2026-10-22T01:00:00Z"), failures = 0, section = "AI 与科技前沿", issue = true,
        )
        assertEquals("新闻：AI 与科技前沿", watchModeLabel(watch))
        assertEquals("试用：AI 与科技前沿", watchModeLabel(watch.copy(trial = true)))
        assertEquals("新闻", watchModeLabel(watch.copy(section = null)))
        assertEquals("汇总进简报", watchModeLabel(watch.copy(mode = NowWatch.Mode.digest, section = null)))
        assertEquals("即时提醒", watchModeLabel(watch.copy(mode = NowWatch.Mode.immediate, section = null)))
        assertEquals("脚本", watchKindLabel(NowWatch.Kind.trigger))
    }

    @Test
    fun `interval label uses the largest whole unit`() {
        assertEquals("每 30 分钟", intervalLabel(1_800))
        assertEquals("每 2 小时", intervalLabel(7_200))
        assertEquals("每天", intervalLabel(86_400))
        assertEquals("每 2 天", intervalLabel(172_800))
        assertEquals("每 90 分钟", intervalLabel(5_400))
    }

    private fun story(id: Long, section: String, focus: Boolean = false, reaction: NowBriefingStory.Reaction? = null) =
        NowBriefingStory(storyId = id, section = section, title = "事件 $id", url = "https://x.test/$id", sources = emptyList(), focus = focus, following = false, reaction = reaction)

    @Test
    fun `choosing the current reaction again takes it back`() {
        val more = NowBriefingStory.Reaction.more
        val less = NowBriefingStory.Reaction.less
        assertEquals(less, nextReaction(null, less))
        assertEquals(more, nextReaction(less, more))
        assertNull(nextReaction(less, less))
    }

    @Test
    fun `today's focus gets cards and the other stories are briefs, each in the briefing's order`() {
        val stories = listOf(story(1, "国际"), story(2, "AI", focus = true), story(3, "国际"), story(4, "国内", focus = true))
        assertEquals(listOf(stories[1], stories[3]), focusStories(stories))
        assertEquals(listOf(stories[0], stories[2]), briefStories(stories))
    }

    @Test
    fun `a story names up to three sources and counts the rest`() {
        assertNull(sourceLine(emptyList()))
        assertEquals("中新网要闻", sourceLine(listOf("中新网要闻")))
        assertEquals("中新网要闻 · BBC 中文 · FT 中文网", sourceLine(listOf("中新网要闻", "BBC 中文", "FT 中文网")))
        assertEquals("中新网要闻 · BBC 中文 等 4 个来源", sourceLine(listOf("中新网要闻", "BBC 中文", "FT 中文网", "知乎热榜")))
    }

    @Test
    fun `a card says how long its piece reads or that it is being written`() {
        assertEquals("约 1 分钟读完", readingTime("短"))
        assertEquals("约 1 分钟读完", readingTime("字".repeat(400)))
        assertEquals("约 2 分钟读完", readingTime("字".repeat(401) + "\n\n  "))
        val focus = story(2, "AI", focus = true)
        assertNull(pieceLabel(focus))
        assertEquals("正在写", pieceLabel(focus.copy(piece = NowStoryPiece(status = NowStoryPiece.Status.writing))))
        assertEquals("约 2 分钟读完", pieceLabel(focus.copy(piece = NowStoryPiece(status = NowStoryPiece.Status.ready, body = "字".repeat(800)))))
        assertNull(pieceLabel(focus.copy(piece = NowStoryPiece(status = NowStoryPiece.Status.failed))))
    }

    @Test
    fun `the page is loaded again only while a piece is being written`() {
        fun page(vararg stories: NowBriefingStory) = NowResponse(
            panels = emptyList(), commitments = emptyList(), concerns = emptyList(), goals = emptyList(), followUps = emptyList(), watches = emptyList(),
            briefing = NowBriefing(
                threadId = "thread_briefings", runId = "run_b", date = today, body = "", finishedAt = OffsetDateTime.parse("2026-10-11T00:55:00Z"),
                stories = stories.toList(), parts = emptyList(),
            ),
        )
        val focus = story(2, "AI", focus = true)
        assertFalse(writingPiece(null))
        assertFalse(writingPiece(page(story(1, "国际"), focus.copy(piece = NowStoryPiece(status = NowStoryPiece.Status.ready, body = "写好了")))))
        assertTrue(writingPiece(page(story(1, "国际"), focus.copy(piece = NowStoryPiece(status = NowStoryPiece.Status.writing)))))
    }

    @Test
    fun `a story's page leads into its conversation or asks Acorn to explain it`() {
        val brief = story(1, "国际")
        assertEquals("让 Acorn 展开讲讲", storyAskLabel(brief))
        assertEquals("接着问这件事", storyAskLabel(brief.copy(threadId = "thread_story")))
    }

    @Test
    fun `a source link is named by its host`() {
        assertEquals("chinanews.com.cn", sourceHost("https://www.chinanews.com.cn/sh/2026/11-09/10728761.shtml"))
        assertEquals("daily.ai225.com", sourceHost("https://daily.ai225.com/daily/2026-11-10"))
        assertEquals("not a link", sourceHost("not a link"))
    }
}
