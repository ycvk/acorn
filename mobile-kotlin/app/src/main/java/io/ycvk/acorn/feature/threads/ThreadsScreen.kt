package io.ycvk.acorn.feature.threads

import androidx.compose.foundation.background
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.Thread
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornButton
import io.ycvk.acorn.core.ui.AcornDialog
import io.ycvk.acorn.core.ui.AcornIcons
import io.ycvk.acorn.core.ui.AcornMenu
import io.ycvk.acorn.core.ui.AcornMenuItem
import io.ycvk.acorn.core.ui.DayGroup
import io.ycvk.acorn.core.ui.EmptyState
import io.ycvk.acorn.core.ui.ErrorState
import io.ycvk.acorn.core.ui.Hairline
import io.ycvk.acorn.core.ui.IconAction
import io.ycvk.acorn.core.ui.InlineError
import io.ycvk.acorn.core.ui.ListRow
import io.ycvk.acorn.core.ui.OnReturnToForeground
import io.ycvk.acorn.core.ui.PageHeader
import io.ycvk.acorn.core.ui.ScreenPadding
import io.ycvk.acorn.core.ui.SectionLabel
import io.ycvk.acorn.core.ui.SkeletonRows
import io.ycvk.acorn.core.ui.Status
import io.ycvk.acorn.core.ui.StatusGlyph
import io.ycvk.acorn.core.ui.SwipeToDelete
import io.ycvk.acorn.core.ui.clockTime
import io.ycvk.acorn.core.ui.dayGroup
import io.ycvk.acorn.core.ui.groupItem
import io.ycvk.acorn.core.ui.hairline
import io.ycvk.acorn.core.ui.listTime
import io.ycvk.acorn.core.ui.plainSnippet
import io.ycvk.acorn.core.ui.rememberHaptics
import java.time.LocalDate
import java.time.ZoneId

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ThreadsScreen(
    onThreadClick: (String) -> Unit,
    onNewChat: () -> Unit,
    viewModel: ThreadsViewModel = hiltViewModel(),
) {
    val threads by viewModel.threads.collectAsStateWithLifecycle()
    val error by viewModel.error.collectAsStateWithLifecycle()
    val load by viewModel.load.collectAsStateWithLifecycle()
    val refreshing by viewModel.refreshing.collectAsStateWithLifecycle()
    var query by rememberSaveable { mutableStateOf("") }
    var pendingDelete by remember { mutableStateOf<Thread?>(null) }

    LaunchedEffect(Unit) { viewModel.loadThreads() }
    OnReturnToForeground(viewModel::loadThreads)

    Column(modifier = Modifier.fillMaxSize()) {
        PageHeader("对话") {
            IconAction(AcornIcons.Compose, "新对话", onNewChat, filled = true)
        }
        SearchField(query, { query = it }, "搜索对话", Modifier.padding(start = ScreenPadding, end = ScreenPadding, bottom = 4.dp))
        error?.let {
            InlineError(it, Modifier.padding(horizontal = ScreenPadding, vertical = 4.dp), action = "知道了", onAction = viewModel::dismissError)
        }
        PullToRefreshBox(
            isRefreshing = refreshing && threads.isNotEmpty(),
            onRefresh = viewModel::loadThreads,
            modifier = Modifier.weight(1f),
        ) {
            when (val content = threadsContent(load, threads.size)) {
                ThreadsContent.Loading -> SkeletonRows()
                is ThreadsContent.Failed -> ErrorState(content.message, onRetry = viewModel::loadThreads)
                ThreadsContent.Empty -> EmptyState(
                    icon = AcornIcons.Threads,
                    title = "还没有对话",
                    body = "和 Acorn 说点什么。它会记住重要的事，按约定提醒你。",
                    action = { AcornButton("开始对话", onNewChat, compact = true) },
                )
                ThreadsContent.Items -> ThreadList(
                    threads = threads.filter { query.isBlank() || it.title.contains(query.trim(), ignoreCase = true) },
                    query = query,
                    onThreadClick = onThreadClick,
                    onDelete = { pendingDelete = it },
                )
            }
        }
    }

    pendingDelete?.let { thread ->
        AcornDialog(
            title = "删除这个对话？",
            body = "「${threadTitle(thread)}」会被删除，Acorn 也会忘掉其中说过的内容，无法恢复。",
            confirm = "删除",
            destructive = true,
            onConfirm = {
                viewModel.deleteThread(thread.id)
                pendingDelete = null
            },
            onDismiss = { pendingDelete = null },
        )
    }
}

/** The latest reply as plain text; the owner's own message reads as "你：…". */
fun threadPreview(thread: Thread): String? {
    val text = plainSnippet(thread.preview.orEmpty()).takeIf { it.isNotEmpty() } ?: return null
    return if (thread.previewRole == Thread.PreviewRole.user) "你：$text" else text
}

fun threadTitle(thread: Thread): String = thread.title.takeIf { it.isNotBlank() && it != "New Thread" } ?: "新对话"

@Composable
private fun ThreadList(
    threads: List<Thread>,
    query: String,
    onThreadClick: (String) -> Unit,
    onDelete: (Thread) -> Unit,
) {
    if (threads.isEmpty()) {
        EmptyState(AcornIcons.Search, "没有找到", "没有标题包含「${query.trim()}」的对话。")
        return
    }
    val zone = ZoneId.systemDefault()
    val today = LocalDate.now(zone)
    val groups = threads.groupBy { dayGroup(it.updatedAt, today, zone) }.toSortedMap()
    LazyColumn(modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
        groups.forEach { (group, items) ->
            item(key = "group:${group.name}") { SectionLabel(group.label, Modifier.animateItem(), count = items.size) }
            itemsIndexed(items, key = { _, it -> it.id }) { index, thread ->
                Column(Modifier.animateItem().groupItem(index, items.size)) {
                    SwipeToDelete(onDelete = { onDelete(thread) }) {
                        ThreadRow(
                            thread = thread,
                            time = if (group == DayGroup.Today || group == DayGroup.Yesterday) clockTime(thread.updatedAt, zone) else listTime(thread.updatedAt, today, zone),
                            onClick = { onThreadClick(thread.id) },
                            onDelete = { onDelete(thread) },
                        )
                    }
                    if (index < items.lastIndex) Hairline(inset = ScreenPadding + 28.dp)
                }
            }
        }
    }
}

@Composable
private fun ThreadRow(thread: Thread, time: String, onClick: () -> Unit, onDelete: () -> Unit) {
    val colors = AcornTheme.colors
    var menu by remember { mutableStateOf(false) }
    val haptics = rememberHaptics()
    Box {
        ListRow(
            onClick = onClick,
            onLongClick = {
                haptics.longPress()
                menu = true
            },
            leading = { StatusGlyph(threadStatus(thread.state), Modifier.padding(top = 3.dp)) },
            verticalAlignment = Alignment.Top,
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(threadTitle(thread), style = AcornTheme.type.headline, color = colors.ink, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                Spacer(Modifier.width(10.dp))
                Text(time, style = AcornTheme.type.mono.copy(fontSize = 12.sp), color = colors.inkFaint)
            }
            val line = threadStateLabel(thread.state) ?: threadPreview(thread)
            line?.let {
                Text(it, style = AcornTheme.type.footnote, color = colors.inkSoft, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        AcornMenu(expanded = menu, onDismiss = { menu = false }) {
            AcornMenuItem("删除对话", onClick = {
                menu = false
                onDelete()
            }, danger = true, icon = AcornIcons.Trash)
        }
    }
}

private fun threadStatus(state: Thread.State): Status = when (state) {
    Thread.State.running -> Status.Running
    Thread.State.interrupted -> Status.Waiting
    Thread.State.failed, Thread.State.degraded -> Status.Failed
    Thread.State.completed -> Status.Done
    Thread.State.new -> Status.Idle
}

private fun threadStateLabel(state: Thread.State): String? = when (state) {
    Thread.State.running -> "正在进行"
    Thread.State.interrupted -> "已暂停，等你确认"
    Thread.State.failed -> "上次运行出错了"
    Thread.State.degraded -> "部分步骤失败"
    Thread.State.new, Thread.State.completed -> null
}

/** A search input on a pill. */
@Composable
fun SearchField(value: String, onValueChange: (String) -> Unit, placeholder: String, modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    Row(
        modifier = modifier
            .fillMaxWidth()
            .height(42.dp)
            .clip(AcornShapes.pill)
            .background(colors.surface)
            .hairline(AcornShapes.pill)
            .padding(start = 14.dp, end = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(AcornIcons.Search, contentDescription = null, tint = colors.inkFaint, modifier = Modifier.size(16.dp))
        Spacer(Modifier.width(8.dp))
        Box(modifier = Modifier.weight(1f)) {
            if (value.isEmpty()) Text(placeholder, style = AcornTheme.type.footnote, color = colors.inkFaint)
            BasicTextField(
                value = value,
                onValueChange = onValueChange,
                singleLine = true,
                textStyle = AcornTheme.type.footnote.copy(color = colors.ink),
                cursorBrush = SolidColor(colors.accent),
                modifier = Modifier.fillMaxWidth(),
            )
        }
        if (value.isNotEmpty()) {
            IconAction(AcornIcons.Close, "清除", { onValueChange("") }, tint = colors.inkFaint, modifier = Modifier.size(32.dp))
        }
    }
}
