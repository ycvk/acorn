package io.ycvk.acorn.feature.now

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowForward
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.Language
import androidx.compose.material.icons.outlined.MoreHoriz
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.WbSunny
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.NowBriefing
import io.ycvk.acorn.api.models.NowCommitment
import io.ycvk.acorn.api.models.NowConcern
import io.ycvk.acorn.api.models.NowResponse
import io.ycvk.acorn.api.models.NowWatch
import io.ycvk.acorn.api.models.PendingActionSummary
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.ErrorState
import io.ycvk.acorn.core.ui.EmptyState
import io.ycvk.acorn.core.ui.Hairline
import io.ycvk.acorn.core.ui.IconAction
import io.ycvk.acorn.core.ui.InlineError
import io.ycvk.acorn.core.ui.LargeHeader
import io.ycvk.acorn.core.ui.ListRow
import io.ycvk.acorn.core.ui.OnReturnToForeground
import io.ycvk.acorn.core.ui.Panel
import io.ycvk.acorn.core.ui.ScreenPadding
import io.ycvk.acorn.core.ui.SectionLabel
import io.ycvk.acorn.core.ui.SkeletonRows
import io.ycvk.acorn.core.ui.StatusDot
import io.ycvk.acorn.core.ui.Tag
import io.ycvk.acorn.core.ui.ago
import io.ycvk.acorn.core.ui.weekdayName
import io.ycvk.acorn.feature.approvals.ApprovalCard
import io.ycvk.acorn.feature.approvals.ApprovalsViewModel
import io.ycvk.acorn.feature.approvals.PendingActionSheet
import io.ycvk.acorn.feature.approvals.optionDecision
import java.time.LocalDate
import java.time.LocalTime
import java.time.OffsetDateTime
import java.time.ZoneId

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NowScreen(
    onThreadClick: (String) -> Unit,
    onNewChat: () -> Unit,
    onSettings: () -> Unit,
    onBrowser: () -> Unit,
    viewModel: NowViewModel = hiltViewModel(),
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

    val refresh = {
        viewModel.refresh()
        approvals.loadPendingActions()
    }
    LaunchedEffect(Unit) { refresh() }
    OnReturnToForeground(refresh)

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
                contentPadding = PaddingValues(bottom = 96.dp),
            ) {
                item(key = "header") {
                    LargeHeader(
                        title = greeting(LocalTime.now(zone)),
                        eyebrow = "${today.monthValue}月${today.dayOfMonth}日 ${weekdayName(today.dayOfWeek)}",
                    ) {
                        IconAction(Icons.Outlined.Language, "Agent 的浏览器", onBrowser, tint = AcornTheme.colors.inkSoft)
                        IconAction(Icons.Outlined.Settings, "设置", onSettings, tint = AcornTheme.colors.inkSoft)
                    }
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
                    item(key = "pending:label") { SectionLabel("等你确认 · ${pending.size}") }
                    items(pending, key = { "pending:${it.actionId}" }) { action ->
                        ApprovalCard(
                            action = action,
                            deciding = deciding == action.actionId,
                            onDecide = { option ->
                                approvals.decideAction(action.actionId, optionDecision(option), option.id, null)
                            },
                            onOpen = { approvals.loadActionDetail(action.actionId) },
                            modifier = Modifier
                                .padding(horizontal = ScreenPadding, vertical = 5.dp)
                                .animateItem(),
                        )
                    }
                }
                when (val state = load) {
                    NowLoad.Loading -> item(key = "loading") { SkeletonRows(rows = 4) }
                    is NowLoad.Failed -> item(key = "failed") { ErrorState(state.message, onRetry = refresh) }
                    is NowLoad.Loaded -> nowSections(
                        now = state.now,
                        briefingSections = state.briefing,
                        pending = pending,
                        today = today,
                        zone = zone,
                        onBriefingClick = onThreadClick,
                        onCancel = { cancelling = it },
                        onPause = viewModel::pauseWatch,
                        onResume = viewModel::resumeWatch,
                    )
                }
            }
        }
        AskBar(onClick = onNewChat, modifier = Modifier.align(Alignment.BottomCenter))
    }

    selected?.let { detail ->
        PendingActionSheet(
            detail = detail,
            deciding = deciding == detail.actionId,
            onDismiss = approvals::clearSelection,
            onDecide = { option -> approvals.decideAction(detail.actionId, optionDecision(option), option.id, null) },
        )
    }

    cancelling?.let { commitment ->
        AlertDialog(
            onDismissRequest = { cancelling = null },
            title = { Text("取消这个约定？") },
            text = { Text("「${commitment.content}」以及之后的提醒都会取消。") },
            confirmButton = {
                TextButton(onClick = {
                    cancelling = null
                    viewModel.cancelCommitment(commitment.id)
                }) { Text("取消约定", color = AcornTheme.colors.danger) }
            },
            dismissButton = { TextButton(onClick = { cancelling = null }) { Text("保留") } },
        )
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
    briefingSections: List<BriefingSection>,
    pending: List<PendingActionSummary>,
    today: LocalDate,
    zone: ZoneId,
    onBriefingClick: (String) -> Unit,
    onCancel: (NowCommitment) -> Unit,
    onPause: (Long) -> Unit,
    onResume: (Long) -> Unit,
) {
    val briefing = now.briefing
    val nothing = briefing == null && pending.isEmpty() && now.commitments.isEmpty() &&
        now.concerns.isEmpty() && now.watches.isEmpty()
    if (nothing) {
        item(key = "empty") {
            EmptyState(
                icon = Icons.Outlined.WbSunny,
                title = "今天很清静",
                body = "约定、追踪和每天的简报会出现在这里。想让 Acorn 做什么，直接在下面告诉它。",
            )
        }
        return
    }

    if (briefing != null) {
        item(key = "briefing:label") { SectionLabel(if (briefing.date == today) "今日简报" else "最近的简报") }
        item(key = "briefing") { BriefingCard(briefing, briefingSections, onClick = { onBriefingClick(briefing.threadId) }) }
    }

    if (now.commitments.isNotEmpty()) {
        item(key = "commitments:label") { SectionLabel("接下来") }
        items(now.commitments, key = { "commitment:${it.id}" }) { commitment ->
            CommitmentRow(commitment, wakeTime(commitment.wakeAt, today, zone), onCancel = { onCancel(commitment) })
            Hairline(inset = ScreenPadding + 64.dp)
        }
    }

    if (now.concerns.isNotEmpty()) {
        item(key = "concerns:label") { SectionLabel("正在跟进") }
        items(now.concerns, key = { "concern:${it.id}" }) { concern ->
            ConcernRow(concern)
            Hairline(inset = ScreenPadding + 21.dp)
        }
    }

    if (now.watches.isNotEmpty()) {
        item(key = "watches:label") { SectionLabel("追踪") }
        items(now.watches, key = { "watch:${it.id}" }) { watch ->
            WatchRow(watch, onPause = { onPause(watch.id) }, onResume = { onResume(watch.id) })
            Hairline(inset = ScreenPadding)
        }
    }
}

@Composable
private fun BriefingCard(briefing: NowBriefing, sections: List<BriefingSection>, onClick: () -> Unit) {
    val colors = AcornTheme.colors
    val preview = AcornTheme.type.reading.copy(fontSize = AcornTheme.type.callout.fontSize, lineHeight = AcornTheme.type.body.lineHeight)
    Panel(modifier = Modifier.padding(horizontal = ScreenPadding), onClick = onClick) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                "早安 · ${briefing.date.monthValue}月${briefing.date.dayOfMonth}日",
                style = AcornTheme.type.headline,
                color = colors.ink,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Spacer(Modifier.width(8.dp))
            Text(ago(briefing.finishedAt, OffsetDateTime.now()), style = AcornTheme.type.caption, color = colors.inkFaint)
        }
        sections.take(4).forEach { section ->
            Spacer(Modifier.height(12.dp))
            section.heading?.let {
                Text(it, style = AcornTheme.type.caption, color = colors.accent)
                Spacer(Modifier.height(2.dp))
            }
            Text(section.text, style = preview, color = colors.inkSoft, maxLines = 3, overflow = TextOverflow.Ellipsis)
        }
        Spacer(Modifier.height(14.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("阅读全文", style = AcornTheme.type.footnote.copy(fontWeight = FontWeight.Medium), color = colors.accent)
            Spacer(Modifier.width(4.dp))
            Icon(Icons.AutoMirrored.Outlined.ArrowForward, contentDescription = null, tint = colors.accent, modifier = Modifier.size(14.dp))
        }
    }
}

@Composable
private fun CommitmentRow(commitment: NowCommitment, wake: WakeTime, onCancel: () -> Unit) {
    val colors = AcornTheme.colors
    var menu by remember { mutableStateOf(false) }
    ListRow(
        onLongClick = { menu = true },
        leading = {
            Column(modifier = Modifier.widthIn(min = 50.dp)) {
                Text(wake.time, style = AcornTheme.type.headline, color = if (commitment.state == NowCommitment.State.due) colors.warning else colors.ink)
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
private fun ConcernRow(concern: NowConcern) {
    val colors = AcornTheme.colors
    val waiting = concern.state == NowConcern.State.waiting
    ListRow(
        leading = { StatusDot(if (waiting) colors.warning else colors.success, pulsing = !waiting) },
        trailing = if (waiting) ({ Tag("在等你", color = colors.warningInk, container = colors.warningSoft) }) else null,
    ) {
        Text(concern.title, style = AcornTheme.type.body, color = colors.ink)
        if (concern.reason.isNotBlank()) {
            Text(concern.reason, style = AcornTheme.type.footnote, color = colors.inkSoft, maxLines = 3, overflow = TextOverflow.Ellipsis)
        }
    }
}

@Composable
private fun WatchRow(watch: NowWatch, onPause: () -> Unit, onResume: () -> Unit) {
    val colors = AcornTheme.colors
    var menu by remember { mutableStateOf(false) }
    ListRow(
        onLongClick = { menu = true },
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
            if (watch.mode == NowWatch.Mode.immediate) "即时提醒" else "汇总进简报",
            intervalLabel(watch.intervalSeconds),
            watch.lastCheckedAt?.let { "${ago(it, OffsetDateTime.now())}检查" },
        ).joinToString(" · ")
        Text(meta, style = AcornTheme.type.footnote, color = colors.inkFaint)
        if (watch.status == NowWatch.Status.failing) {
            watch.lastError?.let {
                Text(it, style = AcornTheme.type.footnote, color = colors.danger, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

fun watchKindLabel(kind: NowWatch.Kind): String = when (kind) {
    NowWatch.Kind.rss -> "RSS"
    NowWatch.Kind.github -> "GitHub"
    NowWatch.Kind.web -> "网页"
    NowWatch.Kind.web_rendered -> "动态网页"
}

@Composable
private fun RowMenu(open: Boolean, onOpen: (Boolean) -> Unit, items: @Composable () -> Unit) {
    Box {
        IconAction(Icons.Outlined.MoreHoriz, "更多", { onOpen(true) }, tint = AcornTheme.colors.inkFaint)
        DropdownMenu(
            expanded = open,
            onDismissRequest = { onOpen(false) },
            containerColor = AcornTheme.colors.surface,
            shape = AcornShapes.medium,
        ) { items() }
    }
}

@Composable
private fun MenuItem(text: String, danger: Boolean = false, onClick: () -> Unit) {
    DropdownMenuItem(
        text = { Text(text, style = AcornTheme.type.callout, color = if (danger) AcornTheme.colors.danger else AcornTheme.colors.ink) },
        onClick = onClick,
    )
}

/** The entry to a new conversation, floating above the tab bar. */
@Composable
private fun AskBar(onClick: () -> Unit, modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    val shape = RoundedCornerShape(25.dp)
    Box(
        modifier = modifier
            .fillMaxWidth()
            .background(Brush.verticalGradient(listOf(Color.Transparent, colors.canvas, colors.canvas)))
            .padding(start = ScreenPadding, end = ScreenPadding, top = 20.dp, bottom = 12.dp),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .height(50.dp)
                .clip(shape)
                .background(colors.surface)
                .border(0.5.dp, colors.lineStrong, shape)
                .clickable(onClick = onClick)
                .padding(horizontal = 18.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(Icons.Outlined.AutoAwesome, contentDescription = null, tint = colors.accent, modifier = Modifier.size(18.dp))
            Spacer(Modifier.width(10.dp))
            Text("问点什么，或者交给 Acorn 去做", style = AcornTheme.type.callout, color = colors.inkFaint)
        }
    }
}
