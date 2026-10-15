package io.ycvk.acorn.core.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme

enum class DiffKind { Same, Added, Removed }

data class DiffLine(val kind: DiffKind, val text: String)

/** The lines that turn [before] into [after], by longest common subsequence; unchanged lines stay in order. */
fun lineDiff(before: String, after: String): List<DiffLine> {
    val old = before.lines()
    val new = after.lines()
    // common[i][j] is the length of the longest common subsequence of old[i:] and new[j:].
    val common = Array(old.size + 1) { IntArray(new.size + 1) }
    for (i in old.indices.reversed()) {
        for (j in new.indices.reversed()) {
            common[i][j] = if (old[i] == new[j]) common[i + 1][j + 1] + 1 else maxOf(common[i + 1][j], common[i][j + 1])
        }
    }
    val lines = mutableListOf<DiffLine>()
    var i = 0
    var j = 0
    while (i < old.size && j < new.size) {
        when {
            old[i] == new[j] -> {
                lines += DiffLine(DiffKind.Same, old[i])
                i++
                j++
            }
            common[i + 1][j] >= common[i][j + 1] -> lines += DiffLine(DiffKind.Removed, old[i++])
            else -> lines += DiffLine(DiffKind.Added, new[j++])
        }
    }
    while (i < old.size) lines += DiffLine(DiffKind.Removed, old[i++])
    while (j < new.size) lines += DiffLine(DiffKind.Added, new[j++])
    return lines
}

/** Changed lines marked and tinted, unchanged ones plain, in reading order. */
@Composable
fun DiffView(lines: List<DiffLine>, modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    Column(modifier = modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        lines.forEach { line ->
            val (mark, ink, fill) = when (line.kind) {
                DiffKind.Same -> Triple(" ", colors.inkSoft, Color.Transparent)
                DiffKind.Added -> Triple("+", colors.ink, colors.accentSoft)
                DiffKind.Removed -> Triple("−", colors.inkSoft, colors.dangerSoft)
            }
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .background(fill, AcornShapes.small)
                    .padding(horizontal = 8.dp, vertical = 3.dp),
            ) {
                Text(mark, style = AcornTheme.type.mono, color = ink, modifier = Modifier.width(14.dp))
                Text(line.text.ifEmpty { " " }, style = AcornTheme.type.callout, color = ink)
            }
        }
    }
}

/** One entry in a revision history: its number, who made it and when, and whether it is in use. */
@Composable
fun RevisionRow(title: String, meta: String, current: Boolean, onClick: () -> Unit) {
    ListRow(
        onClick = onClick,
        trailing = if (current) {
            { Tag("当前", color = AcornTheme.colors.accentInk, container = AcornTheme.colors.accentSoft) }
        } else {
            null
        },
    ) {
        Text(title, style = AcornTheme.type.callout, color = AcornTheme.colors.ink)
        Text(meta, style = AcornTheme.type.caption, color = AcornTheme.colors.inkFaint)
    }
}
