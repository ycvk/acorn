package io.ycvk.acorn.feature.memory

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import io.ycvk.acorn.api.models.PortraitEvidence
import io.ycvk.acorn.api.models.PortraitLine
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornIcons
import io.ycvk.acorn.core.ui.Hairline
import io.ycvk.acorn.core.ui.InlineError
import io.ycvk.acorn.core.ui.ListRow
import io.ycvk.acorn.core.ui.ScreenPadding
import io.ycvk.acorn.core.ui.SwipeToDelete
import io.ycvk.acorn.core.ui.ago
import io.ycvk.acorn.core.ui.groupItem
import java.time.OffsetDateTime

/** How a line's evidence reads under it: how many of the owner's words it rests on. */
fun portraitBasis(line: PortraitLine): String =
    if (line.evidence.isEmpty()) "依据已被忘掉" else "依据 ${line.evidence.size} 条你说过的话"

/**
 * The portrait above the memory list: what Acorn keeps in mind about the owner. A tap shows the
 * owner's words a line rests on; swiping a line asks to remove it.
 */
fun LazyListScope.portraitItems(
    section: PortraitSection,
    error: String?,
    expanded: Long?,
    now: OffsetDateTime,
    onToggle: (Long) -> Unit,
    onRemove: (PortraitLine) -> Unit,
    onOpenThread: (String) -> Unit,
    onDismissError: () -> Unit,
    onRetry: () -> Unit,
) {
    if (section is PortraitSection.Hidden) return
    item(key = "portrait_title") {
        ListTitle("我对你的了解", "Acorn 每晚从你说过的话里整理，回答前都会看。点开看依据，左滑删掉不对的。")
    }
    if (error != null) {
        item(key = "portrait_error") {
            InlineError(error, Modifier.padding(start = ScreenPadding, end = ScreenPadding, bottom = 8.dp), action = "知道了", onAction = onDismissError)
        }
    }
    when (section) {
        PortraitSection.Hidden -> Unit
        is PortraitSection.Failed -> item(key = "portrait_failed") {
            InlineError("画像没能载入：${section.message}", Modifier.padding(horizontal = ScreenPadding), action = "重试", onAction = onRetry)
        }
        is PortraitSection.Lines -> itemsIndexed(section.lines, key = { _, it -> "portrait_${it.id}" }) { index, line ->
            Column(Modifier.animateItem().groupItem(index, section.lines.size)) {
                SwipeToDelete(onDelete = { onRemove(line) }) {
                    PortraitRow(line, expanded == line.id, now, onClick = { onToggle(line.id) }, onOpenThread = onOpenThread)
                }
                if (index < section.lines.lastIndex) Hairline(inset = ScreenPadding)
            }
        }
    }
    item(key = "memories_title") { ListTitle("记忆", null) }
}

@Composable
private fun ListTitle(title: String, body: String?) {
    val colors = AcornTheme.colors
    Column(
        modifier = Modifier.fillMaxWidth().padding(start = ScreenPadding, end = ScreenPadding, top = 4.dp, bottom = 10.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Text(title, style = AcornTheme.type.footnote, color = colors.inkFaint)
        if (body != null) Text(body, style = AcornTheme.type.caption, color = colors.inkSoft)
    }
}

@Composable
private fun PortraitRow(line: PortraitLine, expanded: Boolean, now: OffsetDateTime, onClick: () -> Unit, onOpenThread: (String) -> Unit) {
    val colors = AcornTheme.colors
    ListRow(onClick = onClick) {
        Text(line.content, style = AcornTheme.type.body, color = colors.ink)
        Text(portraitBasis(line), style = AcornTheme.type.caption, color = colors.inkFaint, modifier = Modifier.padding(top = 2.dp))
        if (expanded) {
            Column(modifier = Modifier.padding(top = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                line.evidence.forEach { EvidenceLine(it, now, onOpenThread) }
            }
        }
    }
}

@Composable
private fun EvidenceLine(evidence: PortraitEvidence, now: OffsetDateTime, onOpenThread: (String) -> Unit) {
    val colors = AcornTheme.colors
    val open = evidence.threadId?.let { thread -> { onOpenThread(thread) } }
    Row(
        modifier = Modifier.fillMaxWidth().then(if (open != null) Modifier.clickable(onClick = open) else Modifier),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text("“${evidence.excerpt}”", style = AcornTheme.type.reading, color = colors.inkSoft)
            Text(ago(evidence.recordedAt, now), style = AcornTheme.type.caption, color = colors.inkFaint)
        }
        if (open != null) {
            Icon(AcornIcons.ChevronRight, contentDescription = "打开对话", tint = colors.inkFaint, modifier = Modifier.size(18.dp))
        }
    }
}
