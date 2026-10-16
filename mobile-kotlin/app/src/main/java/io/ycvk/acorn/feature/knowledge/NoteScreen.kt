package io.ycvk.acorn.feature.knowledge

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Link
import androidx.compose.material.icons.outlined.MoreHoriz
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.KnowledgeNote
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.ErrorState
import io.ycvk.acorn.core.ui.IconAction
import io.ycvk.acorn.core.ui.InlineError
import io.ycvk.acorn.core.ui.Markdown
import io.ycvk.acorn.core.ui.ScreenPadding
import io.ycvk.acorn.core.ui.SkeletonRows
import io.ycvk.acorn.core.ui.TopBar
import io.ycvk.acorn.core.ui.ago
import java.net.URI
import java.time.OffsetDateTime

@Composable
fun NoteScreen(
    path: String,
    viewModel: KnowledgeViewModel,
    onBack: () -> Unit,
) {
    val load by viewModel.note.collectAsStateWithLifecycle()
    val deleteError by viewModel.deleteError.collectAsStateWithLifecycle()
    LaunchedEffect(path) {
        viewModel.clearDeleteError()
        viewModel.openNote(path)
    }
    val scroll = rememberScrollState()
    val note = (load as? NoteLoad.Loaded)?.note
    val colors = AcornTheme.colors
    var menu by remember { mutableStateOf(false) }
    var confirmDelete by remember { mutableStateOf(false) }

    Column(modifier = Modifier.fillMaxSize().background(colors.canvas)) {
        // The title moves into the bar once the heading scrolls away.
        TopBar(
            onBack = onBack,
            title = note?.title?.takeIf { scroll.value > 160 },
            showDivider = scroll.value > 0,
        ) {
            if (note != null) {
                Box {
                    IconAction(Icons.Outlined.MoreHoriz, "更多", { menu = true })
                    DropdownMenu(
                        expanded = menu,
                        onDismissRequest = { menu = false },
                        containerColor = colors.surface,
                        shape = AcornShapes.medium,
                    ) {
                        DropdownMenuItem(
                            text = { Text("删除笔记", style = AcornTheme.type.callout, color = colors.danger) },
                            onClick = {
                                menu = false
                                confirmDelete = true
                            },
                        )
                    }
                }
            }
        }
        deleteError?.let { message ->
            InlineError(
                message,
                modifier = Modifier.padding(horizontal = ScreenPadding, vertical = 4.dp),
                action = "知道了",
                onAction = viewModel::clearDeleteError,
            )
        }
        when (val current = load) {
            NoteLoad.Loading -> SkeletonRows(rows = 5)
            is NoteLoad.Failed -> ErrorState(current.message, onRetry = { viewModel.openNote(path) })
            is NoteLoad.Loaded -> NoteBody(
                note = current.note,
                profile = viewModel.connection(),
                modifier = Modifier.verticalScroll(scroll),
            )
        }
    }

    if (confirmDelete) {
        AlertDialog(
            onDismissRequest = { confirmDelete = false },
            title = { Text("删除这篇笔记？") },
            text = { Text("笔记会被删除，Acorn 也会忘掉其中的内容，无法恢复。") },
            confirmButton = {
                TextButton(onClick = {
                    confirmDelete = false
                    viewModel.deleteNote(path, onBack)
                }) { Text("删除", color = colors.danger) }
            },
            dismissButton = { TextButton(onClick = { confirmDelete = false }) { Text("取消") } },
        )
    }
}

/** Drops a leading "# Title" when it repeats the note title shown above the body. */
fun bodyWithoutTitle(body: String, title: String): String {
    val trimmed = body.trimStart()
    val first = trimmed.lineSequence().firstOrNull().orEmpty()
    return if (first.startsWith("# ") && first.removePrefix("# ").trim() == title.trim()) trimmed.removePrefix(first).trimStart() else body
}

@Composable
private fun NoteBody(note: KnowledgeNote, profile: ConnectionProfile?, modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    val uriHandler = LocalUriHandler.current
    Column(
        modifier = modifier
            .navigationBarsPadding()
            .padding(start = ScreenPadding, end = ScreenPadding, top = 4.dp, bottom = 40.dp),
    ) {
        noteFolder(note.path)?.let {
            Text(it, style = AcornTheme.type.footnote, color = colors.accent)
            Spacer(Modifier.height(6.dp))
        }
        Text(note.title, style = AcornTheme.type.largeTitle.copy(fontSize = AcornTheme.type.title.fontSize * 1.2f), color = colors.ink)
        Spacer(Modifier.height(8.dp))
        val meta = listOfNotNull(
            "${ago(note.updatedAt, OffsetDateTime.now())}更新",
            note.tags.takeIf { it.isNotEmpty() }?.joinToString(" ") { "#$it" },
        ).joinToString("  ·  ")
        Text(meta, style = AcornTheme.type.footnote, color = colors.inkFaint)
        note.source?.let { source ->
            Spacer(Modifier.height(14.dp))
            Row(
                modifier = Modifier
                    .clip(AcornShapes.small)
                    .background(colors.surfaceMuted)
                    .clickable { uriHandler.openUri(source) }
                    .padding(horizontal = 12.dp, vertical = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(Icons.Outlined.Link, contentDescription = null, tint = colors.inkSoft, modifier = Modifier.size(16.dp))
                Spacer(Modifier.width(8.dp))
                Text(
                    runCatching { URI(source).host }.getOrNull() ?: source,
                    style = AcornTheme.type.footnote,
                    color = colors.inkSoft,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        Spacer(Modifier.height(24.dp))
        Column(verticalArrangement = Arrangement.spacedBy(14.dp)) {
            noteSegments(bodyWithoutTitle(note.body, note.title)).forEach { segment ->
                when (segment) {
                    is NoteSegment.Markdown -> SelectionContainer { Markdown(segment.text) }
                    is NoteSegment.Image -> if (profile != null) AttachmentImage(segment.path, segment.description, profile)
                }
            }
        }
    }
}
