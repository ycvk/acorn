package io.ycvk.acorn.feature.knowledge

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AutoStories
import androidx.compose.material.icons.outlined.SearchOff
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.KnowledgeNoteSummary
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.EmptyState
import io.ycvk.acorn.core.ui.ErrorState
import io.ycvk.acorn.core.ui.Hairline
import io.ycvk.acorn.core.ui.InlineError
import io.ycvk.acorn.core.ui.LargeHeader
import io.ycvk.acorn.core.ui.ListRow
import io.ycvk.acorn.core.ui.ScreenPadding
import io.ycvk.acorn.core.ui.SegmentedControl
import io.ycvk.acorn.core.ui.SkeletonRows
import io.ycvk.acorn.core.ui.SwipeToDelete
import io.ycvk.acorn.core.ui.listTime
import io.ycvk.acorn.core.ui.plainSnippet
import io.ycvk.acorn.feature.memory.MemoryList
import io.ycvk.acorn.feature.memory.MemoryViewModel
import io.ycvk.acorn.feature.threads.SearchField
import java.time.LocalDate
import java.time.ZoneId

/** The knowledge tab: notes Acorn keeps for the owner, and what it remembers about them. */
@Composable
fun KnowledgeScreen(
    viewModel: KnowledgeViewModel,
    memoryViewModel: MemoryViewModel,
    onNoteClick: (String) -> Unit,
    onMemoryClick: (String) -> Unit,
) {
    var section by rememberSaveable { mutableIntStateOf(0) }
    Column(modifier = Modifier.fillMaxSize()) {
        LargeHeader("知识")
        SegmentedControl(
            options = listOf("笔记", "记忆"),
            selected = section,
            onSelect = { section = it },
            modifier = Modifier.padding(horizontal = ScreenPadding, vertical = 4.dp),
        )
        when (section) {
            0 -> NoteList(viewModel, onNoteClick)
            else -> MemoryList(memoryViewModel, onMemoryClick)
        }
    }
}

@Composable
private fun NoteList(viewModel: KnowledgeViewModel, onNoteClick: (String) -> Unit) {
    val query by viewModel.query.collectAsStateWithLifecycle()
    val load by viewModel.load.collectAsStateWithLifecycle()
    val deleteError by viewModel.deleteError.collectAsStateWithLifecycle()
    var pendingDelete by remember { mutableStateOf<KnowledgeNoteSummary?>(null) }

    LaunchedEffect(Unit) { viewModel.refresh() }

    Column(modifier = Modifier.fillMaxSize()) {
        SearchField(query, viewModel::onQueryChange, "搜索笔记", Modifier.padding(horizontal = ScreenPadding, vertical = 6.dp))
        deleteError?.let {
            InlineError("没能删除：$it", Modifier.padding(horizontal = ScreenPadding, vertical = 4.dp), action = "知道了", onAction = viewModel::clearDeleteError)
        }
        when (val content = knowledgeContent(load, query)) {
            KnowledgeContent.Loading -> SkeletonRows()
            is KnowledgeContent.Failed -> ErrorState(content.message, onRetry = viewModel::refresh)
            KnowledgeContent.Empty -> EmptyState(
                icon = Icons.Outlined.AutoStories,
                title = "知识库还是空的",
                body = "在任意应用里点「分享」并选择 Acorn，链接、文字和图片都会整理成笔记放在这里。",
            )
            KnowledgeContent.NoMatches -> EmptyState(
                icon = Icons.Outlined.SearchOff,
                title = "没有找到相关笔记",
                body = "换个关键词试试。",
            )
            is KnowledgeContent.Items -> {
                val zone = ZoneId.systemDefault()
                val today = LocalDate.now(zone)
                LazyColumn(modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(top = 8.dp, bottom = 24.dp)) {
                    items(content.notes, key = { it.path }) { note ->
                        SwipeToDelete(onDelete = { pendingDelete = note }, modifier = Modifier.animateItem()) {
                            NoteRow(note, listTime(note.updatedAt, today, zone), onClick = { onNoteClick(note.path) })
                        }
                        Hairline(inset = ScreenPadding)
                    }
                }
            }
        }
    }

    pendingDelete?.let { note ->
        AlertDialog(
            onDismissRequest = { pendingDelete = null },
            title = { Text("删除这篇笔记？") },
            text = { Text("「${note.title}」会被删除，Acorn 也会忘掉其中的内容，无法恢复。") },
            confirmButton = {
                TextButton(onClick = {
                    viewModel.deleteNote(note.path) {}
                    pendingDelete = null
                }) { Text("删除", color = AcornTheme.colors.danger) }
            },
            dismissButton = { TextButton(onClick = { pendingDelete = null }) { Text("取消") } },
        )
    }
}

/** The folder a note lives in, e.g. "inbox" for inbox/tokio.md. */
fun noteFolder(path: String): String? = path.substringBeforeLast('/', "").takeIf { it.isNotEmpty() }

@Composable
private fun NoteRow(note: KnowledgeNoteSummary, time: String, onClick: () -> Unit) {
    val colors = AcornTheme.colors
    ListRow(onClick = onClick) {
        Text(note.title, style = AcornTheme.type.headline, color = colors.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
        val snippet = plainSnippet(snippetText(note.snippet), note.title)
        if (snippet.isNotBlank()) {
            Text(snippet, style = AcornTheme.type.footnote, color = colors.inkSoft, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
        Text(
            listOfNotNull(noteFolder(note.path), time).joinToString(" · "),
            style = AcornTheme.type.caption,
            color = colors.inkFaint,
        )
    }
}
