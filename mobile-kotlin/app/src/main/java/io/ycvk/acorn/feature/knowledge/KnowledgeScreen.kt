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
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.KnowledgeNoteSummary
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.EmptyState
import io.ycvk.acorn.core.ui.ErrorState
import io.ycvk.acorn.core.ui.Hairline
import io.ycvk.acorn.core.ui.LargeHeader
import io.ycvk.acorn.core.ui.ListRow
import io.ycvk.acorn.core.ui.ScreenPadding
import io.ycvk.acorn.core.ui.SkeletonRows
import io.ycvk.acorn.core.ui.listTime
import io.ycvk.acorn.core.ui.plainSnippet
import io.ycvk.acorn.feature.threads.SearchField
import java.time.LocalDate
import java.time.ZoneId

@Composable
fun KnowledgeScreen(
    viewModel: KnowledgeViewModel,
    onNoteClick: (String) -> Unit,
) {
    val query by viewModel.query.collectAsStateWithLifecycle()
    val load by viewModel.load.collectAsStateWithLifecycle()

    LaunchedEffect(Unit) { viewModel.refresh() }

    Column(modifier = Modifier.fillMaxSize()) {
        LargeHeader("知识")
        SearchField(query, viewModel::onQueryChange, "搜索笔记", Modifier.padding(horizontal = ScreenPadding, vertical = 6.dp))
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
                        NoteRow(note, listTime(note.updatedAt, today, zone), onClick = { onNoteClick(note.path) })
                        Hairline(inset = ScreenPadding)
                    }
                }
            }
        }
    }
}

/** The folder a note lives in, e.g. "briefings" for briefings/2026-10-11.md. */
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
