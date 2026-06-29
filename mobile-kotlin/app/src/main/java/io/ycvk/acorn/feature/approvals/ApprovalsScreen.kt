package io.ycvk.acorn.feature.approvals

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
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
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material3.AssistChip
import androidx.compose.material3.AssistChipDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.DecidePendingActionRequest
import io.ycvk.acorn.api.models.PendingActionDetail
import io.ycvk.acorn.api.models.PendingActionOption
import io.ycvk.acorn.api.models.PendingActionSummary
import io.ycvk.acorn.core.theme.AetherError
import io.ycvk.acorn.core.theme.AetherOnPrimary
import io.ycvk.acorn.core.theme.AetherOnSurface
import io.ycvk.acorn.core.theme.AetherOnSurfaceVariant
import io.ycvk.acorn.core.theme.AetherPrimary
import io.ycvk.acorn.core.theme.AetherSurface
import io.ycvk.acorn.core.theme.AetherSurfaceHigh
import io.ycvk.acorn.core.theme.AetherSurfaceVariant
import io.ycvk.acorn.core.theme.AetherTertiary
import io.ycvk.acorn.core.theme.gradientBackground
import java.time.Duration
import java.time.OffsetDateTime

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ApprovalsScreen(
    modifier: Modifier = Modifier,
    onThreadClick: (String) -> Unit = {},
    viewModel: ApprovalsViewModel = hiltViewModel(),
) {
    val pendingActions by viewModel.pendingActions.collectAsStateWithLifecycle()
    val selectedAction by viewModel.selectedAction.collectAsStateWithLifecycle()
    val error by viewModel.error.collectAsStateWithLifecycle()
    val deciding by viewModel.deciding.collectAsStateWithLifecycle()

    LaunchedEffect(Unit) { viewModel.loadPendingActions() }

    val detail = selectedAction
    if (detail != null) {
        PendingActionDetailSheet(
            detail = detail,
            deciding = deciding,
            onDismiss = { viewModel.clearSelection() },
            onDecide = viewModel::decideAction,
        )
    }

    LazyColumn(
        modifier = modifier
            .fillMaxSize()
            .gradientBackground(),
        contentPadding = PaddingValues(
            start = 16.dp,
            end = 16.dp,
            top = 4.dp,
            // Nav bar clearance; last item must clear bottom navigation.
            bottom = 80.dp,
        ),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        item {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(top = 16.dp, bottom = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    "Approvals",
                    style = MaterialTheme.typography.displayLarge,
                    color = AetherOnSurface,
                )
                if (pendingActions.isNotEmpty()) {
                    Spacer(Modifier.width(12.dp))
                    Surface(
                        color = AetherSurfaceHigh,
                        shape = MaterialTheme.shapes.large,
                    ) {
                        Text(
                            "${pendingActions.size} pending",
                            style = MaterialTheme.typography.labelSmall,
                            color = AetherTertiary,
                            modifier = Modifier.padding(horizontal = 8.dp, vertical = 2.dp),
                        )
                    }
                }
            }
        }
        items(pendingActions, key = { it.actionId }) { action ->
            PendingActionCard(
                action = action,
                onClick = { onThreadClick(action.threadId) },
                onReview = { viewModel.loadActionDetail(action.actionId) },
            )
        }

        if (pendingActions.isEmpty() && error == null) {
            item {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(top = 80.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Column(horizontalAlignment = Alignment.CenterHorizontally) {
                        Text(
                            "No pending approvals",
                            style = MaterialTheme.typography.bodyMedium,
                            color = AetherOnSurfaceVariant,
                        )
                        Spacer(Modifier.height(8.dp))
                        Text(
                            "All clear",
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
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(16.dp),
                    color = AetherError.copy(alpha = 0.1f),
                    shape = MaterialTheme.shapes.large,
                ) {
                    Text(
                        "$it",
                        style = MaterialTheme.typography.bodySmall,
                        color = AetherError,
                        modifier = Modifier.padding(12.dp),
                    )
                }
            }
        }
    }
}

@Composable
private fun PendingActionCard(
    action: PendingActionSummary,
    onClick: () -> Unit,
    onReview: () -> Unit,
) {
    Surface(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick),
        shape = MaterialTheme.shapes.large,
        color = AetherSurfaceHigh,
        contentColor = AetherOnSurface,
    ) {
        Column(modifier = Modifier.padding(16.dp)) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    action.kind.value,
                    style = MaterialTheme.typography.labelSmall,
                    color = AetherOnSurfaceVariant,
                )
                Spacer(Modifier.weight(1f))
                Text(
                    relativeTime(action.createdAt),
                    style = MaterialTheme.typography.labelSmall,
                    color = AetherOnSurfaceVariant,
                )
            }

            Spacer(Modifier.height(8.dp))

            Text(
                action.title.ifBlank { "untitled action" },
                style = MaterialTheme.typography.headlineSmall,
                color = AetherOnSurface,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )

            action.body?.takeIf { it.isNotBlank() }?.let {
                Spacer(Modifier.height(6.dp))
                Text(
                    it,
                    style = MaterialTheme.typography.bodyMedium,
                    color = AetherOnSurfaceVariant,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }

            Spacer(Modifier.height(10.dp))
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clickable(onClick = onReview)
                    .padding(vertical = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    "Review",
                    style = MaterialTheme.typography.labelSmall,
                    color = AetherPrimary,
                )
                Spacer(Modifier.weight(1f))
                Icon(
                    imageVector = Icons.Filled.ChevronRight,
                    contentDescription = null,
                    modifier = Modifier.size(16.dp),
                    tint = AetherOnSurfaceVariant,
                )
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
private fun PendingActionDetailSheet(
    detail: PendingActionDetail,
    deciding: Boolean,
    onDismiss: () -> Unit,
    onDecide: (actionId: String, decision: DecidePendingActionRequest.Decision, selectedOptionId: String?, answer: String?) -> Unit,
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    val sheetShape = RoundedCornerShape(topStart = 28.dp, topEnd = 28.dp)
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = sheetState,
        containerColor = AetherSurface,
        shape = sheetShape,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 24.dp)
                .padding(bottom = 32.dp),
        ) {
            Text(
                "${detail.kind.value} · ${detail.status.value}",
                style = MaterialTheme.typography.labelSmall,
                color = AetherOnSurfaceVariant,
            )

            Spacer(Modifier.height(12.dp))
            Text(
                detail.title.ifBlank { "untitled action" },
                style = MaterialTheme.typography.headlineMedium,
                color = AetherOnSurface,
            )

            detail.body?.takeIf { it.isNotBlank() }?.let {
                Spacer(Modifier.height(6.dp))
                Text(
                    it,
                    style = MaterialTheme.typography.bodyMedium,
                    color = AetherOnSurface,
                )
            }

            detail.reason?.takeIf { it.isNotBlank() }?.let {
                Spacer(Modifier.height(8.dp))
                Text(
                    "reason",
                    style = MaterialTheme.typography.labelSmall,
                    color = AetherOnSurfaceVariant,
                )
                Spacer(Modifier.height(2.dp))
                Text(
                    it,
                    style = MaterialTheme.typography.bodySmall,
                    color = AetherOnSurfaceVariant,
                )
            }

            detail.rule?.takeIf { it.isNotBlank() }?.let {
                Spacer(Modifier.height(4.dp))
                Text(
                    "rule",
                    style = MaterialTheme.typography.labelSmall,
                    color = AetherOnSurfaceVariant,
                )
                Spacer(Modifier.height(2.dp))
                Text(
                    it,
                    style = MaterialTheme.typography.bodySmall,
                    color = AetherOnSurfaceVariant,
                )
            }

            if (detail.options.isNotEmpty()) {
                Spacer(Modifier.height(20.dp))
                Text(
                    "options",
                    style = MaterialTheme.typography.labelSmall,
                    color = AetherOnSurfaceVariant,
                )
                Spacer(Modifier.height(10.dp))
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    detail.options.forEach { option ->
                        val isDecline =
                            decideFor(option) == DecidePendingActionRequest.Decision.decline
                        AssistChip(
                            onClick = {
                                onDecide(detail.actionId, decideFor(option), option.id, null)
                            },
                            label = {
                                Text(
                                    option.label,
                                    style = MaterialTheme.typography.labelLarge,
                                )
                            },
                            enabled = !deciding,
                            shape = MaterialTheme.shapes.medium,
                            colors = AssistChipDefaults.assistChipColors(
                                containerColor = if (isDecline) AetherSurfaceVariant else AetherPrimary,
                                labelColor = if (isDecline) AetherError else AetherOnPrimary,
                                disabledContainerColor = AetherSurfaceVariant,
                                disabledLabelColor = AetherOnSurfaceVariant,
                            ),
                        )
                    }
                }
            }

            if (deciding) {
                Spacer(Modifier.height(20.dp))
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.Center,
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(16.dp),
                        strokeWidth = 2.dp,
                        color = AetherPrimary,
                    )
                    Spacer(Modifier.width(8.dp))
                    Text(
                        "processing…",
                        style = MaterialTheme.typography.labelSmall,
                        color = AetherOnSurfaceVariant,
                    )
                }
            }
        }
    }
}

private fun decideFor(option: PendingActionOption): DecidePendingActionRequest.Decision {
    val label = option.label.lowercase()
    val isDecline = label.startsWith("reject") ||
        label.startsWith("decline") ||
        label.startsWith("no") ||
        label == "cancel"
    return if (isDecline) {
        DecidePendingActionRequest.Decision.decline
    } else {
        DecidePendingActionRequest.Decision.accept
    }
}

private fun relativeTime(createdAt: OffsetDateTime): String {
    val minutes = Duration.between(createdAt, OffsetDateTime.now()).toMinutes()
    return when {
        minutes < 60 -> "${minutes}m"
        minutes < 1440 -> "${minutes / 60}h"
        minutes < 10080 -> "${minutes / 1440}d"
        else -> "${createdAt.monthValue}/${createdAt.dayOfMonth}"
    }
}
