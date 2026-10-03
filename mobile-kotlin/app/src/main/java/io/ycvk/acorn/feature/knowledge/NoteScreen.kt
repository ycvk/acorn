package io.ycvk.acorn.feature.knowledge

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.jeziellago.compose.markdowntext.MarkdownText
import io.ycvk.acorn.api.models.KnowledgeNote
import io.ycvk.acorn.core.theme.AetherError
import io.ycvk.acorn.core.theme.AetherOnSurface
import io.ycvk.acorn.core.theme.AetherOnSurfaceVariant
import io.ycvk.acorn.core.theme.AetherPrimary
import io.ycvk.acorn.core.theme.AetherSurface
import io.ycvk.acorn.core.theme.gradientBackground
import io.ycvk.acorn.feature.threads.relativeTime

@Composable
fun NoteScreen(
    path: String,
    viewModel: KnowledgeViewModel,
    onBack: () -> Unit,
) {
    val load by viewModel.note.collectAsStateWithLifecycle()
    LaunchedEffect(path) { viewModel.openNote(path) }

    Column(modifier = Modifier.fillMaxSize().gradientBackground()) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .statusBarsPadding()
                .padding(horizontal = 12.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                modifier = Modifier
                    .size(40.dp)
                    .clip(CircleShape)
                    .background(AetherSurface.copy(alpha = 0.96f))
                    .clickable(onClick = onBack),
                contentAlignment = Alignment.Center,
            ) {
                Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "back", tint = AetherOnSurface, modifier = Modifier.size(20.dp))
            }
            Spacer(Modifier.width(12.dp))
            Text(
                path,
                style = MaterialTheme.typography.labelMedium,
                color = AetherOnSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        when (val current = load) {
            NoteLoad.Loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                CircularProgressIndicator(color = AetherPrimary)
            }
            is NoteLoad.Failed -> Column(
                modifier = Modifier.fillMaxSize().padding(32.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.Center,
            ) {
                Text(current.message, style = MaterialTheme.typography.bodySmall, color = AetherError)
                TextButton(onClick = { viewModel.openNote(path) }) { Text("Retry", color = AetherPrimary) }
            }
            is NoteLoad.Loaded -> NoteBody(current.note)
        }
    }
}

@Composable
private fun NoteBody(note: KnowledgeNote) {
    val uriHandler = LocalUriHandler.current
    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .navigationBarsPadding()
            .padding(horizontal = 20.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(
            note.title,
            style = MaterialTheme.typography.headlineMedium.copy(fontWeight = FontWeight.SemiBold),
            color = AetherOnSurface,
        )
        val meta = listOfNotNull(
            "Updated ${relativeTime(note.updatedAt)} ago",
            note.tags.takeIf { it.isNotEmpty() }?.joinToString(" ") { "#$it" },
        ).joinToString("  ·  ")
        Text(meta, style = MaterialTheme.typography.labelSmall, color = AetherOnSurfaceVariant)
        note.source?.let { source ->
            Text(
                source,
                style = MaterialTheme.typography.bodySmall,
                color = AetherPrimary,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.clickable { uriHandler.openUri(source) },
            )
        }
        SelectionContainer {
            MarkdownText(
                markdown = withAttachmentPlaceholders(note.body),
                style = MaterialTheme.typography.bodyLarge.copy(color = AetherOnSurface),
            )
        }
    }
}
