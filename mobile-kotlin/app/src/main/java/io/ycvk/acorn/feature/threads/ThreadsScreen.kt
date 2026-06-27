package io.ycvk.acorn.feature.threads

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
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
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.FloatingActionButtonDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.Thread
import io.ycvk.acorn.core.theme.Accent
import io.ycvk.acorn.core.theme.AccentDim
import io.ycvk.acorn.core.theme.Bg
import io.ycvk.acorn.core.theme.Border
import io.ycvk.acorn.core.theme.Danger
import io.ycvk.acorn.core.theme.OnAccent
import io.ycvk.acorn.core.theme.Surface
import io.ycvk.acorn.core.theme.TextPrimary
import io.ycvk.acorn.core.theme.TextSecondary
import io.ycvk.acorn.core.theme.Warning

@Composable
fun ThreadsScreen(
    onThreadClick: (String) -> Unit,
    modifier: Modifier = Modifier,
    viewModel: ThreadsViewModel = hiltViewModel(),
) {
    val threads by viewModel.threads.collectAsStateWithLifecycle()
    val error by viewModel.error.collectAsStateWithLifecycle()

    LaunchedEffect(Unit) { viewModel.loadThreads() }

    Scaffold(
        modifier = modifier,
        containerColor = Bg,
        floatingActionButton = {
            FloatingActionButton(
                onClick = { viewModel.createNewThread(onThreadClick) },
                containerColor = AccentDim,
                contentColor = OnAccent,
                shape = RoundedCornerShape(4.dp),
                elevation = FloatingActionButtonDefaults.elevation(0.dp, 0.dp),
            ) {
                Icon(Icons.Filled.Add, contentDescription = "new thread")
            }
        },
    ) { padding ->
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .background(Bg)
                .padding(padding),
            contentPadding = androidx.compose.foundation.layout.PaddingValues(
                horizontal = 0.dp,
                vertical = 4.dp,
            ),
            verticalArrangement = Arrangement.spacedBy(1.dp),
        ) {
            items(threads, key = { it.id }) { thread ->
                ThreadItem(thread = thread, onClick = { onThreadClick(thread.id) })
            }

            if (threads.isEmpty() && error == null) {
                item {
                    Box(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(top = 120.dp),
                        contentAlignment = Alignment.Center,
                    ) {
                        Text(
                            "> no threads. tap + to start",
                            style = MaterialTheme.typography.bodySmall.copy(
                                fontFamily = FontFamily.Monospace,
                            ),
                            color = TextSecondary,
                        )
                    }
                }
            }

            error?.let {
                item {
                    Text(
                        "! $it",
                        modifier = Modifier.padding(16.dp),
                        style = MaterialTheme.typography.bodySmall.copy(
                            fontFamily = FontFamily.Monospace,
                        ),
                        color = Danger,
                    )
                }
            }
        }
    }
}

@Composable
private fun ThreadItem(thread: Thread, onClick: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .background(Surface)
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        // Status dot
        val statusColor = when (thread.state) {
            Thread.State.running -> Accent
            Thread.State.failed -> Danger
            Thread.State.degraded -> Warning
            else -> TextSecondary
        }
        Box(
            modifier = Modifier
                .size(8.dp)
                .clip(CircleShape)
                .background(statusColor),
        )
        Spacer(Modifier.width(12.dp))

        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = thread.title.ifBlank { "untitled" },
                style = MaterialTheme.typography.headlineSmall,
                color = TextPrimary,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Spacer(Modifier.height(2.dp))
            Text(
                text = thread.id,
                style = MaterialTheme.typography.titleSmall,
                color = TextSecondary,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }

        Text(
            text = thread.state.value,
            style = MaterialTheme.typography.labelSmall,
            color = statusColor,
        )
    }
}
