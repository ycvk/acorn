package io.ycvk.acorn.feature.memory

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Psychology
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.MemorySummary
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornButton
import io.ycvk.acorn.core.ui.AcornIcons
import io.ycvk.acorn.core.ui.ButtonKind
import io.ycvk.acorn.core.ui.EmptyState
import io.ycvk.acorn.core.ui.ErrorState
import io.ycvk.acorn.core.ui.FilterPill
import io.ycvk.acorn.core.ui.Hairline
import io.ycvk.acorn.core.ui.ListRow
import io.ycvk.acorn.core.ui.OnReturnToForeground
import io.ycvk.acorn.core.ui.ScreenPadding
import io.ycvk.acorn.core.ui.SkeletonRows
import io.ycvk.acorn.core.ui.Tag
import io.ycvk.acorn.core.ui.listTime
import io.ycvk.acorn.feature.threads.SearchField
import java.time.LocalDate
import java.time.ZoneId

/** What Acorn remembers about the owner: search, a kind filter and the list. */
@Composable
fun MemoryList(viewModel: MemoryViewModel, onMemoryClick: (String) -> Unit) {
    val query by viewModel.query.collectAsStateWithLifecycle()
    val kind by viewModel.kind.collectAsStateWithLifecycle()
    val limit by viewModel.limit.collectAsStateWithLifecycle()
    val load by viewModel.list.collectAsStateWithLifecycle()

    LaunchedEffect(Unit) { viewModel.refresh() }
    OnReturnToForeground { viewModel.refresh() }

    Column(modifier = Modifier.fillMaxSize()) {
        SearchField(query, viewModel::onQueryChange, "搜索记忆", Modifier.padding(start = ScreenPadding, end = ScreenPadding, bottom = 8.dp))
        Row(
            modifier = Modifier.fillMaxWidth().padding(start = ScreenPadding - 4.dp, end = ScreenPadding, bottom = 8.dp),
            horizontalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            FilterPill("全部", kind == null, { viewModel.selectKind(null) })
            MemorySummary.Kind.entries.forEach { option ->
                FilterPill(kindLabel(option), kind == option, { viewModel.selectKind(option) })
            }
        }
        Hairline()
        when (val content = memoryListContent(load, query, kind, limit)) {
            MemoryListContent.Loading -> SkeletonRows()
            is MemoryListContent.Failed -> ErrorState(content.message, onRetry = viewModel::refresh)
            MemoryListContent.Empty -> EmptyState(
                icon = Icons.Outlined.Psychology,
                title = "还没有记忆",
                body = "和 Acorn 聊天、分享内容后，它会把关于你的事实和由此得出的推断整理在这里，每一条都能追溯到原话。",
            )
            MemoryListContent.NoMatches -> EmptyState(
                icon = AcornIcons.Search,
                title = "没有找到相关记忆",
                body = "换个关键词或类别试试。",
            )
            is MemoryListContent.Items -> {
                val zone = ZoneId.systemDefault()
                val today = LocalDate.now(zone)
                LazyColumn(modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 32.dp)) {
                    items(content.memories, key = { it.id }) { memory ->
                        Column(Modifier.animateItem()) {
                            MemoryRow(memory, listTime(memory.updatedAt, today, zone), onClick = { onMemoryClick(memory.id) })
                            Hairline(inset = ScreenPadding)
                        }
                    }
                    if (content.canLoadMore) {
                        item(key = "more") {
                            Box(modifier = Modifier.fillMaxWidth().padding(16.dp), contentAlignment = Alignment.Center) {
                                AcornButton("显示更多", viewModel::loadMore, kind = ButtonKind.Quiet, compact = true)
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun MemoryRow(memory: MemorySummary, time: String, onClick: () -> Unit) {
    val colors = AcornTheme.colors
    ListRow(onClick = onClick) {
        Text(memory.content, style = AcornTheme.type.body, color = colors.ink, maxLines = 3, overflow = TextOverflow.Ellipsis)
        Row(verticalAlignment = Alignment.CenterVertically) {
            Tag(kindLabel(memory.kind))
            memoryMarks(memory).takeIf { it.isNotEmpty() }?.let {
                Spacer(Modifier.width(8.dp))
                Text(it.joinToString(" · "), style = AcornTheme.type.caption, color = colors.inkSoft)
            }
            Spacer(Modifier.weight(1f))
            Text(time, style = AcornTheme.type.mono.copy(fontSize = 12.sp), color = colors.inkFaint)
        }
    }
}
