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
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Chat
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.SearchOff
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.SwipeToDismissBox
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.rememberSwipeToDismissBoxState
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
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.Thread
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornButton
import io.ycvk.acorn.core.ui.EmptyState
import io.ycvk.acorn.core.ui.ErrorState
import io.ycvk.acorn.core.ui.Hairline
import io.ycvk.acorn.core.ui.IconAction
import io.ycvk.acorn.core.ui.InlineError
import io.ycvk.acorn.core.ui.LargeHeader
import io.ycvk.acorn.core.ui.ListRow
import io.ycvk.acorn.core.ui.ScreenPadding
import io.ycvk.acorn.core.ui.SectionLabel
import io.ycvk.acorn.core.ui.SkeletonRows
import io.ycvk.acorn.core.ui.StatusDot
import io.ycvk.acorn.core.ui.DayGroup
import io.ycvk.acorn.core.ui.clockTime
import io.ycvk.acorn.core.ui.dayGroup
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

    Column(modifier = Modifier.fillMaxSize()) {
        LargeHeader("对话") {
            IconAction(Icons.Outlined.Edit, "新对话", onNewChat)
        }
        SearchField(query, { query = it }, "搜索对话", Modifier.padding(horizontal = ScreenPadding, vertical = 6.dp))
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
                    icon = Icons.AutoMirrored.Outlined.Chat,
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
        AlertDialog(
            onDismissRequest = { pendingDelete = null },
            title = { Text("删除这个对话？") },
            text = { Text("「${threadTitle(thread)}」会被删除，无法恢复。") },
            confirmButton = {
                TextButton(onClick = {
                    viewModel.deleteThread(thread.id)
                    pendingDelete = null
                }) { Text("删除", color = AcornTheme.colors.danger) }
            },
            dismissButton = { TextButton(onClick = { pendingDelete = null }) { Text("取消") } },
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
        EmptyState(Icons.Outlined.SearchOff, "没有找到", "没有标题包含「${query.trim()}」的对话。")
        return
    }
    val zone = ZoneId.systemDefault()
    val today = LocalDate.now(zone)
    val groups = threads.groupBy { dayGroup(it.updatedAt, today, zone) }.toSortedMap()
    LazyColumn(modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
        groups.forEach { (group, items) ->
            item(key = "group:${group.name}") { SectionLabel(group.label, Modifier.animateItem()) }
            items(items, key = { it.id }) { thread ->
                SwipeToDelete(onDelete = { onDelete(thread) }, modifier = Modifier.animateItem()) {
                    ThreadRow(
                        thread = thread,
                        time = if (group == DayGroup.Today || group == DayGroup.Yesterday) clockTime(thread.updatedAt, zone) else listTime(thread.updatedAt, today, zone),
                        onClick = { onThreadClick(thread.id) },
                        onDelete = { onDelete(thread) },
                    )
                }
                Hairline(inset = ScreenPadding)
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun SwipeToDelete(onDelete: () -> Unit, modifier: Modifier = Modifier, content: @Composable () -> Unit) {
    val haptics = rememberHaptics()
    val state = rememberSwipeToDismissBoxState(
        confirmValueChange = { value ->
            if (value == SwipeToDismissBoxValue.EndToStart) {
                haptics.longPress()
                onDelete()
            }
            false
        },
    )
    SwipeToDismissBox(
        state = state,
        enableDismissFromStartToEnd = false,
        modifier = modifier,
        backgroundContent = {
            Box(
                modifier = Modifier
                    .fillMaxSize()
                    .background(AcornTheme.colors.danger)
                    .padding(horizontal = 24.dp),
                contentAlignment = Alignment.CenterEnd,
            ) {
                Icon(Icons.Outlined.Delete, contentDescription = "删除", tint = AcornTheme.colors.onAccent)
            }
        },
    ) {
        Box(Modifier.background(AcornTheme.colors.canvas)) { content() }
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
            trailing = { Text(time, style = AcornTheme.type.caption, color = colors.inkFaint) },
        ) {
            Text(threadTitle(thread), style = AcornTheme.type.headline, color = colors.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
            val state = threadState(thread.state)
            if (state != null) {
                val (label, tone) = state
                Row(verticalAlignment = Alignment.CenterVertically) {
                    val color = when (tone) {
                        Tone.Active -> colors.success
                        Tone.Attention -> colors.warning
                        Tone.Problem -> colors.danger
                    }
                    StatusDot(color, pulsing = tone == Tone.Active)
                    Spacer(Modifier.width(6.dp))
                    Text(label, style = AcornTheme.type.footnote, color = colors.inkSoft)
                }
            } else {
                threadPreview(thread)?.let {
                    Text(it, style = AcornTheme.type.footnote, color = colors.inkSoft, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
        }
        DropdownMenu(
            expanded = menu,
            onDismissRequest = { menu = false },
            containerColor = colors.surface,
            shape = AcornShapes.medium,
        ) {
            DropdownMenuItem(
                text = { Text("删除对话", style = AcornTheme.type.callout, color = colors.danger) },
                onClick = {
                    menu = false
                    onDelete()
                },
            )
        }
    }
}

private enum class Tone { Active, Attention, Problem }

private fun threadState(state: Thread.State): Pair<String, Tone>? = when (state) {
    Thread.State.running -> "正在进行" to Tone.Active
    Thread.State.interrupted -> "已暂停" to Tone.Attention
    Thread.State.failed -> "上次运行出错了" to Tone.Problem
    Thread.State.degraded -> "部分步骤失败" to Tone.Problem
    Thread.State.new, Thread.State.completed -> null
}

/** A rounded, filled search box. */
@Composable
fun SearchField(value: String, onValueChange: (String) -> Unit, placeholder: String, modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    Row(
        modifier = modifier
            .fillMaxWidth()
            .height(40.dp)
            .clip(AcornShapes.small)
            .background(colors.surfaceMuted)
            .padding(start = 12.dp, end = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(Icons.Outlined.Search, contentDescription = null, tint = colors.inkFaint, modifier = Modifier.size(18.dp))
        Spacer(Modifier.width(8.dp))
        Box(modifier = Modifier.weight(1f)) {
            if (value.isEmpty()) Text(placeholder, style = AcornTheme.type.callout, color = colors.inkFaint)
            BasicTextField(
                value = value,
                onValueChange = onValueChange,
                singleLine = true,
                textStyle = AcornTheme.type.callout.copy(color = colors.ink),
                cursorBrush = SolidColor(colors.accent),
                modifier = Modifier.fillMaxWidth(),
            )
        }
        if (value.isNotEmpty()) {
            IconAction(Icons.Outlined.Close, "清除", { onValueChange("") }, tint = colors.inkFaint, modifier = Modifier.size(36.dp))
        }
    }
}
