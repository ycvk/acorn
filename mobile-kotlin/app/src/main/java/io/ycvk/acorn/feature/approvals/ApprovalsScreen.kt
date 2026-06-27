package io.ycvk.acorn.feature.approvals

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.ExperimentalLayoutApi
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
import androidx.compose.material3.AssistChip
import androidx.compose.material3.AssistChipDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.DecidePendingActionRequest
import io.ycvk.acorn.api.models.PendingActionDetail
import io.ycvk.acorn.api.models.PendingActionOption
import io.ycvk.acorn.api.models.PendingActionSummary
import io.ycvk.acorn.core.theme.Accent
import io.ycvk.acorn.core.theme.Bg
import io.ycvk.acorn.core.theme.Border
import io.ycvk.acorn.core.theme.Danger
import io.ycvk.acorn.core.theme.Surface
import io.ycvk.acorn.core.theme.SurfaceVariant
import io.ycvk.acorn.core.theme.TextPrimary
import io.ycvk.acorn.core.theme.TextSecondary
import io.ycvk.acorn.core.theme.Warning

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

    LazyColumn(
        modifier = modifier
            .fillMaxSize()
            .background(Bg),
        contentPadding = androidx.compose.foundation.layout.PaddingValues(
            horizontal = 12.dp,
            vertical = 8.dp,
        ),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        items(pendingActions, key = { it.actionId }) { action ->
            PendingActionCard(action) { viewModel.loadActionDetail(action.actionId) }
        }

        if (pendingActions.isEmpty() && error == null) {
            item {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(top = 120.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        "> no pending approvals",
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

@Composable
private fun PendingActionCard(action: PendingActionSummary, onClick: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .background(Surface)
            .border(
                width = 1.dp,
                color = Border,
                shape = RoundedCornerShape(4.dp),
            )
            .padding(12.dp),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            // Risk chip
            Box(
                modifier = Modifier
                    .background(Warning)
                    .padding(horizontal = 6.dp, vertical = 2.dp),
            ) {
                Text(
                    "RISK",
                    style = MaterialTheme.typography.labelSmall,
                    color = Bg,
                    fontWeight = FontWeight.Bold,
                )
            }
            Spacer(Modifier.width(8.dp))
            Text(
                action.kind.name,
                style = MaterialTheme.typography.labelSmall,
                color = TextSecondary,
            )
        }

        Spacer(Modifier.height(8.dp))

        Text(
            action.title.ifBlank { "untitled action" },
            style = MaterialTheme.typography.headlineSmall,
            color = TextPrimary,
        )

        action.body?.takeIf { it.isNotBlank() }?.let {
            Spacer(Modifier.height(4.dp))
            Text(
                it,
                style = MaterialTheme.typography.bodyMedium,
                color = TextSecondary,
                maxLines = 2,
            )
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
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = sheetState,
        containerColor = Surface,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 24.dp)
                .padding(bottom = 32.dp),
        ) {
            // Risk label
            Box(
                modifier = Modifier
                    .background(Warning)
                    .padding(horizontal = 6.dp, vertical = 2.dp),
            ) {
                Text(
                    "RISK",
                    style = MaterialTheme.typography.labelSmall,
                    color = Bg,
                    fontWeight = FontWeight.Bold,
                )
            }

            Spacer(Modifier.height(8.dp))
            Text(
                detail.title.ifBlank { "untitled action" },
                style = MaterialTheme.typography.headlineSmall,
                color = TextPrimary,
                fontWeight = FontWeight.SemiBold,
            )
            Spacer(Modifier.height(4.dp))
            Text(
                "${detail.kind.name} · ${detail.status.name}",
                style = MaterialTheme.typography.labelSmall,
                color = TextSecondary,
            )

            detail.body?.takeIf { it.isNotBlank() }?.let {
                Spacer(Modifier.height(16.dp))
                Text(
                    it,
                    style = MaterialTheme.typography.bodyMedium,
                    color = TextPrimary,
                )
            }
            detail.reason?.takeIf { it.isNotBlank() }?.let {
                Spacer(Modifier.height(8.dp))
                Text(
                    "reason: $it",
                    style = MaterialTheme.typography.bodySmall,
                    color = TextSecondary,
                )
            }
            detail.rule?.takeIf { it.isNotBlank() }?.let {
                Spacer(Modifier.height(4.dp))
                Text(
                    "rule: $it",
                    style = MaterialTheme.typography.bodySmall,
                    color = TextSecondary,
                )
            }

            if (detail.options.isNotEmpty()) {
                Spacer(Modifier.height(16.dp))
                Text(
                    "options",
                    style = MaterialTheme.typography.labelSmall,
                    color = TextSecondary,
                )
                Spacer(Modifier.height(8.dp))
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    detail.options.forEach { option ->
                        val isDecline = decideFor(option) == DecidePendingActionRequest.Decision.decline
                        AssistChip(
                            onClick = { onDecide(detail.actionId, decideFor(option), option.id, null) },
                            label = { Text(option.label, style = MaterialTheme.typography.labelLarge) },
                            enabled = !deciding,
                            colors = AssistChipDefaults.assistChipColors(
                                containerColor = if (isDecline) SurfaceVariant else Accent,
                                labelColor = if (isDecline) Danger else Bg,
                            ),
                        )
                    }
                }
            }

            if (deciding) {
                Spacer(Modifier.height(16.dp))
                CircularProgressIndicator(
                    modifier = Modifier.align(Alignment.CenterHorizontally),
                    color = Accent,
                )
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
