package io.ycvk.acorn.feature.views

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
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
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.toggleable
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
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
import io.ycvk.acorn.api.models.ViewActionSent
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornButton
import io.ycvk.acorn.core.ui.AcornIcons
import io.ycvk.acorn.core.ui.ButtonKind
import io.ycvk.acorn.core.ui.Hairline
import io.ycvk.acorn.core.ui.Markdown
import io.ycvk.acorn.core.ui.hairline
import io.ycvk.acorn.feature.knowledge.AttachmentImage

/**
 * How a view's buttons and choices reach the server. [idle] is false while a
 * run of the thread is going; [sending] is the [ViewActionPress.id] of the one
 * whose request is out. [picks] holds the options picked but not yet sent, by
 * [ViewActionPress.id], outside the message rows, which are rebuilt when a
 * view updates. [onPin] pins a view to the now page or takes it off; [pinning]
 * is the id of the view whose request is out.
 */
class ViewActions(
    val idle: Boolean,
    val sending: String?,
    val picks: Map<String, List<String>>,
    val onPick: (String, List<String>) -> Unit,
    val onSend: (ViewActionPress) -> Unit,
    val pinning: String?,
    val onPin: (View) -> Unit,
) {
    val canSend: Boolean get() = idle && sending == null

    fun isSending(press: ViewActionPress) = sending == press.id
}

/** The views a reply showed, one card each. */
@Composable
fun ViewCards(views: List<View>, threadId: String?, profile: ConnectionProfile?, actions: ViewActions, modifier: Modifier = Modifier) {
    if (views.isEmpty()) return
    Column(modifier = modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        views.forEach { ViewCard(it, threadId, profile, actions) }
    }
}

/**
 * A view the agent showed: its title and pin over its components, drawn with
 * the app's own components. As a panel on the now page it carries [meta] under
 * the title, and [onOpen] takes a tap on it to its conversation.
 */
@Composable
fun ViewCard(
    view: View,
    threadId: String?,
    profile: ConnectionProfile?,
    actions: ViewActions,
    modifier: Modifier = Modifier,
    meta: String? = null,
    onOpen: (() -> Unit)? = null,
) {
    // An updated view keeps its id, so the tree follows its whole content.
    val tree = remember(view) { viewTree(view) }
    val sent = remember(view) { sentActions(view) }
    val colors = AcornTheme.colors
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(AcornShapes.card)
            .background(colors.surface)
            .hairline(AcornShapes.card)
            .then(if (onOpen != null) Modifier.clickable(onClick = onOpen) else Modifier)
            .padding(start = 14.dp, end = 6.dp, top = 6.dp, bottom = 12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(modifier = Modifier.weight(1f)) {
                Text(tree.title, style = AcornTheme.type.footnote.copy(fontWeight = FontWeight.SemiBold), color = colors.inkSoft)
                meta?.let { Text(it, style = AcornTheme.type.caption, color = colors.inkFaint, maxLines = 1, overflow = TextOverflow.Ellipsis) }
            }
            PinButton(pinned = view.pinnedAt != null, busy = actions.pinning == view.id, onClick = { actions.onPin(view) })
        }
        Spacer(Modifier.height(4.dp))
        Box(modifier = Modifier.padding(end = 8.dp)) {
            tree.root?.let { Component(Drawing(view.id, tree, sent, actions, threadId, profile), it, Scope.ROOT) }
        }
    }
}

/** Pins a view to the now page, or takes it off when [pinned]. */
@Composable
private fun PinButton(pinned: Boolean, busy: Boolean, onClick: () -> Unit) {
    val colors = AcornTheme.colors
    Box(
        modifier = Modifier
            .size(36.dp)
            .clip(CircleShape)
            .clickable(enabled = !busy, role = Role.Button, onClick = onClick)
            .semantics { stateDescription = if (pinned) "已固定到今天" else "未固定" },
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            AcornIcons.Pin,
            contentDescription = if (pinned) "取消固定" else "固定到今天",
            tint = if (pinned) colors.accent else colors.inkFaint,
            modifier = Modifier.size(17.dp).alpha(if (busy) 0.4f else 1f),
        )
    }
}

/** What drawing one view needs besides its components. */
private class Drawing(
    val viewId: String,
    val tree: ViewTree,
    val sent: Map<String, ViewActionSent>,
    val actions: ViewActions,
    val threadId: String?,
    val profile: ConnectionProfile?,
)

/** The List items a component is drawn for, outermost first: the items themselves and their indices. */
private data class Scope(val items: List<Any?>, val indices: List<Int>) {
    fun enter(item: Any?, index: Int) = Scope(items + listOf(item), indices + index)

    companion object {
        val ROOT = Scope(emptyList(), emptyList())
    }
}

@Composable
private fun Component(drawing: Drawing, component: ViewComponent, scope: Scope) {
    val tree = drawing.tree
    val colors = AcornTheme.colors
    val scopes = scope.items
    when (component.type) {
        "Column" -> Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
            tree.children(component).forEach { Component(drawing, it, scope) }
        }
        "Row" -> FlowRow(
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
            itemVerticalAlignment = Alignment.CenterVertically,
        ) {
            tree.children(component).forEach { Component(drawing, it, scope) }
        }
        "Card" -> Column(
            modifier = Modifier
                .fillMaxWidth()
                .clip(AcornShapes.medium)
                .background(colors.well)
                .padding(12.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            tree.children(component).forEach { Component(drawing, it, scope) }
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
                    tree.list(component, "items", scopes).forEachIndexed { index, item -> Component(drawing, template, scope.enter(item, index)) }
                }
            }
        }
        "Chart" -> {
            val points = chartPoints(tree.list(component, "points", scopes))
            val unit = tree.text(component, "unit", scopes)
            if (component.props["variant"] == "line") LineChart(points, unit) else BarChart(points, unit)
        }
        "Button" -> tree.text(component, "label", scopes)?.let { label ->
            ViewButton(drawing, label, component.props["variant"] == "quiet", ViewActionPress(drawing.viewId, component.id, scope.indices))
        }
        "ChoicePicker" -> {
            val submit = tree.text(component, "submit", scopes)
            if (submit != null) {
                ChoicePicker(
                    drawing = drawing,
                    label = tree.text(component, "label", scopes),
                    options = choiceOptions(tree.list(component, "options", scopes)),
                    multiple = component.props["multiple"] == true,
                    submit = submit,
                    press = ViewActionPress(drawing.viewId, component.id, scope.indices),
                )
            }
        }
        else -> Text("这个版本的 App 显示不了这部分", style = AcornTheme.type.caption, color = colors.inkFaint)
    }
}

/** A button that sends its action once; afterwards it stays as a checked mark. */
@Composable
private fun ViewButton(drawing: Drawing, label: String, quiet: Boolean, press: ViewActionPress) {
    if (actionKey(press.componentId, press.scope) in drawing.sent) {
        SentMark(label)
        return
    }
    val actions = drawing.actions
    val sending = actions.isSending(press)
    AcornButton(
        text = label,
        onClick = { actions.onSend(press) },
        kind = if (quiet) ButtonKind.Quiet else ButtonKind.Primary,
        compact = true,
        loading = sending,
        enabled = sending || actions.canSend,
    )
}

/**
 * Options the owner picks, one or several, then sends with the submit button.
 * Once sent, the chosen options stay marked and the rest fade.
 */
@Composable
private fun ChoicePicker(drawing: Drawing, label: String?, options: List<ChoiceOption>, multiple: Boolean, submit: String, press: ViewActionPress) {
    val colors = AcornTheme.colors
    val sent = drawing.sent[actionKey(press.componentId, press.scope)]
    val actions = drawing.actions
    val sending = actions.isSending(press)
    val picked = actions.picks[press.id].orEmpty()
    val shown = sent?.selected ?: picked
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        label?.let { Text(it, style = AcornTheme.type.footnote.copy(fontWeight = FontWeight.Medium), color = colors.inkSoft) }
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            options.forEach { option ->
                OptionChip(
                    label = option.label,
                    picked = option.id in shown,
                    multiple = multiple,
                    enabled = sent == null && !sending,
                    faded = sent != null && option.id !in shown,
                    onToggle = { actions.onPick(press.id, toggleChoice(picked, option.id, multiple)) },
                )
            }
        }
        if (sent != null) {
            SentMark(submit)
        } else {
            AcornButton(
                text = submit,
                onClick = { actions.onSend(press.copy(selected = options.map { it.id }.filter { it in picked })) },
                compact = true,
                loading = sending,
                enabled = sending || (picked.isNotEmpty() && actions.canSend),
            )
        }
    }
}

@Composable
private fun OptionChip(label: String, picked: Boolean, multiple: Boolean, enabled: Boolean, faded: Boolean, onToggle: () -> Unit) {
    val colors = AcornTheme.colors
    val shape = AcornShapes.pill
    val selection = if (multiple) {
        Modifier.toggleable(value = picked, enabled = enabled, role = Role.Checkbox, onValueChange = { onToggle() })
    } else {
        Modifier.selectable(selected = picked, enabled = enabled, role = Role.RadioButton, onClick = onToggle)
    }
    Row(
        modifier = Modifier
            .alpha(if (faded) 0.45f else 1f)
            .clip(shape)
            .background(if (picked) colors.accentSoft else Color.Transparent)
            .then(if (picked) Modifier else Modifier.border(1.dp, colors.line, shape))
            .then(selection)
            .padding(horizontal = 12.dp, vertical = 7.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (picked) {
            Icon(AcornIcons.Check, contentDescription = null, tint = colors.accentInk, modifier = Modifier.size(14.dp))
            Spacer(Modifier.width(5.dp))
        }
        Text(label, style = AcornTheme.type.footnote.copy(fontWeight = FontWeight.Medium), color = if (picked) colors.accentInk else colors.ink)
    }
}

/** What the owner already sent from a view, checked. */
@Composable
private fun SentMark(text: String) {
    val colors = AcornTheme.colors
    Row(
        modifier = Modifier
            .height(34.dp)
            .clip(AcornShapes.pill)
            .background(colors.surfaceMuted)
            .semantics(mergeDescendants = true) { stateDescription = "已发送" }
            .padding(horizontal = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(AcornIcons.Check, contentDescription = null, tint = colors.inkSoft, modifier = Modifier.size(15.dp))
        Spacer(Modifier.width(6.dp))
        Text(text, style = AcornTheme.type.footnote.copy(fontWeight = FontWeight.Medium), color = colors.inkSoft, maxLines = 1)
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

/** How narrow a column of long text gets before the table scrolls instead; four such columns fit a phone. */
const val MIN_WRAPPED_COLUMN = 96f

/** A column wide enough for its longest line, within limits; CJK characters count double. */
fun columnWidth(texts: List<String>): Dp {
    val longest = texts.maxOfOrNull { text -> text.sumOf { if (it.code >= 0x2E80) 2 else 1 }.toInt() } ?: 0
    return (longest * 7 + 20).coerceIn(56, 240).dp
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
