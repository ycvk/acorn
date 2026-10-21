package io.ycvk.acorn.feature.views

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import coil3.compose.SubcomposeAsyncImage
import coil3.network.NetworkHeaders
import coil3.network.httpHeaders
import coil3.request.ImageRequest
import coil3.request.crossfade
import io.ycvk.acorn.api.models.View
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornIcons
import io.ycvk.acorn.core.ui.Hairline
import io.ycvk.acorn.core.ui.Markdown
import io.ycvk.acorn.core.ui.hairline
import io.ycvk.acorn.feature.knowledge.AttachmentImage

/** The views a reply showed, one card each. */
@Composable
fun ViewCards(views: List<View>, threadId: String?, profile: ConnectionProfile?, modifier: Modifier = Modifier) {
    if (views.isEmpty()) return
    Column(modifier = modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        views.forEach { ViewCard(it, threadId, profile) }
    }
}

/** A view the agent showed: its title over its components, drawn with the app's own components. */
@Composable
fun ViewCard(view: View, threadId: String?, profile: ConnectionProfile?, modifier: Modifier = Modifier) {
    val tree = remember(view.id) { viewTree(view) }
    val colors = AcornTheme.colors
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(AcornShapes.card)
            .background(colors.surface)
            .hairline(AcornShapes.card)
            .padding(horizontal = 14.dp, vertical = 12.dp),
    ) {
        Text(tree.title, style = AcornTheme.type.footnote.copy(fontWeight = FontWeight.SemiBold), color = colors.inkSoft)
        Spacer(Modifier.height(10.dp))
        tree.root?.let { Component(Drawing(tree, threadId, profile), it, emptyList()) }
    }
}

/** What drawing one view needs besides its components. */
private class Drawing(val tree: ViewTree, val threadId: String?, val profile: ConnectionProfile?)

@Composable
private fun Component(drawing: Drawing, component: ViewComponent, scopes: List<Any?>) {
    val tree = drawing.tree
    val colors = AcornTheme.colors
    when (component.type) {
        "Column" -> Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
            tree.children(component).forEach { Component(drawing, it, scopes) }
        }
        "Row" -> FlowRow(
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
            itemVerticalAlignment = Alignment.CenterVertically,
        ) {
            tree.children(component).forEach { Component(drawing, it, scopes) }
        }
        "Card" -> Column(
            modifier = Modifier
                .fillMaxWidth()
                .clip(AcornShapes.medium)
                .background(colors.well)
                .padding(12.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            tree.children(component).forEach { Component(drawing, it, scopes) }
        }
        "Divider" -> Hairline()
        "Text" -> tree.text(component, "text", scopes)?.let { text ->
            when (component.props["variant"]) {
                "title" -> Markdown(text, style = AcornTheme.type.headline)
                "caption" -> Markdown(text, style = AcornTheme.type.caption.copy(color = colors.inkSoft))
                else -> Markdown(text, style = AcornTheme.type.body)
            }
        }
        "Badge" -> tree.text(component, "text", scopes)?.let { Badge(it, component.props["tone"] as? String) }
        "Link" -> {
            val label = tree.text(component, "label", scopes)
            val url = tree.text(component, "url", scopes)
            if (label != null && url != null) ViewLink(label, url)
        }
        "Image" -> tree.text(component, "file", scopes)?.let { ref ->
            ViewImage(ref, tree.text(component, "alt", scopes).orEmpty(), drawing)
        }
        "Table" -> {
            val columns = (component.props["columns"] as? List<*>).orEmpty().map { it as? String ?: "" }
            ViewTable(columns, tableRows(tree.list(component, "rows", scopes), columns.size))
        }
        "List" -> {
            val template = tree.component(component.props["template"] as? String)
            if (template != null) {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    tree.list(component, "items", scopes).forEach { item -> Component(drawing, template, scopes + listOf(item)) }
                }
            }
        }
        "Chart" -> {
            val points = chartPoints(tree.list(component, "points", scopes))
            val unit = tree.text(component, "unit", scopes)
            if (component.props["variant"] == "line") LineChart(points, unit) else BarChart(points, unit)
        }
        else -> Text("这个版本的 App 显示不了这部分", style = AcornTheme.type.caption, color = colors.inkFaint)
    }
}

@Composable
private fun Badge(text: String, tone: String?) {
    val colors = AcornTheme.colors
    val (background, ink) = when (tone) {
        "accent" -> colors.accentSoft to colors.accentInk
        "success" -> colors.success.copy(alpha = 0.14f) to colors.success
        "warning" -> colors.warningSoft to colors.warningInk
        "danger" -> colors.dangerSoft to colors.danger
        else -> colors.surfaceMuted to colors.inkSoft
    }
    Text(
        text,
        style = AcornTheme.type.caption.copy(fontWeight = FontWeight.Medium),
        color = ink,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
        modifier = Modifier
            .clip(AcornShapes.pill)
            .background(background)
            .padding(horizontal = 9.dp, vertical = 3.dp),
    )
}

@Composable
private fun ViewLink(label: String, url: String) {
    val uriHandler = LocalUriHandler.current
    val colors = AcornTheme.colors
    Row(
        modifier = Modifier
            .clip(AcornShapes.small)
            .clickable { uriHandler.openUri(url) }
            .padding(vertical = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(label, style = AcornTheme.type.callout.copy(fontWeight = FontWeight.Medium), color = colors.accent)
        Icon(AcornIcons.ChevronRight, contentDescription = null, tint = colors.accent, modifier = Modifier.size(16.dp))
    }
}

@Composable
private fun ViewImage(ref: String, alt: String, drawing: Drawing) {
    val profile = drawing.profile ?: return
    if (ref.startsWith("attachments/")) {
        AttachmentImage(ref, alt, profile)
        return
    }
    val threadId = drawing.threadId ?: return
    val context = LocalContext.current
    val request = remember(threadId, ref, profile) {
        ImageRequest.Builder(context)
            .data("${profile.serverUrl.trimEnd('/')}/v1/threads/$threadId/files/$ref")
            .httpHeaders(NetworkHeaders.Builder().set("Authorization", "Bearer ${profile.accessToken}").build())
            .crossfade(true)
            .build()
    }
    SubcomposeAsyncImage(
        model = request,
        contentDescription = alt,
        contentScale = ContentScale.FillWidth,
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(max = 360.dp)
            .clip(AcornShapes.medium),
        loading = { ImageNote(alt.ifBlank { "图片" }) },
        error = { ImageNote("图片没能加载") },
    )
}

@Composable
private fun ImageNote(text: String) {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .height(120.dp)
            .background(AcornTheme.colors.surfaceMuted),
        contentAlignment = Alignment.Center,
    ) {
        Text(text, style = AcornTheme.type.caption, color = AcornTheme.colors.inkFaint)
    }
}

/**
 * Columns sized to their text: a narrower table stretches to the card, a
 * wider one first wraps its widest columns and scrolls sideways only when
 * that is not enough.
 */
@Composable
private fun ViewTable(columns: List<String>, rows: List<List<String>>) {
    if (columns.isEmpty()) return
    val colors = AcornTheme.colors
    BoxWithConstraints(
        modifier = Modifier
            .fillMaxWidth()
            .clip(AcornShapes.medium)
            .hairline(AcornShapes.medium),
    ) {
        val natural = columns.indices.map { index -> columnWidth(listOf(columns[index]) + rows.map { it[index] }).value }
        val widths = fitColumns(natural, maxWidth.value).map { it.dp }
        Column(modifier = Modifier.horizontalScroll(rememberScrollState())) {
            Row(modifier = Modifier.background(colors.well)) {
                columns.forEachIndexed { index, heading -> Cell(heading, widths[index], header = true) }
            }
            rows.forEach { row ->
                Hairline()
                Row { row.forEachIndexed { index, cell -> Cell(cell, widths[index], header = false) } }
            }
        }
    }
}

@Composable
private fun Cell(text: String, width: Dp, header: Boolean) {
    Text(
        text,
        style = if (header) AcornTheme.type.caption.copy(fontWeight = FontWeight.SemiBold) else AcornTheme.type.footnote,
        color = if (header) AcornTheme.colors.inkSoft else AcornTheme.colors.ink,
        modifier = Modifier
            .width(width)
            .padding(horizontal = 10.dp, vertical = 8.dp),
    )
}

/**
 * Column widths for a table [available] wide, from the widths their text
 * wants: stretched when they leave room, otherwise the columns wider than
 * [MIN_WRAPPED_COLUMN] give up their extra in proportion until the table fits
 * or they are that narrow.
 */
fun fitColumns(natural: List<Float>, available: Float): List<Float> {
    val total = natural.sum()
    if (total <= available) return natural.map { it * available / total }
    val spare = natural.sumOf { (it - MIN_WRAPPED_COLUMN).coerceAtLeast(0f).toDouble() }.toFloat()
    if (spare <= 0f) return natural
    val cut = (total - available).coerceAtMost(spare)
    return natural.map { width -> width - (width - MIN_WRAPPED_COLUMN).coerceAtLeast(0f) * cut / spare }
}

/** How narrow a column of long text gets before the table scrolls instead. */
const val MIN_WRAPPED_COLUMN = 120f

/** A column wide enough for its longest line, within limits; CJK characters count double. */
fun columnWidth(texts: List<String>): Dp {
    val longest = texts.maxOfOrNull { text -> text.sumOf { if (it.code >= 0x2E80) 2 else 1 }.toInt() } ?: 0
    return (longest * 7 + 20).coerceIn(72, 240).dp
}

/** Bars side by side with their labels and values, scaled to the largest value. */
@Composable
private fun BarChart(points: List<ChartPoint>, unit: String?) {
    val colors = AcornTheme.colors
    val largest = points.maxOfOrNull { it.value }?.takeIf { it > 0 } ?: 1.0
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        points.forEach { point ->
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    point.label,
                    style = AcornTheme.type.caption,
                    color = colors.inkSoft,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.width(88.dp),
                )
                Box(modifier = Modifier.weight(1f).height(14.dp)) {
                    Box(
                        modifier = Modifier
                            .fillMaxHeight()
                            .fillMaxWidth((point.value / largest).coerceIn(0.0, 1.0).toFloat())
                            .clip(AcornShapes.small)
                            .background(colors.accent),
                    )
                }
                Text(
                    valueLabel(point.value, unit),
                    style = AcornTheme.type.caption.copy(fontWeight = FontWeight.Medium),
                    color = colors.ink,
                    modifier = Modifier.padding(start = 8.dp),
                )
            }
        }
    }
}

/** A line through the points, with the highest and lowest values and the first and last labels. */
@Composable
private fun LineChart(points: List<ChartPoint>, unit: String?) {
    if (points.isEmpty()) return
    val colors = AcornTheme.colors
    val high = points.maxOf { it.value }
    val low = points.minOf { it.value }
    val stroke = 2.dp
    Column {
        Text(valueLabel(high, unit), style = AcornTheme.type.caption, color = colors.inkFaint)
        Canvas(
            modifier = Modifier
                .fillMaxWidth()
                .height(120.dp)
                .padding(vertical = 6.dp),
        ) {
            val span = (high - low).takeIf { it > 0 } ?: 1.0
            val step = if (points.size > 1) size.width / (points.size - 1) else 0f
            val offsets = points.mapIndexed { index, point ->
                val x = if (points.size > 1) index * step else size.width / 2
                Offset(x, (size.height * (1 - (point.value - low) / span)).toFloat())
            }
            val path = Path().apply {
                offsets.forEachIndexed { index, offset -> if (index == 0) moveTo(offset.x, offset.y) else lineTo(offset.x, offset.y) }
            }
            drawPath(path, colors.accent, style = Stroke(width = stroke.toPx(), cap = StrokeCap.Round, join = StrokeJoin.Round))
            offsets.forEach { drawCircle(colors.accent, radius = 3.dp.toPx(), center = it) }
        }
        Text(valueLabel(low, unit), style = AcornTheme.type.caption, color = colors.inkFaint)
        Row(modifier = Modifier.fillMaxWidth().padding(top = 4.dp), horizontalArrangement = Arrangement.SpaceBetween) {
            Text(points.first().label, style = AcornTheme.type.caption, color = colors.inkSoft)
            if (points.size > 1) Text(points.last().label, style = AcornTheme.type.caption, color = colors.inkSoft)
        }
    }
}

fun valueLabel(value: Double, unit: String?): String = displayText(value).orEmpty() + (unit?.let { " $it" } ?: "")
