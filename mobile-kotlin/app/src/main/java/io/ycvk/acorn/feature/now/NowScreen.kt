package io.ycvk.acorn.feature.now

import androidx.compose.foundation.LocalIndication
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyItemScope
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.NowBriefing
import io.ycvk.acorn.api.models.NowBriefingStory
import io.ycvk.acorn.api.models.NowCommitment
import io.ycvk.acorn.api.models.NowConcern
import io.ycvk.acorn.api.models.NowFollowUp
import io.ycvk.acorn.api.models.NowGoal
import io.ycvk.acorn.api.models.NowPanel
import io.ycvk.acorn.api.models.NowResponse
import io.ycvk.acorn.api.models.NowStoryPiece
import io.ycvk.acorn.api.models.NowWatch
import io.ycvk.acorn.api.models.PendingActionSummary
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornDialog
import io.ycvk.acorn.core.ui.AcornIcons
import io.ycvk.acorn.core.ui.AcornMenu
import io.ycvk.acorn.core.ui.AcornMenuItem
import io.ycvk.acorn.core.ui.EmptyState
import io.ycvk.acorn.core.ui.ErrorState
import io.ycvk.acorn.core.ui.Hairline
import io.ycvk.acorn.core.ui.IconAction
import io.ycvk.acorn.core.ui.InlineError
import io.ycvk.acorn.core.ui.ListRow
import io.ycvk.acorn.core.ui.Load
import io.ycvk.acorn.core.ui.OnReturnToForeground
import io.ycvk.acorn.core.ui.OrbState
import io.ycvk.acorn.core.ui.Panel
import io.ycvk.acorn.core.ui.ScreenPadding
import io.ycvk.acorn.core.ui.SectionLabel
import io.ycvk.acorn.core.ui.SkeletonRows
import io.ycvk.acorn.core.ui.Status
import io.ycvk.acorn.core.ui.StatusGlyph
import io.ycvk.acorn.core.ui.Tag
import io.ycvk.acorn.core.ui.ThinkingOrb
import io.ycvk.acorn.core.ui.ago
import io.ycvk.acorn.core.ui.groupItem
import io.ycvk.acorn.core.ui.plainSnippet
import io.ycvk.acorn.core.ui.pressScale
import io.ycvk.acorn.core.ui.raised
import io.ycvk.acorn.core.ui.weekdayName
import io.ycvk.acorn.feature.approvals.ApprovalCard
import io.ycvk.acorn.feature.approvals.ApprovalsViewModel
import io.ycvk.acorn.feature.approvals.PendingActionSheet
import io.ycvk.acorn.feature.views.ViewActions
import io.ycvk.acorn.feature.views.ViewCard
import java.time.LocalDate
import java.time.LocalTime
import java.time.OffsetDateTime
import java.time.ZoneId

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NowScreen(
    viewModel: NowViewModel,
    onThreadClick: (String) -> Unit,
    onStoryClick: (Long) -> Unit,
    onNewChat: () -> Unit,
    onSettings: () -> Unit,
    onBrowser: () -> Unit,
    bottomInset: Dp,
    approvals: ApprovalsViewModel = hiltViewModel(),
) {
    val load by viewModel.load.collectAsStateWithLifecycle()
    val refreshing by viewModel.refreshing.collectAsStateWithLifecycle()
    val actionError by viewModel.actionError.collectAsStateWithLifecycle()
    val pending by approvals.pendingActions.collectAsStateWithLifecycle()
    val deciding by approvals.deciding.collectAsStateWithLifecycle()
    val selected by approvals.selectedAction.collectAsStateWithLifecycle()
    val approvalError by approvals.error.collectAsStateWithLifecycle()
    var cancelling by remember { mutableStateOf<NowCommitment?>(null) }
    var stoppingGoal by remember { mutableStateOf<NowGoal?>(null) }
    var cancellingFollowUp by remember { mutableStateOf<NowFollowUp?>(null) }

    val refresh = {
        viewModel.refresh()
        approvals.loadPendingActions()
    }
    LaunchedEffect(Unit) { refresh() }
    OnReturnToForeground(refresh)
    RefreshWhilePiecesAreWritten(load, viewModel::refresh)

    val zone = ZoneId.systemDefault()
    val today = LocalDate.now(zone)

    Box(modifier = Modifier.fillMaxSize()) {
        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = refresh,
            modifier = Modifier.fillMaxSize(),
        ) {
            LazyColumn(
                modifier = Modifier.fillMaxSize(),
                contentPadding = PaddingValues(bottom = bottomInset + 96.dp),
            ) {
                item(key = "header") {
                    Hero(
                        greeting = greeting(LocalTime.now(zone)),
                        date = "${today.monthValue}月${today.dayOfMonth}日 · ${weekdayName(today.dayOfWeek)}",
                        onBrowser = onBrowser,
                        onSettings = onSettings,
                    )
                }
                listOfNotNull(actionError, approvalError).forEach { message ->
                    item(key = "error:$message") {
                        InlineError(
                            message,
                            modifier = Modifier.padding(horizontal = ScreenPadding, vertical = 4.dp),
                            action = "知道了",
                            onAction = {
                                viewModel.dismissActionError()
                                approvals.dismissError()
                            },
                        )
                    }
                }
                if (pending.isNotEmpty()) {
                    item(key = "pending:label") { SectionLabel("等你确认", count = pending.size) }
                    items(pending, key = { "pending:${it.actionId}" }) { action ->
                        ApprovalCard(
                            action = action,
                            deciding = deciding == action.actionId,
                            onDecide = { approvals.decideAction(action.actionId, it) },
                            onOpen = { approvals.loadActionDetail(action.actionId) },
                            modifier = Modifier
                                .animateItem()
                                .padding(horizontal = ScreenPadding, vertical = 8.dp),
                        )
                    }
                }
                when (val state = load) {
                    Load.Loading -> item(key = "loading") { SkeletonRows(rows = 4) }
                    is Load.Failed -> item(key = "failed") { ErrorState(state.message, onRetry = refresh) }
                    is Load.Loaded -> nowSections(
                        now = state.value,
                        pending = pending,
                        today = today,
                        zone = zone,
                        profile = viewModel.connection(),
                        onThreadClick = onThreadClick,
                        onStoryClick = onStoryClick,
                        onUnpin = viewModel::unpinView,
                        onCancel = { cancelling = it },
                        onStopGoal = { stoppingGoal = it },
                        onCancelFollowUp = { cancellingFollowUp = it },
                        onPause = viewModel::pauseWatch,
                        onResume = viewModel::resumeWatch,
                    )
                }
            }
        }
        AskBar("问点什么，或者交给 Acorn 去做", onClick = onNewChat, modifier = Modifier.align(Alignment.BottomCenter).padding(bottom = bottomInset))
    }

    selected?.let { detail ->
        PendingActionSheet(
            detail = detail,
            deciding = deciding == detail.actionId,
            error = approvalError,
            onDismiss = approvals::clearSelection,
            onDismissError = approvals::dismissError,
            onDecide = { approvals.decideAction(detail.actionId, it) },
        )
    }

    cancelling?.let { commitment ->
        AcornDialog(
            title = "取消这个约定？",
            body = "「${commitment.content}」以及之后的提醒都会取消。",
            confirm = "取消约定",
            dismiss = "保留",
            destructive = true,
            onConfirm = {
                cancelling = null
                viewModel.cancelCommitment(commitment.id)
            },
            onDismiss = { cancelling = null },
        )
    }

    stoppingGoal?.let { goal ->
        AcornDialog(
            title = "停止这个目标？",
            body = "Acorn 不会再为「${goal.objective}」接着跑，已经做完的留在对话里。",
            confirm = "停止目标",
            dismiss = "保留",
            destructive = true,
            onConfirm = {
                stoppingGoal = null
                viewModel.stopGoal(goal.id)
            },
            onDismiss = { stoppingGoal = null },
        )
    }

    cancellingFollowUp?.let { followUp ->
        AcornDialog(
            title = "取消这次等待？",
            body = "Acorn 不会再为「${followUp.task}」醒来。",
            confirm = "取消等待",
            dismiss = "保留",
            destructive = true,
            onConfirm = {
                cancellingFollowUp = null
                viewModel.cancelFollowUp(followUp.id)
            },
            onDismiss = { cancellingFollowUp = null },
        )
    }
}

/** The page's opening: the date and round actions, then the greeting beside Acorn's orb, listening. */
@Composable
private fun Hero(greeting: String, date: String, onBrowser: () -> Unit, onSettings: () -> Unit) {
    val colors = AcornTheme.colors
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .statusBarsPadding()
            .padding(start = ScreenPadding + 4.dp, end = ScreenPadding - 2.dp, bottom = 8.dp),
    ) {
        Row(modifier = Modifier.fillMaxWidth().height(60.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(date, style = AcornTheme.type.eyebrow, color = colors.inkSoft, modifier = Modifier.weight(1f))
            IconAction(AcornIcons.Globe, "Agent 的浏览器", onBrowser, filled = true)
            Spacer(Modifier.width(8.dp))
            IconAction(AcornIcons.Settings, "设置", onSettings, filled = true)
        }
        Row(modifier = Modifier.padding(top = 2.dp, end = 4.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(greeting, style = AcornTheme.type.display, color = colors.ink, modifier = Modifier.weight(1f))
            ThinkingOrb(OrbState.Listening, size = 64.dp, speed = 0.35f)
        }
    }
}

fun greeting(now: LocalTime): String = when (now.hour) {
    in 5..10 -> "早上好"
    in 11..12 -> "中午好"
    in 13..17 -> "下午好"
    in 18..22 -> "晚上好"
    else -> "夜深了"
}

private fun LazyListScope.nowSections(
    now: NowResponse,
    pending: List<PendingActionSummary>,
    today: LocalDate,
    zone: ZoneId,
    profile: ConnectionProfile?,
    onThreadClick: (String) -> Unit,
    onStoryClick: (Long) -> Unit,
    onUnpin: (NowPanel) -> Unit,
    onCancel: (NowCommitment) -> Unit,
    onStopGoal: (NowGoal) -> Unit,
    onCancelFollowUp: (NowFollowUp) -> Unit,
    onPause: (Long) -> Unit,
    onResume: (Long) -> Unit,
) {
    val briefing = now.briefing
    val nothing = briefing == null && pending.isEmpty() && now.panels.isEmpty() && now.commitments.isEmpty() &&
        now.concerns.isEmpty() && now.goals.isEmpty() && now.followUps.isEmpty() && now.watches.isEmpty()
    if (nothing) {
        item(key = "empty") {
            EmptyState(
                icon = AcornIcons.Today,
                title = "今天很清静",
                body = "约定、目标、追踪和每天的简报会出现在这里。想让 Acorn 做什么，直接在下面告诉它。",
            )
        }
        return
    }

    if (briefing != null) {
        item(key = "briefing:label") { SectionLabel(if (briefing.date == today) "今日简报" else "最近的简报") }
        item(key = "briefing:header") { BriefingHeader(briefing, onClick = { onThreadClick(briefing.threadId) }) }
        items(focusStories(briefing.stories), key = { "story:${it.storyId}" }) { story ->
            FocusStoryCard(
                story,
                onClick = { onStoryClick(story.storyId) },
                modifier = Modifier.animateItem().padding(horizontal = ScreenPadding, vertical = 5.dp),
            )
        }
        briefStories(briefing.stories).takeIf { it.isNotEmpty() }?.let { briefs ->
            item(key = "briefing:briefs") {
                BriefsCard(briefs, onStoryClick, Modifier.animateItem().padding(horizontal = ScreenPadding, vertical = 5.dp))
            }
        }
        briefingPartPreviews(briefing).takeIf { it.isNotEmpty() }?.let { parts ->
            item(key = "briefing:parts") {
                BriefingPartsCard(parts, onClick = { onThreadClick(briefing.threadId) }, Modifier.animateItem().padding(horizontal = ScreenPadding, vertical = 5.dp))
            }
        }
    }

    if (now.panels.isNotEmpty()) {
        item(key = "panels:label") { SectionLabel("面板", count = now.panels.size) }
        items(now.panels, key = { "panel:${it.view.id}" }) { panel ->
            PanelCard(panel, profile, onOpen = { onThreadClick(panel.threadId) }, onUnpin = { onUnpin(panel) })
        }
    }

    if (now.commitments.isNotEmpty()) {
        item(key = "commitments:label") { SectionLabel("接下来", count = now.commitments.size) }
        itemsIndexed(now.commitments, key = { _, it -> "commitment:${it.id}" }) { index, commitment ->
            Column(Modifier.animateItem().groupItem(index, now.commitments.size)) {
                CommitmentRow(commitment, wakeTime(commitment.wakeAt, today, zone), onCancel = { onCancel(commitment) })
                if (index < now.commitments.lastIndex) Hairline(inset = ScreenPadding + 63.dp)
            }
        }
    }

    if (now.concerns.isNotEmpty()) {
        item(key = "concerns:label") { SectionLabel("正在跟进", count = now.concerns.size) }
        itemsIndexed(now.concerns, key = { _, it -> "concern:${it.id}" }) { index, concern ->
            Column(Modifier.animateItem().groupItem(index, now.concerns.size)) {
                ConcernRow(concern, onClick = { onThreadClick(concern.threadId) })
                if (index < now.concerns.lastIndex) Hairline(inset = ScreenPadding + 28.dp)
            }
        }
    }

    if (now.goals.isNotEmpty()) {
        item(key = "goals:label") { SectionLabel("目标", count = now.goals.size) }
        itemsIndexed(now.goals, key = { _, it -> "goal:${it.id}" }) { index, goal ->
            Column(Modifier.animateItem().groupItem(index, now.goals.size)) {
                GoalRow(goal, onClick = { onThreadClick(goal.threadId) }, onStop = { onStopGoal(goal) })
                if (index < now.goals.lastIndex) Hairline(inset = ScreenPadding + 28.dp)
            }
        }
    }

    if (now.followUps.isNotEmpty()) {
        item(key = "follow_ups:label") { SectionLabel("等结果", count = now.followUps.size) }
        itemsIndexed(now.followUps, key = { _, it -> "follow_up:${it.id}" }) { index, followUp ->
            Column(Modifier.animateItem().groupItem(index, now.followUps.size)) {
                FollowUpRow(
                    followUp,
                    deadline = followUpDeadline(followUp, today, zone),
                    onClick = { onThreadClick(followUp.threadId) },
                    onCancel = { onCancelFollowUp(followUp) },
                )
                if (index < now.followUps.lastIndex) Hairline(inset = ScreenPadding + 28.dp)
            }
        }
    }

    if (now.watches.isNotEmpty()) {
        item(key = "watches:label") { SectionLabel("追踪", count = now.watches.size) }
        itemsIndexed(now.watches, key = { _, it -> "watch:${it.id}" }) { index, watch ->
            Column(Modifier.animateItem().groupItem(index, now.watches.size)) {
                WatchRow(watch, onPause = { onPause(watch.id) }, onResume = { onResume(watch.id) })
                if (index < now.watches.lastIndex) Hairline(inset = ScreenPadding + 28.dp)
            }
        }
    }
}

/**
 * The line over a briefing's stories: the day it is for and when it was
 * written. A tap opens the briefing in its conversation.
 */
@Composable
private fun BriefingHeader(briefing: NowBriefing, onClick: () -> Unit) {
    val colors = AcornTheme.colors
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = ScreenPadding - 8.dp)
            .clip(AcornShapes.small)
            .clickable(onClickLabel = "阅读简报全文", onClick = onClick)
            .padding(horizontal = 12.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            "${briefing.date.monthValue}月${briefing.date.dayOfMonth}日 · ${ago(briefing.finishedAt, OffsetDateTime.now())}写好",
            style = AcornTheme.type.caption,
            color = colors.inkFaint,
            modifier = Modifier.weight(1f),
        )
        Text("阅读全文", style = AcornTheme.type.footnote.copy(fontWeight = FontWeight.Medium), color = colors.accent)
        Spacer(Modifier.width(2.dp))
        Icon(AcornIcons.ChevronRight, contentDescription = null, tint = colors.accent, modifier = Modifier.size(16.dp))
    }
}

/**
 * A story of today's focus: its section, the title and sentence the briefing
 * wrote, its sources and how far the piece about it is. A tap opens its
 * reading page.
 */
@Composable
private fun FocusStoryCard(story: NowBriefingStory, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    Panel(modifier = modifier, onClick = onClick, contentPadding = PaddingValues(18.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(story.section, style = AcornTheme.type.eyebrow, color = colors.accentInk, modifier = Modifier.weight(1f))
            StoryMarks(story)
        }
        Spacer(Modifier.height(6.dp))
        Text(
            story.title,
            style = AcornTheme.type.headline.copy(fontWeight = FontWeight.SemiBold),
            color = colors.ink,
            maxLines = 3,
            overflow = TextOverflow.Ellipsis,
        )
        story.summary?.takeIf { it.isNotBlank() }?.let {
            Spacer(Modifier.height(6.dp))
            Text(it, style = AcornTheme.type.callout, color = colors.inkSoft, maxLines = 4, overflow = TextOverflow.Ellipsis)
        }
        val meta = listOfNotNull(sourceLine(story.sources), pieceLabel(story)).joinToString(" · ")
        if (meta.isNotEmpty()) {
            Spacer(Modifier.height(10.dp))
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (story.piece?.status == NowStoryPiece.Status.writing) {
                    CircularProgressIndicator(color = colors.inkFaint, strokeWidth = 1.5.dp, modifier = Modifier.size(11.dp))
                    Spacer(Modifier.width(6.dp))
                }
                Text(meta, style = AcornTheme.type.caption, color = colors.inkFaint, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

/** The briefing's other stories, each with the sentence the briefing wrote and its sources. A tap on one opens its reading page. */
@Composable
private fun BriefsCard(stories: List<NowBriefingStory>, onStoryClick: (Long) -> Unit, modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    Panel(modifier = modifier, contentPadding = PaddingValues(vertical = 6.dp)) {
        Text("快讯", style = AcornTheme.type.eyebrow, color = colors.accentInk, modifier = Modifier.padding(start = 18.dp, top = 10.dp, bottom = 2.dp))
        stories.forEachIndexed { index, story ->
            BriefRow(story, onClick = { onStoryClick(story.storyId) })
            if (index < stories.lastIndex) Hairline(inset = 18.dp)
        }
    }
}

@Composable
private fun BriefRow(story: NowBriefingStory, onClick: () -> Unit) {
    val colors = AcornTheme.colors
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClickLabel = "打开这件事", onClick = onClick)
            .padding(horizontal = 18.dp, vertical = 12.dp),
    ) {
        Row(verticalAlignment = Alignment.Top) {
            Text(
                story.title,
                style = AcornTheme.type.callout.copy(fontWeight = FontWeight.Medium),
                color = if (story.reaction == NowBriefingStory.Reaction.less) colors.inkSoft else colors.ink,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            StoryMarks(story, Modifier.padding(start = 8.dp, top = 1.dp))
        }
        story.summary?.takeIf { it.isNotBlank() }?.let {
            Spacer(Modifier.height(3.dp))
            Text(it, style = AcornTheme.type.footnote, color = colors.inkSoft, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
        sourceLine(story.sources)?.let {
            Spacer(Modifier.height(4.dp))
            Text(it, style = AcornTheme.type.caption, color = colors.inkFaint, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

/** What the owner asked of a story: that Acorn follows it, and more or less of its kind. */
@Composable
private fun StoryMarks(story: NowBriefingStory, modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    if (!story.following && story.reaction == null) return
    Row(modifier = modifier, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        if (story.following) Tag("跟进中", color = colors.success)
        when (story.reaction) {
            NowBriefingStory.Reaction.more -> Tag("多报", color = colors.accentInk, container = colors.accentSoft)
            NowBriefingStory.Reaction.less -> Tag("少报")
            null -> Unit
        }
    }
}

/** The parts of a briefing beside its news, such as what is due today and what needs the owner. A tap opens the briefing. */
@Composable
private fun BriefingPartsCard(parts: List<Pair<String?, String>>, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    Panel(modifier = modifier, onClick = onClick, contentPadding = PaddingValues(18.dp)) {
        parts.forEachIndexed { index, (heading, text) ->
            if (index > 0) Spacer(Modifier.height(12.dp))
            heading?.let {
                Text(it, style = AcornTheme.type.eyebrow, color = colors.accentInk)
                Spacer(Modifier.height(2.dp))
            }
            Text(text, style = AcornTheme.type.callout, color = colors.inkSoft, maxLines = 3, overflow = TextOverflow.Ellipsis)
        }
    }
}

/** The parts of a briefing its card previews, as plain text, at most four. */
fun briefingPartPreviews(briefing: NowBriefing): List<Pair<String?, String>> =
    briefing.parts.map { it.heading to plainSnippet(it.body) }.filter { it.second.isNotEmpty() }.take(4)

/**
 * A view the owner pinned, drawn as in its conversation under the name of
 * that conversation and when it last changed. A tap anywhere on it, its
 * buttons and choices included, opens the conversation; the pin takes it off.
 */
@Composable
private fun LazyItemScope.PanelCard(panel: NowPanel, profile: ConnectionProfile?, onOpen: () -> Unit, onUnpin: () -> Unit) {
    val actions = ViewActions(
        idle = true,
        sending = null,
        picks = emptyMap(),
        onPick = { _, _ -> onOpen() },
        onSend = { onOpen() },
        pinning = null,
        onPin = { onUnpin() },
    )
    ViewCard(
        view = panel.view,
        threadId = panel.threadId,
        profile = profile,
        actions = actions,
        meta = panelMeta(panel, OffsetDateTime.now()),
        onOpen = onOpen,
        modifier = Modifier
            .animateItem()
            .padding(horizontal = ScreenPadding, vertical = 6.dp),
    )
}

@Composable
private fun CommitmentRow(commitment: NowCommitment, wake: WakeTime, onCancel: () -> Unit) {
    val colors = AcornTheme.colors
    var menu by remember { mutableStateOf(false) }
    ListRow(
        onLongClick = { menu = true },
        verticalAlignment = Alignment.Top,
        leading = {
            Column(modifier = Modifier.widthIn(min = 50.dp)) {
                Text(wake.time, style = AcornTheme.type.mono.copy(fontSize = 14.sp, fontWeight = FontWeight.Medium), color = if (commitment.state == NowCommitment.State.due) colors.warning else colors.ink)
                Text(wake.day, style = AcornTheme.type.caption, color = colors.inkFaint)
            }
        },
        trailing = { RowMenu(menu, { menu = it }) { MenuItem("取消约定", danger = true) { menu = false; onCancel() } } },
    ) {
        Text(commitment.content, style = AcornTheme.type.body, color = colors.ink)
        val tags = listOfNotNull(
            "重复".takeIf { commitment.recurrence != null },
            "等待结果".takeIf { commitment.occurrences.isNotEmpty() },
        )
        if (tags.isNotEmpty()) {
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                tags.forEach { tag ->
                    if (tag == "等待结果") Tag(tag, color = colors.warningInk, container = colors.warningSoft) else Tag(tag)
                }
            }
        }
    }
}

@Composable
private fun ConcernRow(concern: NowConcern, onClick: () -> Unit) {
    val colors = AcornTheme.colors
    val waiting = concern.state == NowConcern.State.waiting
    ListRow(
        onClick = onClick,
        verticalAlignment = Alignment.Top,
        leading = { StatusGlyph(if (waiting) Status.Waiting else Status.Running, Modifier.padding(top = 3.dp)) },
        trailing = if (waiting) ({ Tag("在等你", color = colors.warningInk, container = colors.warningSoft) }) else null,
    ) {
        Text(concern.title, style = AcornTheme.type.body, color = colors.ink)
        concern.outcome?.let {
            Text("成品：$it", style = AcornTheme.type.footnote, color = colors.inkSoft, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
        if (concern.reason.isNotBlank()) {
            Text(concern.reason, style = AcornTheme.type.footnote, color = colors.inkSoft, maxLines = 3, overflow = TextOverflow.Ellipsis)
        }
    }
}

/** A goal Acorn keeps working on: its objective, how many runs it took and its conversation. A tap opens the conversation. */
@Composable
private fun GoalRow(goal: NowGoal, onClick: () -> Unit, onStop: () -> Unit) {
    val colors = AcornTheme.colors
    var menu by remember { mutableStateOf(false) }
    ListRow(
        onClick = onClick,
        onLongClick = { menu = true },
        verticalAlignment = Alignment.Top,
        leading = { StatusGlyph(Status.Running, Modifier.padding(top = 3.dp)) },
        trailing = { RowMenu(menu, { menu = it }) { MenuItem("停止目标", danger = true) { menu = false; onStop() } } },
    ) {
        Text(goal.objective, style = AcornTheme.type.body, color = colors.ink, maxLines = 3, overflow = TextOverflow.Ellipsis)
        Text("${goalProgress(goal)} · ${goal.threadTitle}", style = AcornTheme.type.footnote, color = colors.inkSoft, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** A follow-up that wakes Acorn: its task, what it waits for, its deadline and its conversation. A tap opens the conversation. */
@Composable
private fun FollowUpRow(followUp: NowFollowUp, deadline: String, onClick: () -> Unit, onCancel: () -> Unit) {
    val colors = AcornTheme.colors
    var menu by remember { mutableStateOf(false) }
    ListRow(
        onClick = onClick,
        onLongClick = { menu = true },
        verticalAlignment = Alignment.Top,
        leading = { StatusGlyph(Status.Waiting, Modifier.padding(top = 3.dp)) },
        trailing = { RowMenu(menu, { menu = it }) { MenuItem("取消等待", danger = true) { menu = false; onCancel() } } },
    ) {
        Text(followUp.task, style = AcornTheme.type.body, color = colors.ink, maxLines = 3, overflow = TextOverflow.Ellipsis)
        Text(followUpCondition(followUp), style = AcornTheme.type.footnote, color = colors.inkSoft, maxLines = 2, overflow = TextOverflow.Ellipsis)
        Text("$deadline · ${followUp.threadTitle}", style = AcornTheme.type.footnote, color = colors.inkFaint, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

@Composable
private fun WatchRow(watch: NowWatch, onPause: () -> Unit, onResume: () -> Unit) {
    val colors = AcornTheme.colors
    var menu by remember { mutableStateOf(false) }
    ListRow(
        onLongClick = { menu = true },
        verticalAlignment = Alignment.Top,
        leading = {
            StatusGlyph(
                when (watch.status) {
                    NowWatch.Status.active -> Status.Running
                    NowWatch.Status.paused -> Status.Idle
                    NowWatch.Status.failing -> Status.Failed
                },
                Modifier.padding(top = 3.dp),
            )
        },
        trailing = {
            RowMenu(menu, { menu = it }) {
                when (watch.status) {
                    NowWatch.Status.paused -> MenuItem("恢复追踪") { menu = false; onResume() }
                    NowWatch.Status.failing -> {
                        MenuItem("立即重试") { menu = false; onResume() }
                        MenuItem("暂停追踪") { menu = false; onPause() }
                    }
                    NowWatch.Status.active -> MenuItem("暂停追踪") { menu = false; onPause() }
                }
            }
        },
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(watch.name, style = AcornTheme.type.body, color = colors.ink, modifier = Modifier.weight(1f, fill = false))
            when (watch.status) {
                NowWatch.Status.paused -> { Spacer(Modifier.width(8.dp)); Tag("已暂停") }
                NowWatch.Status.failing -> { Spacer(Modifier.width(8.dp)); Tag("出错", color = colors.danger, container = colors.dangerSoft) }
                NowWatch.Status.active -> Unit
            }
        }
        val meta = listOfNotNull(
            watchKindLabel(watch.kind),
            watchModeLabel(watch),
            if (watch.issue == true) "期刊" else null,
            intervalLabel(watch.intervalSeconds),
            watch.lastCheckedAt?.let { "${ago(it, OffsetDateTime.now())}检查" },
        ).joinToString(" · ")
        Text(meta, style = AcornTheme.type.footnote, color = colors.inkSoft)
        if (watch.status == NowWatch.Status.failing) {
            watch.lastError?.let {
                Text(it, style = AcornTheme.type.footnote, color = colors.danger, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

/** What happens with a watch's new items; a news source names its section. */
fun watchModeLabel(watch: NowWatch): String = when (watch.mode) {
    NowWatch.Mode.immediate -> "即时提醒"
    NowWatch.Mode.digest -> "汇总进简报"
    NowWatch.Mode.news -> (if (watch.trial == true) "试用" else "新闻") + (watch.section?.let { "：$it" } ?: "")
}

fun watchKindLabel(kind: NowWatch.Kind): String = when (kind) {
    NowWatch.Kind.rss -> "RSS"
    NowWatch.Kind.github -> "GitHub"
    NowWatch.Kind.web -> "网页"
    NowWatch.Kind.web_rendered -> "动态网页"
    NowWatch.Kind.trigger -> "脚本"
}

@Composable
private fun RowMenu(open: Boolean, onOpen: (Boolean) -> Unit, items: @Composable () -> Unit) {
    Box {
        IconAction(AcornIcons.More, "更多", { onOpen(true) }, tint = AcornTheme.colors.inkFaint)
        AcornMenu(expanded = open, onDismiss = { onOpen(false) }) { items() }
    }
}

@Composable
private fun MenuItem(text: String, danger: Boolean = false, onClick: () -> Unit) {
    AcornMenuItem(text, onClick, danger = danger)
}

/** The entry to a conversation, a pill floating over the foot of a page: a new one on the Today tab, a story's on its reading page. [busy] holds while the ask it sends is on its way. */
@Composable
internal fun AskBar(text: String, onClick: () -> Unit, modifier: Modifier = Modifier, busy: Boolean = false) {
    val colors = AcornTheme.colors
    val shape = AcornShapes.pill
    val interaction = remember { MutableInteractionSource() }
    Box(
        modifier = modifier
            .fillMaxWidth()
            .background(Brush.verticalGradient(listOf(Color.Transparent, colors.canvas.copy(alpha = 0.92f), colors.canvas)))
            .padding(start = ScreenPadding, end = ScreenPadding, top = 28.dp, bottom = 14.dp),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .pressScale(interaction, 0.98f)
                .raised(shape)
                .height(54.dp)
                .clip(shape)
                .background(colors.surface)
                .clickable(interaction, LocalIndication.current, enabled = !busy, onClick = onClick)
                .padding(start = 16.dp, end = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            ThinkingOrb(OrbState.Thinking, size = 22.dp, animated = false)
            Spacer(Modifier.width(12.dp))
            Text(text, style = AcornTheme.type.callout, color = colors.inkFaint, modifier = Modifier.weight(1f))
            Box(
                modifier = Modifier.size(38.dp).background(colors.surfaceMuted, CircleShape),
                contentAlignment = Alignment.Center,
            ) {
                if (busy) {
                    CircularProgressIndicator(color = colors.inkSoft, strokeWidth = 2.dp, modifier = Modifier.size(16.dp))
                } else {
                    Icon(AcornIcons.ArrowUp, contentDescription = null, tint = colors.inkSoft, modifier = Modifier.size(17.dp))
                }
            }
        }
    }
}
