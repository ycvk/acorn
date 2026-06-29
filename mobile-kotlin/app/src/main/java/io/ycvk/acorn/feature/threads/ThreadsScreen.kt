package io.ycvk.acorn.feature.threads

import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.tween
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
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
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.FloatingActionButtonDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.SwipeToDismissBox
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberSwipeToDismissBoxState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.Thread
import io.ycvk.acorn.core.theme.AetherBackground
import io.ycvk.acorn.core.theme.AetherError
import io.ycvk.acorn.core.theme.AetherOnPrimary
import io.ycvk.acorn.core.theme.AetherOnSurface
import io.ycvk.acorn.core.theme.AetherOnSurfaceVariant
import io.ycvk.acorn.core.theme.AetherPrimary
import io.ycvk.acorn.core.theme.AetherSecondary
import io.ycvk.acorn.core.theme.AetherSurface
import io.ycvk.acorn.core.theme.AetherSurfaceHigh
import io.ycvk.acorn.core.theme.AetherTertiary
import io.ycvk.acorn.core.theme.gradientBackground
import java.time.Duration
import java.time.OffsetDateTime

private val MotionEasing = CubicBezierEasing(0.22f, 0.84f, 0.18f, 1f)

@OptIn(androidx.compose.foundation.ExperimentalFoundationApi::class)
@Composable
fun ThreadsScreen(
    onThreadClick: (String) -> Unit,
    modifier: Modifier = Modifier,
    onApprovalsClick: () -> Unit = {},
    pendingCount: Int = 0,
    onOpenDrawer: (() -> Unit)? = null,
    viewModel: ThreadsViewModel = hiltViewModel(),
) {
    val threads by viewModel.threads.collectAsStateWithLifecycle()
    val error by viewModel.error.collectAsStateWithLifecycle()

    var pendingDelete by remember { mutableStateOf<Thread?>(null) }

    LaunchedEffect(Unit) { viewModel.loadThreads() }

    pendingDelete?.let { thread ->
        AlertDialog(
            onDismissRequest = { pendingDelete = null },
            title = { Text("Delete thread?") },
            text = {
                Text("Delete \"${thread.title.ifBlank { "untitled" }}\"? This cannot be undone.")
            },
            confirmButton = {
                TextButton(onClick = {
                    viewModel.deleteThread(thread.id)
                    pendingDelete = null
                }) { Text("Delete", color = AetherError) }
            },
            dismissButton = {
                TextButton(onClick = { pendingDelete = null }) { Text("Cancel") }
            },
        )
    }

    Scaffold(
        modifier = modifier,
        containerColor = AetherBackground,
        floatingActionButton = {
            FloatingActionButton(
                onClick = { viewModel.createNewThread(onThreadClick) },
                containerColor = AetherPrimary,
                contentColor = AetherOnPrimary,
                shape = MaterialTheme.shapes.large,
                elevation = FloatingActionButtonDefaults.elevation(0.dp, 0.dp),
            ) {
                Icon(Icons.Filled.Add, contentDescription = "new thread")
            }
        },
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .gradientBackground()
                .padding(padding),
        ) {
            // Header
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 12.dp, vertical = 16.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                if (onOpenDrawer != null) {
                    Box(
                        modifier = Modifier
                            .size(40.dp)
                            .clip(CircleShape)
                            .background(AetherSurface.copy(alpha = 0.96f))
                            .clickable { onOpenDrawer() },
                        contentAlignment = Alignment.Center,
                    ) {
                        Icon(
                            Icons.Filled.Menu,
                            contentDescription = "menu",
                            tint = AetherOnSurface,
                            modifier = Modifier.size(20.dp),
                        )
                    }
                    Spacer(Modifier.width(12.dp))
                }
                Text(
                    "Inbox",
                    style = MaterialTheme.typography.displayLarge,
                    color = AetherOnSurface,
                )
                Spacer(Modifier.weight(1f))
                if (pendingCount > 0) {
                    Surface(
                        onClick = onApprovalsClick,
                        shape = MaterialTheme.shapes.large,
                        color = AetherTertiary.copy(alpha = 0.15f),
                        border = BorderStroke(1.dp, AetherTertiary.copy(alpha = 0.4f)),
                    ) {
                        Row(
                            modifier = Modifier.padding(horizontal = 10.dp, vertical = 4.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(4.dp),
                        ) {
                            Box(
                                modifier = Modifier
                                    .size(6.dp)
                                    .clip(CircleShape)
                                    .background(AetherTertiary),
                            )
                            Text(
                                "$pendingCount pending",
                                style = MaterialTheme.typography.labelSmall,
                                color = AetherTertiary,
                            )
                        }
                    }
                    Spacer(Modifier.width(12.dp))
                }
                Text(
                    "${threads.size} threads",
                    style = MaterialTheme.typography.labelSmall,
                    color = AetherOnSurfaceVariant,
                )
            }

            LazyColumn(
                modifier = Modifier.fillMaxSize(),
                contentPadding = PaddingValues(
                    start = 16.dp,
                    end = 16.dp,
                    top = 4.dp,
                    bottom = 96.dp,
                ),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                items(threads, key = { it.id }) { thread ->
                    val dismissState = rememberSwipeToDismissBoxState(
                        confirmValueChange = { value ->
                            if (value == SwipeToDismissBoxValue.EndToStart) {
                                pendingDelete = thread
                            }
                            false
                        },
                    )
                    SwipeToDismissBox(
                        state = dismissState,
                        enableDismissFromStartToEnd = false,
                        backgroundContent = {
                            Box(
                                modifier = Modifier
                                    .fillMaxSize()
                                    .clip(MaterialTheme.shapes.large)
                                    .background(AetherError)
                                    .padding(horizontal = 20.dp),
                                contentAlignment = Alignment.CenterEnd,
                            ) {
                                Icon(
                                    Icons.Filled.Delete,
                                    contentDescription = "delete",
                                    tint = AetherOnPrimary,
                                )
                            }
                        },
                        modifier = Modifier.animateItemPlacement(
                            tween(280, easing = MotionEasing),
                        ),
                    ) {
                        ThreadItem(thread = thread, onClick = { onThreadClick(thread.id) })
                    }
                }

                if (threads.isEmpty() && error == null) {
                    item {
                        Box(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(top = 80.dp),
                            contentAlignment = Alignment.Center,
                        ) {
                            Column(
                                horizontalAlignment = Alignment.CenterHorizontally,
                                verticalArrangement = Arrangement.spacedBy(8.dp),
                            ) {
                                Text(
                                    "No threads yet",
                                    style = MaterialTheme.typography.bodyMedium,
                                    color = AetherOnSurfaceVariant,
                                )
                                Text(
                                    "Tap + to start",
                                    style = MaterialTheme.typography.labelSmall,
                                    color = AetherOnSurfaceVariant,
                                )
                            }
                        }
                    }
                }

                error?.let {
                    item {
                        Surface(
                            modifier = Modifier.fillMaxWidth(),
                            shape = MaterialTheme.shapes.large,
                            color = AetherError.copy(alpha = 0.1f),
                        ) {
                            Text(
                                text = "$it",
                                style = MaterialTheme.typography.bodySmall,
                                color = AetherError,
                                modifier = Modifier.padding(12.dp),
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun ThreadItem(thread: Thread, onClick: () -> Unit) {
    val statusColor = when (thread.state) {
        Thread.State.running -> AetherSecondary
        Thread.State.failed -> AetherError
        Thread.State.degraded -> AetherTertiary
        else -> AetherOnSurfaceVariant
    }

    Surface(
        onClick = onClick,
        modifier = Modifier.fillMaxWidth(),
        shape = MaterialTheme.shapes.large,
        color = AetherSurfaceHigh,
    ) {
        Row(
            modifier = Modifier.padding(16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                modifier = Modifier
                    .size(8.dp)
                    .background(statusColor, CircleShape),
            )
            Spacer(Modifier.width(12.dp))

            Column(modifier = Modifier.weight(1f)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        text = thread.title.ifBlank { "untitled" },
                        style = MaterialTheme.typography.headlineSmall,
                        color = AetherOnSurface,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                    Spacer(Modifier.width(8.dp))
                    Text(
                        text = relativeTime(thread.updatedAt),
                        style = MaterialTheme.typography.labelSmall,
                        color = AetherOnSurfaceVariant,
                    )
                }
                Spacer(Modifier.height(4.dp))
                Text(
                    text = thread.state.value,
                    style = MaterialTheme.typography.labelSmall,
                    color = statusColor,
                )
            }
        }
    }
}

fun relativeTime(instant: OffsetDateTime): String {
    val minutes = Duration.between(instant, OffsetDateTime.now()).toMinutes()
    return when {
        minutes < 60 -> "${minutes}m"
        minutes < 1440 -> "${minutes / 60}h"
        minutes < 10080 -> "${minutes / 1440}d"
        else -> "${instant.monthValue}/${instant.dayOfMonth}"
    }
}
