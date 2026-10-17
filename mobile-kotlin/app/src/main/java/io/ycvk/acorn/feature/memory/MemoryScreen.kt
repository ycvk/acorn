package io.ycvk.acorn.feature.memory

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.MemoryDetail
import io.ycvk.acorn.api.models.MemoryEvidence
import io.ycvk.acorn.api.models.MemorySummary
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornButton
import io.ycvk.acorn.core.ui.AcornDialog
import io.ycvk.acorn.core.ui.AcornIcons
import io.ycvk.acorn.core.ui.ButtonKind
import io.ycvk.acorn.core.ui.ErrorState
import io.ycvk.acorn.core.ui.InlineError
import io.ycvk.acorn.core.ui.ScreenPadding
import io.ycvk.acorn.core.ui.SkeletonRows
import io.ycvk.acorn.core.ui.TopBar
import io.ycvk.acorn.core.ui.ago
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/**
 * One memory: what Acorn holds, the quotes it rests on, what it was drawn
 * from and how it read before. Facts can be corrected; any memory can be
 * forgotten.
 */
@Composable
fun MemoryScreen(
    id: String,
    viewModel: MemoryViewModel,
    onBack: () -> Unit,
    onOpenMemory: (String) -> Unit,
    onReplaceMemory: (String) -> Unit,
    onOpenThread: (String) -> Unit,
    onOpenNote: (String) -> Unit,
) {
    val load by viewModel.memory.collectAsStateWithLifecycle()
    val change by viewModel.change.collectAsStateWithLifecycle()
    LaunchedEffect(id) { viewModel.open(id) }
    val scroll = rememberScrollState()
    var confirmForget by rememberSaveable { mutableStateOf(false) }
    var editing by rememberSaveable { mutableStateOf<String?>(null) }
    val detail = (load as? MemoryLoad.Loaded)?.detail?.takeIf { it.memory.id == id }

    Column(modifier = Modifier.fillMaxSize().background(AcornTheme.colors.canvas)) {
        TopBar(onBack = onBack, title = detail?.let { kindLabel(it.memory.kind) }, showDivider = scroll.value > 0)
        when (val current = load) {
            MemoryLoad.Loading -> SkeletonRows(rows = 4)
            is MemoryLoad.Failed -> ErrorState(current.message, onRetry = { viewModel.open(id) })
            is MemoryLoad.Loaded -> if (detail == null) {
                SkeletonRows(rows = 4)
            } else {
                Column(modifier = Modifier.weight(1f).verticalScroll(scroll)) {
                    MemoryBody(detail, onOpenMemory, onOpenThread, onOpenNote)
                }
                Column(
                    modifier = Modifier
                        .navigationBarsPadding()
                        .padding(horizontal = ScreenPadding, vertical = 12.dp),
                    verticalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    (change as? MemoryChange.Failed)?.let {
                        InlineError(it.message, action = "知道了", onAction = viewModel::dismissChangeError)
                    }
                    Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                        if (detail.memory.kind == MemorySummary.Kind.fact) {
                            AcornButton(
                                "修改",
                                { editing = detail.memory.content },
                                modifier = Modifier.weight(1f),
                                kind = ButtonKind.Secondary,
                                enabled = change != MemoryChange.Saving,
                            )
                        }
                        AcornButton(
                            "忘掉",
                            { confirmForget = true },
                            modifier = Modifier.weight(1f),
                            kind = ButtonKind.Danger,
                            loading = change == MemoryChange.Saving && editing == null,
                        )
                    }
                }
            }
        }
    }

    if (confirmForget && detail != null) {
        AcornDialog(
            title = "忘掉这条记忆？",
            body = forgetConsequence(detail.memory.kind),
            confirm = "忘掉",
            destructive = true,
            onConfirm = {
                confirmForget = false
                viewModel.forget(detail.memory.id, onDone = onBack)
            },
            onDismiss = { confirmForget = false },
        )
    }

    val draft = editing
    if (draft != null && detail != null) {
        val saving = change == MemoryChange.Saving
        AcornDialog(
            title = "修改这条事实",
            body = "用你自己的话写下正确的说法，Acorn 会以此为准，原来的说法留在历史里。",
            confirm = "保存",
            confirmEnabled = draft.isNotBlank() && draft.trim() != detail.memory.content && !saving,
            confirmLoading = saving,
            dismissEnabled = !saving,
            onConfirm = {
                viewModel.correct(detail.memory, draft) { newId ->
                    editing = null
                    onReplaceMemory(newId)
                }
            },
            onDismiss = { if (!saving) editing = null },
        ) {
            BasicTextField(
                value = draft,
                onValueChange = { editing = it },
                textStyle = AcornTheme.type.body.copy(color = AcornTheme.colors.ink),
                cursorBrush = SolidColor(AcornTheme.colors.accent),
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(min = 96.dp)
                    .clip(AcornShapes.medium)
                    .background(AcornTheme.colors.surfaceMuted)
                    .padding(14.dp),
            )
        }
    }
}

@Composable
private fun MemoryBody(
    detail: MemoryDetail,
    onOpenMemory: (String) -> Unit,
    onOpenThread: (String) -> Unit,
    onOpenNote: (String) -> Unit,
) {
    val colors = AcornTheme.colors
    val memory = detail.memory
    val now = OffsetDateTime.now()
    Column(modifier = Modifier.padding(start = ScreenPadding, end = ScreenPadding, top = 4.dp, bottom = 24.dp)) {
        memoryMarks(memory).takeIf { it.isNotEmpty() }?.let {
            Text(it.joinToString(" · "), style = AcornTheme.type.footnote, color = colors.accent)
            Spacer(Modifier.height(6.dp))
        }
        Text(memory.content, style = AcornTheme.type.title, color = colors.ink)
        Spacer(Modifier.height(10.dp))
        Text(memoryMeta(memory, now), style = AcornTheme.type.footnote, color = colors.inkFaint)
        memory.scope?.takeIf { it.isNotBlank() }?.let {
            Spacer(Modifier.height(4.dp))
            Text("适用于：$it", style = AcornTheme.type.footnote, color = colors.inkSoft)
        }

        if (detail.evidence.isNotEmpty()) {
            SectionTitle("依据")
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                detail.evidence.forEach { evidence ->
                    EvidenceQuote(evidence, now, onOpenThread, onOpenNote)
                }
            }
        }

        if (detail.basedOn.isNotEmpty()) {
            SectionTitle("推断自")
            detail.basedOn.forEach { parent ->
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(AcornShapes.small)
                        .clickable { onOpenMemory(parent.id) }
                        .padding(vertical = 10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(parent.content, style = AcornTheme.type.callout, color = colors.ink, modifier = Modifier.weight(1f), maxLines = 3, overflow = TextOverflow.Ellipsis)
                    Icon(AcornIcons.ChevronRight, contentDescription = null, tint = colors.inkFaint, modifier = Modifier.size(18.dp))
                }
            }
        }

        if (detail.earlier.isNotEmpty()) {
            SectionTitle("之前的说法")
            detail.earlier.forEach { earlier ->
                Column(modifier = Modifier.padding(vertical = 6.dp)) {
                    Text(earlier.content, style = AcornTheme.type.callout.copy(textDecoration = TextDecoration.LineThrough), color = colors.inkSoft)
                    Text(ago(earlier.updatedAt, now), style = AcornTheme.type.caption, color = colors.inkFaint)
                }
            }
        }
    }
}

@Composable
private fun EvidenceQuote(
    evidence: MemoryEvidence,
    now: OffsetDateTime,
    onOpenThread: (String) -> Unit,
    onOpenNote: (String) -> Unit,
) {
    val colors = AcornTheme.colors
    val open: (() -> Unit)? = evidence.notePath?.let { path -> { onOpenNote(path) } }
        ?: evidence.threadId?.let { thread -> { onOpenThread(thread) } }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(AcornShapes.small)
            .background(colors.surface)
            .border(0.5.dp, colors.line, AcornShapes.small)
            .then(if (open != null) Modifier.clickable(onClick = open) else Modifier)
            .padding(horizontal = 14.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text("“${evidence.quote}”", style = AcornTheme.type.reading, color = colors.ink)
            val relation = if (evidence.relation == MemoryEvidence.Relation.contradicts) "与之矛盾 · " else ""
            Text("$relation${evidenceSource(evidence)} · ${ago(evidence.recordedAt, now)}", style = AcornTheme.type.caption, color = colors.inkFaint)
        }
        if (open != null) {
            Icon(AcornIcons.ChevronRight, contentDescription = "打开来源", tint = colors.inkFaint, modifier = Modifier.size(18.dp))
        }
    }
}

@Composable
private fun SectionTitle(text: String) {
    Text(
        text,
        style = AcornTheme.type.footnote,
        color = AcornTheme.colors.inkFaint,
        modifier = Modifier.padding(top = 28.dp, bottom = 8.dp),
    )
}

private val validityDate = DateTimeFormatter.ofPattern("yyyy年M月d日")

/** When the memory last changed, and the period it holds for when it has one. */
fun memoryMeta(memory: MemorySummary, now: OffsetDateTime, zone: ZoneId = ZoneId.systemDefault()): String {
    val validity = when {
        memory.validFrom != null && memory.validTo != null ->
            "${memory.validFrom.atZoneSameInstant(zone).format(validityDate)}到${memory.validTo.atZoneSameInstant(zone).format(validityDate)}"
        memory.validTo != null -> "到${memory.validTo.atZoneSameInstant(zone).format(validityDate)}为止"
        memory.validFrom != null -> "从${memory.validFrom.atZoneSameInstant(zone).format(validityDate)}起"
        else -> null
    }
    return listOfNotNull("${ago(memory.updatedAt, now)}更新", validity).joinToString("  ·  ")
}
