package io.ycvk.acorn.feature.skills

import io.ycvk.acorn.api.models.SkillAuthor
import io.ycvk.acorn.api.models.SkillPackageServer
import io.ycvk.acorn.api.models.SkillPackageTrigger
import org.junit.Assert.assertEquals
import org.junit.Test
import java.time.OffsetDateTime
import java.time.ZoneOffset

class SkillLabelsTest {
    private val zone = ZoneOffset.ofHours(8)
    private val now = OffsetDateTime.parse("2026-10-18T09:00:00+08:00")

    @Test
    fun `a problem outranks retirement, which outranks eligibility`() {
        assertEquals(SkillStanding.Invalid, skillStanding(retired = true, eligible = false, problem = "parse skill: bad"))
        assertEquals(SkillStanding.Retired, skillStanding(retired = true, eligible = false, problem = null))
        assertEquals(SkillStanding.InUse, skillStanding(retired = false, eligible = true, problem = ""))
        assertEquals(SkillStanding.NotListed, skillStanding(retired = false, eligible = false, problem = null))
    }

    @Test
    fun `server reasons read as the owner's words`() {
        assertEquals("缺少密钥 GITHUB_TOKEN、GITHUB_ORG", disabledReasonLabel("missing_secrets:GITHUB_TOKEN,GITHUB_ORG"))
        assertEquals("缺少工具 shell", disabledReasonLabel("missing_required_tools:shell"))
        assertEquals("附带文件需要工作机", disabledReasonLabel("files_need_workstation"))
        assertEquals("something_new:x", disabledReasonLabel("something_new:x"))
    }

    @Test
    fun `a revision names who wrote it and when`() {
        assertEquals("第 4 版", skillRevisionTitle(4))
        assertEquals("Acorn 改的 · 2 小时前", skillRevisionMeta(SkillAuthor.agent, now.minusHours(2), now))
        assertEquals("你改的", authorLabel(SkillAuthor.owner))
        assertEquals("随版本发布", authorLabel(SkillAuthor.release))
    }

    @Test
    fun `server states say whether the package runs and why not`() {
        assertEquals("正在运行", serverStateLabel(SkillPackageServer.State.ready, null))
        assertEquals("启动失败：exit status 1", serverStateLabel(SkillPackageServer.State.failed, "exit status 1"))
        assertEquals("没在运行，下一次用到这个技能时启动", serverStateLabel(SkillPackageServer.State.stopped, null))
        assertEquals("工作机还在跑旧的一版，下一次用到这个技能时换成这一版", serverStateLabel(SkillPackageServer.State.stopped, "older revision"))
        assertEquals("技能已停用，不会运行", serverStateLabel(null, null))
    }

    @Test
    fun `trigger states give the next firing in the owner's day`() {
        val trigger = SkillPackageTrigger(name = "price", schedule = "*/30 * * * *", deliver = SkillPackageTrigger.Deliver.immediate, run = "scripts/check.sh")
        assertEquals("下次 今天 09:30", triggerStateLabel(trigger.copy(state = SkillPackageTrigger.State.scheduled, nextAt = now.plusMinutes(30)), now, zone))
        assertEquals(
            "连续失败 5 次，明天 08:00 再试：exit 1",
            triggerStateLabel(trigger.copy(state = SkillPackageTrigger.State.failing, nextAt = now.plusHours(23), failures = 5, lastError = "exit 1"), now, zone),
        )
        assertEquals("10月21日 09:00", nextTime(now.plusDays(3), now, zone))
        assertEquals("缺少密钥，不会运行", triggerStateLabel(trigger.copy(state = SkillPackageTrigger.State.missing_secrets), now, zone))
        assertEquals("技能已停用，不会运行", triggerStateLabel(trigger, now, zone))
    }

    @Test
    fun `files say their size and why their content is not shown`() {
        assertEquals("512 B", fileSize(512))
        assertEquals("2.0 KB", fileSize(2048))
        assertEquals("1.5 MB", fileSize(1024L * 1024 * 3 / 2))
        assertEquals("不是文本文件，手机上不显示内容", hiddenFileReason(100))
        assertEquals("超过 64 KB，手机上不显示内容", hiddenFileReason(MAX_SHOWN_FILE_BYTES + 1))
    }
}
