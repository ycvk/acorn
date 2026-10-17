package io.ycvk.acorn.feature.approvals

import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.ycvk.acorn.core.ui.Status
import io.ycvk.acorn.core.ui.StatusGlyph
import java.time.OffsetDateTime
import io.ycvk.acorn.api.models.DecidePendingActionRequest
import io.ycvk.acorn.api.models.PendingActionDetail
import io.ycvk.acorn.api.models.PendingActionOption
import io.ycvk.acorn.api.models.PendingActionSummary
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornButton
import io.ycvk.acorn.core.ui.ButtonKind
import io.ycvk.acorn.core.ui.InlineError
import io.ycvk.acorn.core.ui.Panel
import io.ycvk.acorn.core.ui.SheetHandle
import io.ycvk.acorn.core.ui.SheetShape
import io.ycvk.acorn.core.ui.ago
import io.ycvk.acorn.core.ui.rememberHaptics
import io.ycvk.acorn.feature.chat.toolLabel

/**
 * What choosing an option sends. A tool approval or elicitation decides by the
 * option's id, accept or decline; a question answers with the option it names.
 */
fun optionRequest(kind: String, option: PendingActionOption): DecidePendingActionRequest {
    if (kind == "operator_question") {
        return DecidePendingActionRequest(decision = DecidePendingActionRequest.Decision.answer, selectedOptionId = option.id)
    }
    val decision = when (option.id) {
        "accept" -> DecidePendingActionRequest.Decision.accept
        "decline" -> DecidePendingActionRequest.Decision.decline
        else -> error("$kind option ${option.id} is neither accept nor decline")
    }
    return DecidePendingActionRequest(decision = decision, selectedOptionId = option.id)
}

/** A question that allows freeform answers takes the owner's own words. */
fun freeformAnswer(text: String): DecidePendingActionRequest =
    DecidePendingActionRequest(decision = DecidePendingActionRequest.Decision.answer, answer = text.trim())

/** Declining a question names no option and carries no answer. */
val declineQuestion = DecidePendingActionRequest(decision = DecidePendingActionRequest.Decision.decline)

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
    onDecide: (DecidePendingActionRequest) -> Unit,
    onOpen: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val colors = AcornTheme.colors
    val haptics = rememberHaptics()
    Panel(
        modifier = modifier,
        onClick = onOpen,
        contentPadding = PaddingValues(14.dp),
    ) {
        Row {
            StatusGlyph(Status.Waiting, Modifier.padding(top = 2.dp))
            Spacer(Modifier.width(12.dp))
            Column(modifier = Modifier.weight(1f)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(kindLabel(action.kind.value), style = AcornTheme.type.eyebrow, color = colors.warningInk, modifier = Modifier.weight(1f))
                    Text(ago(action.createdAt, OffsetDateTime.now()), style = AcornTheme.type.caption, color = colors.inkFaint)
                }
                Spacer(Modifier.height(4.dp))
                Text(approvalTitle(action.title, action.toolName), style = AcornTheme.type.headline.copy(fontWeight = FontWeight.SemiBold), color = colors.ink, maxLines = 2, overflow = TextOverflow.Ellipsis)
                approvalBody(action.body, action.toolName, action.toolSummary)?.let {
                    Spacer(Modifier.height(2.dp))
                    Text(it, style = AcornTheme.type.footnote, color = colors.inkSoft, maxLines = 3, overflow = TextOverflow.Ellipsis)
                }
                Spacer(Modifier.height(12.dp))
                val inline = action.kind == PendingActionSummary.Kind.tool_approval && action.options.size == 2
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End), modifier = Modifier.fillMaxWidth()) {
                    if (inline) {
                        action.options.sortedBy { it.id == "accept" }.forEach { option ->
                            val accept = option.id == "accept"
                            AcornButton(
                                text = optionLabel(option),
                                onClick = {
                                    if (accept) haptics.confirm() else haptics.reject()
                                    onDecide(optionRequest(action.kind.value, option))
                                },
                                kind = if (accept) ButtonKind.Primary else ButtonKind.Secondary,
                                compact = true,
                                loading = deciding && accept,
                                enabled = !deciding,
                            )
                        }
                    } else {
                        AcornButton("查看并回答", onOpen, kind = ButtonKind.Primary, compact = true)
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PendingActionSheet(
    detail: PendingActionDetail,
    deciding: Boolean,
    error: String?,
    onDismiss: () -> Unit,
    onDismissError: () -> Unit,
    onDecide: (DecidePendingActionRequest) -> Unit,
) {
    val colors = AcornTheme.colors
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        shape = SheetShape,
        containerColor = colors.surface,
        scrimColor = colors.scrim,
        dragHandle = { SheetHandle() },
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .imePadding()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 24.dp)
                .padding(bottom = 28.dp),
        ) {
            Text(kindLabel(detail.kind.value), style = AcornTheme.type.eyebrow, color = colors.warningInk)
            Spacer(Modifier.height(6.dp))
            Text(approvalTitle(detail.title, detail.toolName), style = AcornTheme.type.title, color = colors.ink)
            approvalBody(detail.body, detail.toolName, detail.toolSummary)?.let {
                Spacer(Modifier.height(10.dp))
                Text(it, style = AcornTheme.type.body, color = colors.ink)
            }
            // The whole text the call acts on, such as the skill a skill_write or skill_import stores.
            detail.toolDetail?.takeIf { it.isNotBlank() }?.let { text ->
                Spacer(Modifier.height(16.dp))
                Text("完整内容", style = AcornTheme.type.caption, color = colors.inkFaint)
                Spacer(Modifier.height(4.dp))
                Text(text, style = AcornTheme.type.mono, color = colors.ink)
            }
            detail.reason?.takeIf { it.isNotBlank() }?.let { Detail("原因", it) }
            detail.rule?.takeIf { it.isNotBlank() }?.let { Detail("触发的规则", it) }
            Spacer(Modifier.height(24.dp))
            // A failed decision shows where it was made; the sheet stays open so a typed answer survives.
            error?.let {
                InlineError(it, action = "知道了", onAction = onDismissError)
                Spacer(Modifier.height(12.dp))
            }
            if (detail.kind == PendingActionDetail.Kind.operator_question) {
                QuestionAnswers(detail, deciding, onDecide)
            } else {
                AcceptDecline(detail, deciding, onDecide)
            }
        }
    }
}

/** Decline sits left of accept, as on the Today card. */
@Composable
private fun AcceptDecline(detail: PendingActionDetail, deciding: Boolean, onDecide: (DecidePendingActionRequest) -> Unit) {
    val haptics = rememberHaptics()
    Row(horizontalArrangement = Arrangement.spacedBy(10.dp), modifier = Modifier.fillMaxWidth()) {
        detail.options.sortedBy { it.id == "accept" }.forEach { option ->
            val accept = option.id == "accept"
            AcornButton(
                text = optionLabel(option),
                onClick = {
                    if (accept) haptics.confirm() else haptics.reject()
                    onDecide(optionRequest(detail.kind.value, option))
                },
                kind = if (accept) ButtonKind.Primary else ButtonKind.Secondary,
                enabled = !deciding,
                modifier = Modifier.weight(1f),
            )
        }
    }
}

/** A question is answered with one of its options or, when it allows, in the owner's own words; it can also go unanswered. */
@Composable
private fun QuestionAnswers(detail: PendingActionDetail, deciding: Boolean, onDecide: (DecidePendingActionRequest) -> Unit) {
    val colors = AcornTheme.colors
    val haptics = rememberHaptics()
    var answer by rememberSaveable(detail.actionId) { mutableStateOf("") }
    Column(verticalArrangement = Arrangement.spacedBy(10.dp), modifier = Modifier.fillMaxWidth()) {
        detail.options.forEach { option ->
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                AcornButton(
                    text = option.label,
                    onClick = {
                        haptics.confirm()
                        onDecide(optionRequest(detail.kind.value, option))
                    },
                    kind = ButtonKind.Secondary,
                    enabled = !deciding,
                    modifier = Modifier.fillMaxWidth(),
                )
                option.description?.takeIf(String::isNotBlank)?.let {
                    Text(it, style = AcornTheme.type.footnote, color = colors.inkSoft, modifier = Modifier.padding(horizontal = 4.dp))
                }
            }
        }
        if (detail.allowFreeform == true) {
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(min = 72.dp)
                    .clip(AcornShapes.small)
                    .border(0.5.dp, colors.lineStrong, AcornShapes.small)
                    .padding(horizontal = 14.dp, vertical = 12.dp),
            ) {
                if (answer.isEmpty()) Text("写下你的回答", style = AcornTheme.type.callout, color = colors.inkFaint)
                BasicTextField(
                    value = answer,
                    onValueChange = { answer = it },
                    enabled = !deciding,
                    maxLines = 6,
                    textStyle = AcornTheme.type.callout.copy(color = colors.ink),
                    cursorBrush = SolidColor(colors.accent),
                    modifier = Modifier.fillMaxWidth(),
                )
            }
            AcornButton(
                text = "发送回答",
                onClick = {
                    haptics.confirm()
                    onDecide(freeformAnswer(answer))
                },
                enabled = !deciding && answer.isNotBlank(),
                modifier = Modifier.fillMaxWidth(),
            )
        }
        AcornButton(
            text = "不回答",
            onClick = {
                haptics.reject()
                onDecide(declineQuestion)
            },
            kind = ButtonKind.Quiet,
            enabled = !deciding,
            modifier = Modifier.fillMaxWidth(),
        )
    }
}

@Composable
private fun Detail(label: String, text: String) {
    Spacer(Modifier.height(16.dp))
    Text(label, style = AcornTheme.type.caption, color = AcornTheme.colors.inkFaint)
    Spacer(Modifier.height(2.dp))
    Text(text, style = AcornTheme.type.footnote, color = AcornTheme.colors.inkSoft)
}
