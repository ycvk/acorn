package io.ycvk.acorn.feature.knowledge

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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.KnowledgeNoteSummary
import io.ycvk.acorn.core.theme.AetherError
import io.ycvk.acorn.core.theme.AetherOnSurface
import io.ycvk.acorn.core.theme.AetherOnSurfaceVariant
import io.ycvk.acorn.core.theme.AetherOutlineSoft
import io.ycvk.acorn.core.theme.AetherPrimary
import io.ycvk.acorn.core.theme.AetherSurfaceHigh
import io.ycvk.acorn.feature.threads.relativeTime

@Composable
fun KnowledgeScreen(
    viewModel: KnowledgeViewModel,
    onNoteClick: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    val query by viewModel.query.collectAsStateWithLifecycle()
    val load by viewModel.load.collectAsStateWithLifecycle()

    LaunchedEffect(Unit) { viewModel.refresh() }

    Column(modifier = modifier.fillMaxSize()) {
        Text(
            "Knowledge",
            style = MaterialTheme.typography.displayLarge,
            color = AetherOnSurface,
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 16.dp),
        )
        OutlinedTextField(
            value = query,
            onValueChange = viewModel::onQueryChange,
            placeholder = { Text("Search notes") },
            leadingIcon = { Icon(Icons.Filled.Search, contentDescription = null) },
            singleLine = true,
            shape = RoundedCornerShape(16.dp),
            colors = OutlinedTextFieldDefaults.colors(
                focusedBorderColor = AetherPrimary,
                unfocusedBorderColor = AetherOutlineSoft,
            ),
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 16.dp),
        )
        Spacer(Modifier.height(12.dp))
        Box(modifier = Modifier.fillMaxWidth().weight(1f)) {
            when (val content = knowledgeContent(load, query)) {
                KnowledgeContent.Loading -> Centered { CircularProgressIndicator(color = AetherPrimary) }
                is KnowledgeContent.Failed -> Centered {
                    Text(content.message, style = MaterialTheme.typography.bodySmall, color = AetherError)
                    TextButton(onClick = viewModel::refresh) { Text("Retry", color = AetherPrimary) }
                }
                KnowledgeContent.Empty -> Centered {
                    Text("No notes yet", style = MaterialTheme.typography.titleMedium, color = AetherOnSurface)
                    Text(
                        "Share a link to Acorn and it will file a note here",
                        style = MaterialTheme.typography.bodySmall,
                        color = AetherOnSurfaceVariant,
                    )
                }
                KnowledgeContent.NoMatches -> Centered {
                    Text("No notes match", style = MaterialTheme.typography.bodyMedium, color = AetherOnSurfaceVariant)
                }
                is KnowledgeContent.Items -> LazyColumn(
                    modifier = Modifier.fillMaxSize(),
                    contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 24.dp),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    items(content.notes, key = { it.path }) { note ->
                        NoteItem(note = note, onClick = { onNoteClick(note.path) })
                    }
                }
            }
        }
    }
}

@Composable
private fun NoteItem(note: KnowledgeNoteSummary, onClick: () -> Unit) {
    Surface(
        onClick = onClick,
        modifier = Modifier.fillMaxWidth(),
        shape = MaterialTheme.shapes.large,
        color = AetherSurfaceHigh,
    ) {
        Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    note.title,
                    style = MaterialTheme.typography.headlineSmall,
                    color = AetherOnSurface,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                Spacer(Modifier.width(8.dp))
                Text(relativeTime(note.updatedAt), style = MaterialTheme.typography.labelSmall, color = AetherOnSurfaceVariant)
            }
            Text(note.path, style = MaterialTheme.typography.labelSmall, color = AetherOnSurfaceVariant)
            if (note.snippet.isNotBlank()) {
                Text(
                    snippetText(note.snippet),
                    style = MaterialTheme.typography.bodySmall,
                    color = AetherOnSurfaceVariant,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

@Composable
private fun Centered(content: @Composable () -> Unit) {
    Box(modifier = Modifier.fillMaxSize().padding(horizontal = 32.dp), contentAlignment = Alignment.Center) {
        Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
            content()
        }
    }
}
