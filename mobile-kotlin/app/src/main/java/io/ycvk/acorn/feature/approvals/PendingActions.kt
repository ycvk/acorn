package io.ycvk.acorn.feature.approvals

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.ycvk.acorn.api.models.DecidePendingActionRequest
import io.ycvk.acorn.api.models.PendingActionDetail
import io.ycvk.acorn.api.models.PendingActionOption
import io.ycvk.acorn.api.models.PendingActionSummary
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornButton
import io.ycvk.acorn.core.ui.ButtonKind
import io.ycvk.acorn.core.ui.Panel
import io.ycvk.acorn.core.ui.ago
import io.ycvk.acorn.core.ui.rememberHaptics
import io.ycvk.acorn.feature.chat.toolLabel
import java.time.OffsetDateTime

/** The decision an option sends; tool approvals carry accept/decline ids, questions carry their own labels. */
fun optionDecision(option: PendingActionOption): DecidePendingActionRequest.Decision {
    if (option.id == "accept") return DecidePendingActionRequest.Decision.accept
    if (option.id == "decline") return DecidePendingActionRequest.Decision.decline
    val label = option.label.lowercase()
    val isDecline = label.startsWith("reject") ||
        label.startsWith("decline") ||
        label.startsWith("no") ||
        label == "cancel"
    return if (isDecline) DecidePendingActionRequest.Decision.decline else DecidePendingActionRequest.Decision.accept
}

fun optionLabel(option: PendingActionOption): String = when (option.id) {
    "accept" -> "批准"
    "decline" -> "拒绝"
    else -> option.label
}

/** A tool approval reads as what Acorn wants to do; other actions keep their own title. */
fun approvalTitle(title: String, toolName: String?): String =
    toolName?.takeIf { it.isNotBlank() }?.let { "Acorn 想${toolLabel(it)}" } ?: title

/** A tool approval shows what the call is about; other actions show their message. */
fun approvalBody(body: String?, toolName: String?, toolSummary: String?): String? =
    if (toolName.isNullOrBlank()) body?.takeIf { it.isNotBlank() } else toolSummary?.takeIf { it.isNotBlank() }

fun kindLabel(kind: String): String = when (kind) {
    "tool_approval" -> "需要你批准"
    "operator_question" -> "Acorn 在问你"
    "elicitation" -> "需要你补充"
    else -> kind
}

/** A pending action on Today: two-option approvals decide in place, anything else opens the sheet. */
@Composable
fun ApprovalCard(
    action: PendingActionSummary,
    deciding: Boolean,
    onDecide: (PendingActionOption) -> Unit,
    onOpen: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val colors = AcornTheme.colors
    val haptics = rememberHaptics()
    Panel(
        modifier = modifier,
        color = colors.warningSoft,
        borderColor = colors.warningSoft,
        onClick = onOpen,
        contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 14.dp, bottom = 12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(kindLabel(action.kind.value), style = AcornTheme.type.caption, color = colors.warningInk, modifier = Modifier.weight(1f))
            Text(ago(action.createdAt, OffsetDateTime.now()), style = AcornTheme.type.caption, color = colors.warningInk)
        }
        Spacer(Modifier.height(6.dp))
        Text(approvalTitle(action.title, action.toolName), style = AcornTheme.type.headline, color = colors.ink, maxLines = 2, overflow = TextOverflow.Ellipsis)
        approvalBody(action.body, action.toolName, action.toolSummary)?.let {
            Spacer(Modifier.height(4.dp))
            Text(it, style = AcornTheme.type.footnote, color = colors.inkSoft, maxLines = 3, overflow = TextOverflow.Ellipsis)
        }
        Spacer(Modifier.height(12.dp))
        val inline = action.kind == PendingActionSummary.Kind.tool_approval && action.options.size == 2
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
            if (inline) {
                action.options.sortedBy { optionDecision(it) == DecidePendingActionRequest.Decision.accept }.forEach { option ->
                    val accept = optionDecision(option) == DecidePendingActionRequest.Decision.accept
                    AcornButton(
                        text = optionLabel(option),
                        onClick = {
                            if (accept) haptics.confirm() else haptics.reject()
                            onDecide(option)
                        },
                        kind = if (accept) ButtonKind.Primary else ButtonKind.Raised,
                        compact = true,
                        loading = deciding && accept,
                        enabled = !deciding,
                        modifier = Modifier.weight(1f),
                    )
                }
            } else {
                AcornButton("查看并回答", onOpen, kind = ButtonKind.Primary, compact = true)
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
fun PendingActionSheet(
    detail: PendingActionDetail,
    deciding: Boolean,
    onDismiss: () -> Unit,
    onDecide: (PendingActionOption) -> Unit,
) {
    val colors = AcornTheme.colors
    val haptics = rememberHaptics()
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = colors.surface,
        scrimColor = colors.scrim,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 24.dp)
                .padding(bottom = 28.dp),
        ) {
            Text(kindLabel(detail.kind.value), style = AcornTheme.type.footnote, color = colors.warningInk)
            Spacer(Modifier.height(8.dp))
            Text(approvalTitle(detail.title, detail.toolName), style = AcornTheme.type.title, color = colors.ink)
            approvalBody(detail.body, detail.toolName, detail.toolSummary)?.let {
                Spacer(Modifier.height(10.dp))
                Text(it, style = AcornTheme.type.reading, color = colors.ink)
            }
            // The whole text the call acts on, such as the skill a skill_write stores.
            detail.toolDetail?.takeIf { it.isNotBlank() }?.let { text ->
                Spacer(Modifier.height(16.dp))
                Text("完整内容", style = AcornTheme.type.caption, color = colors.inkFaint)
                Spacer(Modifier.height(4.dp))
                Text(text, style = AcornTheme.type.mono, color = colors.ink)
            }
            detail.reason?.takeIf { it.isNotBlank() }?.let { Detail("原因", it) }
            detail.rule?.takeIf { it.isNotBlank() }?.let { Detail("触发的规则", it) }
            if (detail.options.isNotEmpty()) {
                Spacer(Modifier.height(24.dp))
                // Decline sits left of accept, as on the Today card.
                val options = detail.options.sortedBy { optionDecision(it) == DecidePendingActionRequest.Decision.accept }
                val button: @Composable (PendingActionOption, Modifier) -> Unit = { option, modifier ->
                    val accept = optionDecision(option) == DecidePendingActionRequest.Decision.accept
                    AcornButton(
                        text = optionLabel(option),
                        onClick = {
                            if (accept) haptics.confirm() else haptics.reject()
                            onDecide(option)
                        },
                        kind = if (accept) ButtonKind.Primary else ButtonKind.Secondary,
                        enabled = !deciding,
                        modifier = modifier,
                    )
                }
                if (options.size <= 2) {
                    Row(horizontalArrangement = Arrangement.spacedBy(10.dp), modifier = Modifier.fillMaxWidth()) {
                        options.forEach { button(it, Modifier.weight(1f)) }
                    }
                } else {
                    FlowRow(
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                        verticalArrangement = Arrangement.spacedBy(8.dp),
                        modifier = Modifier.fillMaxWidth(),
                    ) {
                        options.forEach { button(it, Modifier) }
                    }
                }
                detail.options.mapNotNull { it.description?.takeIf(String::isNotBlank)?.let { d -> optionLabel(it) to d } }
                    .takeIf { it.isNotEmpty() }
                    ?.let { described ->
                        Spacer(Modifier.height(12.dp))
                        described.forEach { (label, description) ->
                            Text("$label：$description", style = AcornTheme.type.footnote, color = colors.inkSoft)
                        }
                    }
            }
        }
    }
}

@Composable
private fun Detail(label: String, text: String) {
    Spacer(Modifier.height(16.dp))
    Text(label, style = AcornTheme.type.caption, color = AcornTheme.colors.inkFaint)
    Spacer(Modifier.height(2.dp))
    Text(text, style = AcornTheme.type.footnote, color = AcornTheme.colors.inkSoft)
}
