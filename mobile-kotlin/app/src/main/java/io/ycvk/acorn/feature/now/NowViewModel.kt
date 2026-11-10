package io.ycvk.acorn.feature.now

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.models.NowBriefingStory
import io.ycvk.acorn.api.models.NowFollowUp
import io.ycvk.acorn.api.models.NowGoal
import io.ycvk.acorn.api.models.NowPanel
import io.ycvk.acorn.api.models.NowResponse
import io.ycvk.acorn.api.models.NowStoryPiece
import io.ycvk.acorn.api.models.StoryMessageResponse
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.ui.Load
import io.ycvk.acorn.core.ui.ago
import io.ycvk.acorn.core.ui.clockTime
import io.ycvk.acorn.core.ui.dayLabel
import io.ycvk.acorn.core.ui.loadOf
import io.ycvk.acorn.core.ui.userMessage
import io.ycvk.acorn.data.repository.NowRepository
import io.ycvk.acorn.feature.live.RunFollower
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.ZoneId
import javax.inject.Inject

/** Backs the Today tab. Every change is followed by a fresh load; nothing is changed locally. */
@HiltViewModel
class NowViewModel @Inject constructor(
    private val authController: AuthController,
    private val repository: NowRepository,
    private val follower: RunFollower,
) : ViewModel() {

    private val _load = MutableStateFlow<Load<NowResponse>>(Load.Loading)
    val load: StateFlow<Load<NowResponse>> = _load.asStateFlow()

    private val _refreshing = MutableStateFlow(false)
    val refreshing: StateFlow<Boolean> = _refreshing.asStateFlow()

    /** The last failed change, shown until dismissed or the next change. */
    private val _actionError = MutableStateFlow<String?>(null)
    val actionError: StateFlow<String?> = _actionError.asStateFlow()

    /** The story the owner just asked about, until its message is sent; its row takes no other tap meanwhile. */
    private val _askingStory = MutableStateFlow<Long?>(null)
    val askingStory: StateFlow<Long?> = _askingStory.asStateFlow()

    /** Conversations to open: a story's, once it exists. */
    private val _openThread = MutableSharedFlow<String>(extraBufferCapacity = 1)
    val openThread: SharedFlow<String> = _openThread.asSharedFlow()

    private var loadJob: Job? = null

    fun refresh() {
        val profile = profile() ?: run {
            _load.value = Load.Failed("还没有连接服务器")
            return
        }
        loadJob?.cancel()
        loadJob = viewModelScope.launch { reload(profile) }
    }

    fun cancelCommitment(id: Long) = change { repository.cancelCommitment(it, id) }

    fun stopGoal(id: Long) = change { repository.stopGoal(it, id) }

    fun cancelFollowUp(id: Long) = change { repository.cancelFollowUp(it, id) }

    fun pauseWatch(id: Long) = change { repository.pauseWatch(it, id) }

    fun resumeWatch(id: Long) = change { repository.resumeWatch(it, id) }

    fun unpinView(panel: NowPanel) = change { repository.unpinView(it, panel.threadId, panel.view.id) }

    /** Marks a story more or less of what the owner wants; choosing its current reaction again takes it back. */
    fun react(story: NowBriefingStory, chosen: NowBriefingStory.Reaction) = change {
        when (val next = nextReaction(story.reaction, chosen)) {
            null -> repository.clearStoryReaction(it, story.storyId)
            else -> repository.reactToStory(it, story.storyId, next)
        }
    }

    /** Opens a story's conversation; the first time, asks Acorn there to explain the story. */
    fun openStory(story: NowBriefingStory) {
        val threadId = story.threadId
        if (threadId != null) {
            _openThread.tryEmit(threadId)
            return
        }
        ask(story) { repository.discussStory(it, story.storyId) }
    }

    /** Asks Acorn to keep following a story, in the story's conversation. */
    fun followStory(story: NowBriefingStory) = ask(story) { repository.followStory(it, story.storyId) }

    /** Sends the owner's ask about a story, follows the run it starts and opens the story's conversation. */
    private fun ask(story: NowBriefingStory, send: (ConnectionProfile) -> StoryMessageResponse) {
        val profile = profile() ?: run {
            _actionError.value = "还没有连接服务器"
            return
        }
        if (_askingStory.value != null) return
        _askingStory.value = story.storyId
        _actionError.value = null
        val sendKey = follower.beginSend(story.title)
        viewModelScope.launch {
            try {
                val sent = withContext(Dispatchers.IO) { send(profile) }
                follower.follow(profile, sent.run.id, sent.threadId, story.title, live = true, sendKey = sendKey)
                _openThread.emit(sent.threadId)
            } catch (e: CancellationException) {
                follower.endSend(sendKey)
                throw e
            } catch (e: Exception) {
                follower.endSend(sendKey)
                _actionError.value = userMessage(e)
            } finally {
                _askingStory.value = null
            }
            reload(profile)
        }
    }

    /** The paired server, for loading a panel's images. */
    fun connection(): ConnectionProfile? = profile()

    fun dismissActionError() {
        _actionError.value = null
    }

    private fun change(action: (ConnectionProfile) -> Unit) {
        val profile = profile() ?: run {
            _actionError.value = "还没有连接服务器"
            return
        }
        _actionError.value = null
        loadJob?.cancel()
        loadJob = viewModelScope.launch {
            try {
                withContext(Dispatchers.IO) { action(profile) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _actionError.value = userMessage(e)
            }
            reload(profile)
        }
    }

    private suspend fun reload(profile: ConnectionProfile) {
        _refreshing.value = true
        if (_load.value !is Load.Loaded) _load.value = Load.Loading
        try {
            _load.value = loadOf { repository.now(profile) }
        } finally {
            _refreshing.value = false
        }
    }

    private fun profile() = authController.profile
}

/** When a commitment wakes, in the phone's time zone: a day ("今天", "明天", "10月14日") and a time. */
data class WakeTime(val day: String, val time: String)

fun wakeTime(at: OffsetDateTime, today: LocalDate, zone: ZoneId): WakeTime =
    WakeTime(dayLabel(at.atZoneSameInstant(zone).toLocalDate(), today), clockTime(at, zone))

/** How far a goal got: the runs it took of the most it may take, "已用 4/10 轮". */
fun goalProgress(goal: NowGoal): String = "已用 ${goal.runs}/${goal.maxRuns} 轮"

/** What a follow-up waits for: a job to end or print a text, or a path to be ready. */
fun followUpCondition(followUp: NowFollowUp): String = when (followUp.waitsFor) {
    NowFollowUp.WaitsFor.job_end -> "等任务 ${followUp.jobId.orEmpty()} 结束"
    NowFollowUp.WaitsFor.job_output -> "等任务 ${followUp.jobId.orEmpty()} 输出「${followUp.outputContains.orEmpty()}」"
    NowFollowUp.WaitsFor.path -> "等 ${followUp.path.orEmpty()} 就绪"
}

/** When a follow-up wakes the agent regardless, in the phone's time zone: "最晚明天 14:00". */
fun followUpDeadline(followUp: NowFollowUp, today: LocalDate, zone: ZoneId): String {
    val at = wakeTime(followUp.deadlineAt, today, zone)
    return "最晚${at.day} ${at.time}"
}

/** Under a panel's title: its conversation and when its content last changed, "仓库版本 · 3 分钟前更新". */
fun panelMeta(panel: NowPanel, now: OffsetDateTime): String = "${panel.threadTitle} · ${ago(panel.updatedAt, now)}更新"

/** How often a watch is checked: "每 30 分钟", "每 2 小时", "每天". */
fun intervalLabel(seconds: Long): String = when {
    seconds == 86_400L -> "每天"
    seconds % 86_400 == 0L -> "每 ${seconds / 86_400} 天"
    seconds % 3_600 == 0L -> "每 ${seconds / 3_600} 小时"
    else -> "每 ${seconds / 60} 分钟"
}

/** The reaction after the owner chooses one: the same one again takes it back. */
fun nextReaction(current: NowBriefingStory.Reaction?, chosen: NowBriefingStory.Reaction): NowBriefingStory.Reaction? =
    if (current == chosen) null else chosen

/** The stories of today's focus, in the briefing's order; each gets a card of its own. */
fun focusStories(stories: List<NowBriefingStory>): List<NowBriefingStory> = stories.filter { it.focus }

/** The other stories a briefing picked, in its order; they are listed as briefs. */
fun briefStories(stories: List<NowBriefingStory>): List<NowBriefingStory> = stories.filterNot { it.focus }

/** A story's news sources in a line, "中新网要闻 · BBC 中文"; past three, the first two and how many more. Null without any. */
fun sourceLine(sources: List<String>): String? = when {
    sources.isEmpty() -> null
    sources.size <= 3 -> sources.joinToString(" · ")
    else -> sources.take(2).joinToString(" · ") + " 等 ${sources.size} 个来源"
}

/** How long a piece takes to read, at about 400 characters a minute and no less than one: "约 2 分钟读完". */
fun readingTime(body: String): String {
    val characters = body.count { !it.isWhitespace() }
    return "约 ${maxOf(1, (characters + 399) / 400)} 分钟读完"
}

/** What a story's card says of its piece: how long it reads, that it is being written, or nothing without one. */
fun pieceLabel(story: NowBriefingStory): String? = when (story.piece?.status) {
    NowStoryPiece.Status.ready -> story.piece.body?.let(::readingTime)
    NowStoryPiece.Status.writing -> "正在写"
    NowStoryPiece.Status.failed, null -> null
}

/** Whether the page's briefing has a piece that is still being written. */
fun writingPiece(now: NowResponse?): Boolean =
    now?.briefing?.stories.orEmpty().any { it.piece?.status == NowStoryPiece.Status.writing }

/** The entry from a story's reading page to its conversation: go on asking where it has one, else have Acorn explain it. */
fun storyAskLabel(story: NowBriefingStory): String = if (story.threadId != null) "接着问这件事" else "让 Acorn 展开讲讲"
