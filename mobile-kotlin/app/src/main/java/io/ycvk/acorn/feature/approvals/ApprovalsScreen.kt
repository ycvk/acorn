package io.ycvk.acorn.feature.approvals

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.AssistChip
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.DecidePendingActionRequest
import io.ycvk.acorn.api.models.PendingActionDetail
import io.ycvk.acorn.api.models.PendingActionOption
import io.ycvk.acorn.api.models.PendingActionSummary

/**
 * Approvals tab: lists pending actions that need an operator decision. Tapping
 * an item opens a [ModalBottomSheet] with the full detail and one chip per
 * option, each issuing an accept/decline decision.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ApprovalsScreen(
    modifier: Modifier = Modifier,
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

    LazyColumn(modifier = modifier.fillMaxSize()) {
        items(pendingActions, key = { it.actionId }) { action ->
            PendingActionRow(action) { viewModel.loadActionDetail(action.actionId) }
            HorizontalDivider()
        }

        if (pendingActions.isEmpty() && error == null) {
            item {
                Box(
                    modifier = Modifier.fillMaxWidth().padding(32.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        "No pending approvals",
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }

        error?.let {
            item {
                Text(
                    it,
                    modifier = Modifier.padding(16.dp),
                    color = MaterialTheme.colorScheme.error,
                )
            }
        }
    }
}

@Composable
private fun PendingActionRow(action: PendingActionSummary, onClick: () -> Unit) {
    ListItem(
        headlineContent = { Text(action.title.ifBlank { "Untitled action" }) },
        supportingContent = {
            Column {
                Text(
                    action.kind.name,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                action.body?.takeIf { it.isNotBlank() }?.let {
                    Text(
                        it,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 2,
                    )
                }
            }
        },
        modifier = Modifier.clickable(onClick = onClick),
    )
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
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = sheetState,
    ) {
        Column(modifier = Modifier.fillMaxWidth().padding(horizontal = 24.dp).padding(bottom = 32.dp)) {
            Text(
                detail.title.ifBlank { "Untitled action" },
                style = MaterialTheme.typography.headlineSmall,
                fontWeight = FontWeight.SemiBold,
            )
            Spacer(Modifier.height(4.dp))
            Text(
                "${detail.kind.name} · ${detail.status.name}",
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )

            detail.body?.takeIf { it.isNotBlank() }?.let {
                Spacer(Modifier.height(16.dp))
                Text(it, style = MaterialTheme.typography.bodyMedium)
            }
            detail.reason?.takeIf { it.isNotBlank() }?.let {
                Spacer(Modifier.height(8.dp))
                Text(
                    "Reason: $it",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            detail.rule?.takeIf { it.isNotBlank() }?.let {
                Spacer(Modifier.height(4.dp))
                Text(
                    "Rule: $it",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }

            if (detail.options.isNotEmpty()) {
                Spacer(Modifier.height(16.dp))
                Text("Options", style = MaterialTheme.typography.titleSmall)
                Spacer(Modifier.height(8.dp))
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    detail.options.forEach { option ->
                        AssistChip(
                            onClick = { onDecide(detail.actionId, decideFor(option), option.id, null) },
                            label = { Text(option.label) },
                            enabled = !deciding,
                        )
                    }
                }
            }

            if (deciding) {
                Spacer(Modifier.height(16.dp))
                CircularProgressIndicator(
                    modifier = Modifier.align(Alignment.CenterHorizontally),
                )
            }
        }
    }
}

/**
 * Maps an option's label to the backend decision enum. Options whose label
 * indicates rejection map to [DecidePendingActionRequest.Decision.decline];
 * anything else maps to accept (an explicit option selection is the signal the
 * operator wants that option).
 */
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
