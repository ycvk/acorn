package io.ycvk.acorn.feature.now

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
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.NowBriefing
import io.ycvk.acorn.api.models.NowCommitment
import io.ycvk.acorn.api.models.NowConcern
import io.ycvk.acorn.api.models.NowResponse
import io.ycvk.acorn.api.models.NowWatch
import io.ycvk.acorn.core.theme.AetherError
import io.ycvk.acorn.core.theme.AetherOnSurface
import io.ycvk.acorn.core.theme.AetherOnSurfaceVariant
import io.ycvk.acorn.core.theme.AetherPrimary
import io.ycvk.acorn.core.theme.AetherSurfaceHigh
import io.ycvk.acorn.core.theme.AetherTertiary
import io.ycvk.acorn.feature.knowledge.snippetText
import io.ycvk.acorn.feature.threads.relativeTime
import java.time.LocalDate
import java.time.ZoneId

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NowScreen(
    viewModel: NowViewModel,
    onNoteClick: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    val load by viewModel.load.collectAsStateWithLifecycle()
    val refreshing by viewModel.refreshing.collectAsStateWithLifecycle()
    val actionError by viewModel.actionError.collectAsStateWithLifecycle()
    var cancelling by remember { mutableStateOf<NowCommitment?>(null) }

    LaunchedEffect(Unit) { viewModel.refresh() }

    Column(modifier = modifier.fillMaxSize()) {
        Text(
            "Now",
            style = MaterialTheme.typography.displayLarge,
            color = AetherOnSurface,
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 16.dp),
        )
        actionError?.let { message ->
            Row(
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(message, style = MaterialTheme.typography.bodySmall, color = AetherError, modifier = Modifier.weight(1f))
                TextButton(onClick = viewModel::dismissActionError) { Text("Dismiss", color = AetherPrimary) }
            }
        }
        when (val state = load) {
            NowLoad.Loading -> Centered { CircularProgressIndicator(color = AetherPrimary) }
            is NowLoad.Failed -> Centered {
                Text(state.message, style = MaterialTheme.typography.bodySmall, color = AetherError)
                TextButton(onClick = viewModel::refresh) { Text("Retry", color = AetherPrimary) }
            }
            is NowLoad.Loaded -> PullToRefreshBox(
                isRefreshing = refreshing,
                onRefresh = viewModel::refresh,
                modifier = Modifier.fillMaxWidth().weight(1f),
            ) {
                NowSections(
                    now = state.now,
                    onNoteClick = onNoteClick,
                    onCancel = { cancelling = it },
                    onPause = viewModel::pauseWatch,
                    onResume = viewModel::resumeWatch,
                )
            }
        }
    }

    cancelling?.let { commitment ->
        AlertDialog(
            onDismissRequest = { cancelling = null },
            title = { Text("Cancel commitment?") },
            text = { Text("\"${commitment.content}\" and its future wakes will be cancelled.") },
            confirmButton = {
                TextButton(onClick = {
                    cancelling = null
                    viewModel.cancelCommitment(commitment.id)
                }) { Text("Cancel it", color = AetherTertiary) }
            },
            dismissButton = {
                TextButton(onClick = { cancelling = null }) { Text("Keep") }
            },
        )
    }
}

@Composable
private fun NowSections(
    now: NowResponse,
    onNoteClick: (String) -> Unit,
    onCancel: (NowCommitment) -> Unit,
    onPause: (Long) -> Unit,
    onResume: (Long) -> Unit,
) {
    val zone = ZoneId.systemDefault()
    val today = LocalDate.now(zone)
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        section("Briefing")
        val briefing = now.briefing
        if (briefing == null) {
            empty("No briefing yet")
        } else {
            item(key = "briefing") { BriefingCard(briefing, onClick = { onNoteClick(briefing.path) }) }
        }

        section("Coming up")
        if (now.commitments.isEmpty()) empty("Nothing scheduled")
        items(now.commitments, key = { "commitment:${it.id}" }) { commitment ->
            CommitmentRow(commitment, wakeLabel(commitment.wakeAt, today, zone), onCancel = { onCancel(commitment) })
        }

        section("On it")
        if (now.concerns.isEmpty()) empty("Nothing in progress")
        items(now.concerns, key = { "concern:${it.id}" }) { ConcernRow(it) }

        section("Watches")
        if (now.watches.isEmpty()) empty("No watches")
        items(now.watches, key = { "watch:${it.id}" }) { watch ->
            WatchRow(watch, onPause = { onPause(watch.id) }, onResume = { onResume(watch.id) })
        }
    }
}

private fun LazyListScope.section(title: String) {
    item(key = "section:$title") {
        Text(
            title,
            style = MaterialTheme.typography.titleMedium,
            color = AetherOnSurface,
            modifier = Modifier.padding(top = 8.dp),
        )
    }
}

private fun LazyListScope.empty(text: String) {
    item(key = "empty:$text") {
        Text(text, style = MaterialTheme.typography.bodySmall, color = AetherOnSurfaceVariant)
    }
}

@Composable
private fun BriefingCard(briefing: NowBriefing, onClick: () -> Unit) {
    Card(onClick = onClick) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                briefing.title,
                style = MaterialTheme.typography.headlineSmall,
                color = AetherOnSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Spacer(Modifier.width(8.dp))
            Text(relativeTime(briefing.updatedAt), style = MaterialTheme.typography.labelSmall, color = AetherOnSurfaceVariant)
        }
        if (briefing.snippet.isNotBlank()) {
            Text(
                snippetText(briefing.snippet),
                style = MaterialTheme.typography.bodySmall,
                color = AetherOnSurfaceVariant,
                maxLines = 4,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

@Composable
private fun CommitmentRow(commitment: NowCommitment, wake: String, onCancel: () -> Unit) {
    Card {
        Text(commitment.content, style = MaterialTheme.typography.bodyMedium, color = AetherOnSurface)
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(modifier = Modifier.weight(1f)) {
                Text(wake, style = MaterialTheme.typography.labelSmall, color = AetherPrimary)
                commitment.recurrence?.let {
                    Text("repeats $it", style = MaterialTheme.typography.labelSmall, color = AetherOnSurfaceVariant)
                }
                if (commitment.occurrences.isNotEmpty()) {
                    Text("waiting for an outcome", style = MaterialTheme.typography.labelSmall, color = AetherTertiary)
                }
            }
            TextButton(onClick = onCancel) { Text("Cancel", color = AetherTertiary) }
        }
    }
}

@Composable
private fun ConcernRow(concern: NowConcern) {
    Card {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                concern.title,
                style = MaterialTheme.typography.bodyMedium,
                color = AetherOnSurface,
                modifier = Modifier.weight(1f),
            )
            if (concern.state == NowConcern.State.waiting) {
                Text("waiting", style = MaterialTheme.typography.labelSmall, color = AetherTertiary)
            }
        }
        if (concern.reason.isNotBlank()) {
            Text(concern.reason, style = MaterialTheme.typography.bodySmall, color = AetherOnSurfaceVariant)
        }
    }
}

@Composable
private fun WatchRow(watch: NowWatch, onPause: () -> Unit, onResume: () -> Unit) {
    Card {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(watch.name, style = MaterialTheme.typography.bodyMedium, color = AetherOnSurface)
                Text(
                    "${watch.kind.value} · ${watch.mode.value} · ${intervalLabel(watch.intervalSeconds)}",
                    style = MaterialTheme.typography.labelSmall,
                    color = AetherOnSurfaceVariant,
                )
                when (watch.status) {
                    NowWatch.Status.paused -> Text("paused", style = MaterialTheme.typography.labelSmall, color = AetherOnSurfaceVariant)
                    NowWatch.Status.failing -> Text(
                        "failing: ${watch.lastError.orEmpty()}",
                        style = MaterialTheme.typography.labelSmall,
                        color = AetherError,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                    NowWatch.Status.active -> watch.lastCheckedAt?.let {
                        Text("checked ${relativeTime(it)}", style = MaterialTheme.typography.labelSmall, color = AetherOnSurfaceVariant)
                    }
                }
            }
            when (watch.status) {
                NowWatch.Status.paused -> TextButton(onClick = onResume) { Text("Resume", color = AetherPrimary) }
                NowWatch.Status.failing -> {
                    TextButton(onClick = onResume) { Text("Retry", color = AetherPrimary) }
                    TextButton(onClick = onPause) { Text("Pause", color = AetherOnSurfaceVariant) }
                }
                NowWatch.Status.active -> TextButton(onClick = onPause) { Text("Pause", color = AetherOnSurfaceVariant) }
            }
        }
    }
}

@Composable
private fun Card(onClick: (() -> Unit)? = null, content: @Composable () -> Unit) {
    val body: @Composable () -> Unit = {
        Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) { content() }
    }
    if (onClick == null) {
        Surface(modifier = Modifier.fillMaxWidth(), shape = MaterialTheme.shapes.large, color = AetherSurfaceHigh) { body() }
    } else {
        Surface(onClick = onClick, modifier = Modifier.fillMaxWidth(), shape = MaterialTheme.shapes.large, color = AetherSurfaceHigh) { body() }
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
