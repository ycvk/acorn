package io.ycvk.acorn.feature.knowledge

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.KnowledgeNoteSummary
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornDialog
import io.ycvk.acorn.core.ui.AcornIcons
import io.ycvk.acorn.core.ui.EmptyState
import io.ycvk.acorn.core.ui.ErrorState
import io.ycvk.acorn.core.ui.Hairline
import io.ycvk.acorn.core.ui.InlineError
import io.ycvk.acorn.core.ui.ListRow
import io.ycvk.acorn.core.ui.PageHeader
import io.ycvk.acorn.core.ui.ScreenPadding
import io.ycvk.acorn.core.ui.SegmentedControl
import io.ycvk.acorn.core.ui.SkeletonRows
import io.ycvk.acorn.core.ui.SwipeToDelete
import io.ycvk.acorn.core.ui.Tag
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
        PageHeader("知识")
        SegmentedControl(
            options = listOf("笔记", "记忆"),
            selected = section,
            onSelect = { section = it },
            modifier = Modifier.padding(start = ScreenPadding - 4.dp, end = ScreenPadding, bottom = 10.dp),
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
        SearchField(query, viewModel::onQueryChange, "搜索笔记", Modifier.padding(start = ScreenPadding, end = ScreenPadding, bottom = 10.dp))
        Hairline()
        deleteError?.let {
            InlineError("没能删除：$it", Modifier.padding(horizontal = ScreenPadding, vertical = 4.dp), action = "知道了", onAction = viewModel::clearDeleteError)
        }
        when (val content = knowledgeContent(load, query)) {
            KnowledgeContent.Loading -> SkeletonRows()
            is KnowledgeContent.Failed -> ErrorState(content.message, onRetry = viewModel::refresh)
            KnowledgeContent.Empty -> EmptyState(
                icon = AcornIcons.Knowledge,
                title = "知识库还是空的",
                body = "在任意应用里点「分享」并选择 Acorn，链接、文字和图片都会整理成笔记放在这里。",
            )
            KnowledgeContent.NoMatches -> EmptyState(
                icon = AcornIcons.Search,
                title = "没有找到相关笔记",
                body = "换个关键词试试。",
            )
            is KnowledgeContent.Items -> {
                val zone = ZoneId.systemDefault()
                val today = LocalDate.now(zone)
                LazyColumn(modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 32.dp)) {
                    items(content.notes, key = { it.path }) { note ->
                        Column(Modifier.animateItem()) {
                            SwipeToDelete(onDelete = { pendingDelete = note }) {
                                NoteRow(note, listTime(note.updatedAt, today, zone), onClick = { onNoteClick(note.path) })
                            }
                            Hairline(inset = ScreenPadding + 29.dp)
                        }
                    }
                }
            }
        }
    }

    pendingDelete?.let { note ->
        AcornDialog(
            title = "删除这篇笔记？",
            body = "「${note.title}」会被删除，Acorn 也会忘掉其中的内容，无法恢复。",
            confirm = "删除",
            destructive = true,
            onConfirm = {
                viewModel.deleteNote(note.path) {}
                pendingDelete = null
            },
            onDismiss = { pendingDelete = null },
        )
    }
}

/** The folder a note lives in, e.g. "inbox" for inbox/tokio.md. */
fun noteFolder(path: String): String? = path.substringBeforeLast('/', "").takeIf { it.isNotEmpty() }

@Composable
private fun NoteRow(note: KnowledgeNoteSummary, time: String, onClick: () -> Unit) {
    val colors = AcornTheme.colors
    ListRow(
        onClick = onClick,
        leading = { Icon(AcornIcons.Doc, contentDescription = null, tint = colors.inkSoft, modifier = Modifier.padding(top = 1.dp).size(16.dp)) },
        verticalAlignment = Alignment.Top,
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Row(Modifier.weight(1f), verticalAlignment = Alignment.CenterVertically) {
                Text(note.title, style = AcornTheme.type.headline, color = colors.ink, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                noteFolder(note.path)?.let {
                    Spacer(Modifier.width(8.dp))
                    Tag(it)
                }
            }
            Spacer(Modifier.width(10.dp))
            Text(time, style = AcornTheme.type.mono.copy(fontSize = 12.sp), color = colors.inkFaint)
        }
        val snippet = plainSnippet(snippetText(note.snippet), note.title)
        if (snippet.isNotBlank()) {
            Text(snippet, style = AcornTheme.type.footnote, color = colors.inkSoft, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
    }
}
