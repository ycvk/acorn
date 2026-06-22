package io.ycvk.acorn.feature.runs

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.RunArtifact
import io.ycvk.acorn.api.models.RunDetail

/**
 * Run detail view: shows the run's status, thread, event count, and artifacts.
 * Secondary to the live chat stream; used to inspect a finished run.
 */
@Composable
fun RunDetailScreen(
    runId: String,
    modifier: Modifier = Modifier,
    viewModel: RunDetailViewModel = hiltViewModel(),
) {
    val runDetail by viewModel.runDetail.collectAsStateWithLifecycle()
    val error by viewModel.error.collectAsStateWithLifecycle()

    LaunchedEffect(runId) { viewModel.loadRunDetail(runId) }

    val detail = runDetail
    if (detail == null && error == null) {
        Box(modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            CircularProgressIndicator()
        }
        return
    }

    LazyColumn(
        modifier = modifier.fillMaxSize().padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        detail?.let { d ->
            item { RunSummaryCard(d) }
            if (d.artifacts.isNotEmpty()) {
                item {
                    Text(
                        "Artifacts (${d.artifacts.size})",
                        style = MaterialTheme.typography.titleMedium,
                        fontWeight = FontWeight.SemiBold,
                    )
                }
                items(d.artifacts, key = { it.artifactId }) { artifact ->
                    ArtifactRow(artifact)
                    HorizontalDivider()
                }
            }
            item {
                Text(
                    "Events: ${d.events.size}",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }

        error?.let {
            item {
                Text(
                    it,
                    color = MaterialTheme.colorScheme.error,
                    style = MaterialTheme.typography.bodySmall,
                )
            }
        }
    }
}

@Composable
private fun RunSummaryCard(detail: RunDetail) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(
                detail.thread.title.ifBlank { "Untitled thread" },
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
            )
            Text(
                "Run ${detail.run.id}",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Text(
                "Status: ${detail.run.status.name}",
                style = MaterialTheme.typography.bodyMedium,
            )
            Text(
                "Mode: ${detail.run.mode.name}",
                style = MaterialTheme.typography.bodyMedium,
            )
        }
    }
}

@Composable
private fun ArtifactRow(artifact: RunArtifact) {
    Column(Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
        Text(
            artifact.title ?: artifact.artifactId,
            style = MaterialTheme.typography.bodyMedium,
            fontWeight = FontWeight.Medium,
        )
        Text(
            "${artifact.kind.name} · ${artifact.mimeType ?: "—"} · ${artifact.sizeBytes} bytes",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}
