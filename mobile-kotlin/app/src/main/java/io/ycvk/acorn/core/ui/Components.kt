package io.ycvk.acorn.core.ui

import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.LocalIndication
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CloudOff
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.SwipeToDismissBox
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.Text
import androidx.compose.material3.rememberSwipeToDismissBoxState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.draw.scale
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.CompositingStrategy
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import io.ycvk.acorn.R
import io.ycvk.acorn.core.theme.AcornMotion
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme

val ScreenPadding = 16.dp

/** A 1px rule between rows; inset so it starts under the row text. */
@Composable
fun Hairline(modifier: Modifier = Modifier, inset: Dp = 0.dp) {
    HorizontalDivider(
        modifier = modifier.padding(start = inset),
        thickness = 1.dp,
        color = AcornTheme.colors.line,
    )
}

/** The Acorn mark on an accent tile: the app's signature in headers and as the agent's avatar. */
@Composable
fun AcornMark(modifier: Modifier = Modifier, size: Dp = 24.dp) {
    Box(
        modifier = modifier
            .size(size)
            .background(AcornTheme.colors.accent, RoundedCornerShape(size * 0.27f)),
        contentAlignment = Alignment.Center,
    ) {
        Icon(painterResource(R.drawable.ic_acorn), contentDescription = null, tint = AcornTheme.colors.onAccent, modifier = Modifier.size(size * 0.58f))
    }
}

/** Tab root bar: the Acorn mark, the page title and trailing actions. */
@Composable
fun PageHeader(
    title: String,
    modifier: Modifier = Modifier,
    trailing: @Composable RowScope.() -> Unit = {},
) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .statusBarsPadding()
            .height(52.dp)
            .padding(start = ScreenPadding, end = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        AcornMark()
        Spacer(Modifier.width(10.dp))
        Text(title, style = AcornTheme.type.title, color = AcornTheme.colors.ink, modifier = Modifier.weight(1f))
        Row(verticalAlignment = Alignment.CenterVertically, content = trailing)
    }
}

/** Detail page bar: back, a one-line title with an optional status line, actions, and a rule below. */
@Composable
fun TopBar(
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
    title: String? = null,
    subtitle: String? = null,
    subtitleColor: Color = AcornTheme.colors.inkFaint,
    actions: @Composable RowScope.() -> Unit = {},
) {
    Column(modifier = modifier.background(AcornTheme.colors.canvas)) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .statusBarsPadding()
                .height(52.dp)
                .padding(horizontal = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconAction(AcornIcons.Back, "返回", onBack)
            Column(modifier = Modifier.weight(1f).padding(horizontal = 2.dp)) {
                title?.let {
                    Text(
                        it,
                        style = AcornTheme.type.headline.copy(fontWeight = FontWeight.SemiBold),
                        color = AcornTheme.colors.ink,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                subtitle?.let {
                    Text(it, style = AcornTheme.type.caption, color = subtitleColor, maxLines = 1)
                }
            }
            actions()
        }
        Hairline()
    }
}

@Composable
fun IconAction(
    icon: ImageVector,
    contentDescription: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    tint: Color = AcornTheme.colors.inkSoft,
    enabled: Boolean = true,
) {
    val interaction = remember { MutableInteractionSource() }
    Box(
        modifier = modifier
            .size(40.dp)
            .pressScale(interaction, 0.9f)
            .clip(AcornShapes.medium)
            .clickable(interaction, LocalIndication.current, enabled = enabled, role = Role.Button, onClick = onClick)
            .alpha(if (enabled) 1f else 0.4f),
        contentAlignment = Alignment.Center,
    ) {
        Icon(icon, contentDescription = contentDescription, tint = tint, modifier = Modifier.size(20.dp))
    }
}

/** A small label above a bordered group or a card, as in settings and on 今天, with an optional count. */
@Composable
fun SectionLabel(text: String, modifier: Modifier = Modifier, count: Int? = null, trailing: @Composable RowScope.() -> Unit = {}) {
    val colors = AcornTheme.colors
    Row(
        modifier = modifier
            .fillMaxWidth()
            .padding(start = ScreenPadding + 2.dp, end = ScreenPadding, top = 24.dp, bottom = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text, style = AcornTheme.type.eyebrow, color = colors.inkSoft, modifier = Modifier.weight(1f, fill = false))
        count?.let {
            Spacer(Modifier.width(8.dp))
            Text("$it", style = AcornTheme.type.caption, color = colors.inkFaint)
        }
        Spacer(Modifier.weight(1f))
        trailing()
    }
}

/** A grey band that opens a group in a flat list, with an optional count. */
@Composable
fun ListSection(text: String, modifier: Modifier = Modifier, count: Int? = null) {
    val colors = AcornTheme.colors
    Column(modifier = modifier.fillMaxWidth()) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .background(colors.surfaceMuted)
                .padding(horizontal = ScreenPadding, vertical = 7.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(text, style = AcornTheme.type.eyebrow, color = colors.inkSoft)
            count?.let {
                Spacer(Modifier.width(8.dp))
                Text("$it", style = AcornTheme.type.caption, color = colors.inkFaint)
            }
        }
        Hairline()
    }
}

@Composable
fun StatusDot(color: Color, modifier: Modifier = Modifier, pulsing: Boolean = false) {
    val alpha = if (pulsing) {
        val transition = rememberInfiniteTransition(label = "dot")
        val value by transition.animateFloat(
            initialValue = 1f,
            targetValue = 0.35f,
            animationSpec = infiniteRepeatable(tween(900, easing = AcornMotion.standard), RepeatMode.Reverse),
            label = "dot_alpha",
        )
        value
    } else {
        1f
    }
    Box(
        modifier = modifier
            .size(7.dp)
            .alpha(alpha)
            .background(color, CircleShape),
    )
}

/** Where a piece of work stands, drawn as one small circle glyph. */
enum class Status { Idle, Running, Done, Waiting, Failed }

@Composable
fun StatusGlyph(status: Status, modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    Canvas(modifier.size(15.dp)) {
        val r = size.minDimension / 2 - 1.dp.toPx()
        val c = Offset(size.width / 2, size.height / 2)
        val ring = Stroke(1.6.dp.toPx())
        when (status) {
            Status.Idle -> drawCircle(colors.inkFaint, r, c, style = ring)
            Status.Running -> {
                drawCircle(colors.warning, r, c, style = ring)
                drawArc(colors.warning, -90f, 180f, true, Offset(c.x - r * 0.55f, c.y - r * 0.55f), Size(r * 1.1f, r * 1.1f))
            }
            Status.Done -> {
                drawCircle(colors.accent, r + 0.5.dp.toPx(), c)
                val tick = Path().apply {
                    moveTo(c.x - r * 0.42f, c.y + r * 0.02f)
                    lineTo(c.x - r * 0.1f, c.y + r * 0.34f)
                    lineTo(c.x + r * 0.45f, c.y - r * 0.3f)
                }
                drawPath(tick, colors.onAccent, style = Stroke(1.5.dp.toPx(), cap = StrokeCap.Round, join = StrokeJoin.Round))
            }
            Status.Waiting -> drawCircle(
                colors.warning,
                r,
                c,
                style = Stroke(1.6.dp.toPx(), pathEffect = PathEffect.dashPathEffect(floatArrayOf(3.dp.toPx(), 2.2.dp.toPx()))),
            )
            Status.Failed -> {
                drawCircle(colors.danger, r + 0.5.dp.toPx(), c)
                val d = r * 0.35f
                drawLine(colors.onAccent, Offset(c.x - d, c.y - d), Offset(c.x + d, c.y + d), 1.5.dp.toPx(), StrokeCap.Round)
                drawLine(colors.onAccent, Offset(c.x + d, c.y - d), Offset(c.x - d, c.y + d), 1.5.dp.toPx(), StrokeCap.Round)
            }
        }
    }
}

/** A bordered card in the flow of a page; tinted cards drop the rule. */
@Composable
fun Panel(
    modifier: Modifier = Modifier,
    color: Color = AcornTheme.colors.surface,
    onClick: (() -> Unit)? = null,
    contentPadding: PaddingValues = PaddingValues(14.dp),
    content: @Composable ColumnScope.() -> Unit,
) {
    val colors = AcornTheme.colors
    val shape = AcornShapes.large
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(shape)
            .background(color)
            .border(1.dp, if (color == colors.surface) colors.line else color, shape)
            .then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier)
            .padding(contentPadding),
        content = content,
    )
}

/** A full-width list row. */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun ListRow(
    modifier: Modifier = Modifier,
    onClick: (() -> Unit)? = null,
    onLongClick: (() -> Unit)? = null,
    leading: (@Composable () -> Unit)? = null,
    trailing: (@Composable RowScope.() -> Unit)? = null,
    verticalAlignment: Alignment.Vertical = Alignment.CenterVertically,
    content: @Composable ColumnScope.() -> Unit,
) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .then(
                if (onClick != null || onLongClick != null) {
                    Modifier.combinedClickable(
                        onClick = { onClick?.invoke() },
                        onLongClick = onLongClick,
                    )
                } else {
                    Modifier
                },
            )
            .heightIn(min = 52.dp)
            .padding(horizontal = ScreenPadding, vertical = 11.dp),
        verticalAlignment = verticalAlignment,
    ) {
        leading?.let {
            it()
            Spacer(Modifier.width(13.dp))
        }
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp), content = content)
        trailing?.let {
            Spacer(Modifier.width(10.dp))
            Row(verticalAlignment = Alignment.CenterVertically, content = it)
        }
    }
}

/** A row that asks to delete itself when swiped from the end toward the start; the row springs back either way. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SwipeToDelete(
    onDelete: () -> Unit,
    modifier: Modifier = Modifier,
    background: Color = AcornTheme.colors.canvas,
    content: @Composable () -> Unit,
) {
    val haptics = rememberHaptics()
    val state = rememberSwipeToDismissBoxState(
        confirmValueChange = { value ->
            if (value == SwipeToDismissBoxValue.EndToStart) {
                haptics.longPress()
                onDelete()
            }
            false
        },
    )
    val armed = state.targetValue == SwipeToDismissBoxValue.EndToStart
    val iconScale by animateFloatAsState(if (armed) 1.15f else 0.85f, AcornMotion.pop(), label = "swipe_icon")
    // Painted only mid-swipe, so a resting row never shows red at its edges.
    val swiping = state.dismissDirection == SwipeToDismissBoxValue.EndToStart
    SwipeToDismissBox(
        state = state,
        enableDismissFromStartToEnd = false,
        modifier = modifier,
        backgroundContent = {
            Box(
                modifier = Modifier
                    .fillMaxSize()
                    .background(if (swiping) AcornTheme.colors.danger else Color.Transparent)
                    .padding(horizontal = 24.dp),
                contentAlignment = Alignment.CenterEnd,
            ) {
                Icon(AcornIcons.Trash, contentDescription = "删除", tint = AcornTheme.colors.onAccent, modifier = Modifier.scale(iconScale))
            }
        },
    ) {
        Box(Modifier.background(background)) { content() }
    }
}

/** Danger is a bordered destructive action; Destructive fills it, for the confirming step. */
enum class ButtonKind { Primary, Secondary, Quiet, Danger, Destructive }

@Composable
fun AcornButton(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    kind: ButtonKind = ButtonKind.Primary,
    compact: Boolean = false,
    loading: Boolean = false,
    enabled: Boolean = true,
    leadingIcon: ImageVector? = null,
) {
    val colors = AcornTheme.colors
    val (container, content) = when (kind) {
        ButtonKind.Primary -> colors.accent to colors.onAccent
        ButtonKind.Secondary -> colors.surface to colors.ink
        ButtonKind.Quiet -> Color.Transparent to colors.accent
        ButtonKind.Danger -> colors.surface to colors.danger
        ButtonKind.Destructive -> colors.danger to colors.onAccent
    }
    val bordered = kind == ButtonKind.Secondary || kind == ButtonKind.Danger
    val shape = AcornShapes.medium
    val active = enabled && !loading
    val interaction = remember { MutableInteractionSource() }
    Box(
        modifier = modifier
            .pressScale(interaction, 0.97f)
            .height(if (compact) 32.dp else 40.dp)
            .alpha(if (enabled) 1f else 0.4f)
            .clip(shape)
            .background(container)
            .then(if (bordered) Modifier.border(1.dp, colors.lineStrong, shape) else Modifier)
            .clickable(interaction, LocalIndication.current, enabled = active, role = Role.Button, onClick = onClick)
            .padding(horizontal = if (compact) 12.dp else 16.dp),
        contentAlignment = Alignment.Center,
    ) {
        if (loading) {
            CircularProgressIndicator(color = content, strokeWidth = 2.dp, modifier = Modifier.size(16.dp))
        } else {
            Row(verticalAlignment = Alignment.CenterVertically) {
                leadingIcon?.let {
                    Icon(it, contentDescription = null, tint = content, modifier = Modifier.size(16.dp))
                    Spacer(Modifier.width(6.dp))
                }
                Text(
                    text,
                    style = if (compact) AcornTheme.type.footnote.copy(fontWeight = FontWeight.Medium) else AcornTheme.type.label,
                    color = content,
                    maxLines = 1,
                )
            }
        }
    }
}

/** A centred invitation for an empty space: what it holds and how it fills. */
@Composable
fun EmptyState(
    icon: ImageVector,
    title: String,
    body: String,
    modifier: Modifier = Modifier,
    action: (@Composable () -> Unit)? = null,
) {
    val colors = AcornTheme.colors
    Column(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = 40.dp, vertical = 56.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Box(
            modifier = Modifier
                .size(44.dp)
                .border(1.dp, colors.line, AcornShapes.large)
                .background(colors.surface, AcornShapes.large),
            contentAlignment = Alignment.Center,
        ) {
            Icon(icon, contentDescription = null, tint = colors.inkSoft, modifier = Modifier.size(20.dp))
        }
        Spacer(Modifier.height(16.dp))
        Text(title, style = AcornTheme.type.headline.copy(fontWeight = FontWeight.SemiBold), color = colors.ink, textAlign = TextAlign.Center)
        Spacer(Modifier.height(6.dp))
        Text(body, style = AcornTheme.type.footnote, color = colors.inkSoft, textAlign = TextAlign.Center)
        action?.let {
            Spacer(Modifier.height(18.dp))
            it()
        }
    }
}

@Composable
fun ErrorState(message: String, onRetry: () -> Unit, modifier: Modifier = Modifier) {
    EmptyState(
        icon = Icons.Outlined.CloudOff,
        title = "没能加载",
        body = message,
        modifier = modifier,
        action = { AcornButton("重试", onRetry, kind = ButtonKind.Secondary, compact = true) },
    )
}

/** An inline failure line with an optional action, for errors above existing content. */
@Composable
fun InlineError(message: String, modifier: Modifier = Modifier, action: String? = null, onAction: () -> Unit = {}) {
    val colors = AcornTheme.colors
    Row(
        modifier = modifier
            .fillMaxWidth()
            .clip(AcornShapes.medium)
            .background(colors.dangerSoft)
            .padding(start = 12.dp, end = 4.dp, top = 4.dp, bottom = 4.dp)
            .heightIn(min = 40.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(message, style = AcornTheme.type.footnote, color = colors.danger, modifier = Modifier.weight(1f))
        action?.let {
            Text(
                it,
                style = AcornTheme.type.footnote.copy(fontWeight = FontWeight.Medium),
                color = colors.danger,
                modifier = Modifier
                    .clip(AcornShapes.small)
                    .clickable(onClick = onAction)
                    .padding(horizontal = 10.dp, vertical = 8.dp),
            )
        }
    }
}

/** Placeholder rows while the first load runs; a light band sweeps across them. */
@Composable
fun SkeletonRows(modifier: Modifier = Modifier, rows: Int = 6) {
    val colors = AcornTheme.colors
    val transition = rememberInfiniteTransition(label = "skeleton")
    val sweep by transition.animateFloat(
        initialValue = -0.4f,
        targetValue = 1.4f,
        animationSpec = infiniteRepeatable(tween(1300, easing = AcornMotion.standard)),
        label = "skeleton_sweep",
    )
    val shine = if (colors.isDark) Color.White.copy(alpha = 0.06f) else Color.White.copy(alpha = 0.75f)
    Column(
        modifier = modifier
            .fillMaxSize()
            .graphicsLayer(compositingStrategy = CompositingStrategy.Offscreen)
            .drawWithContent {
                drawContent()
                val band = size.width * 0.35f
                val x = size.width * sweep
                drawRect(
                    brush = Brush.linearGradient(
                        listOf(Color.Transparent, shine, Color.Transparent),
                        start = Offset(x - band, 0f),
                        end = Offset(x + band, size.height * 0.2f),
                    ),
                    blendMode = BlendMode.SrcAtop,
                )
            },
    ) {
        repeat(rows) { index ->
            Row(modifier = Modifier.padding(horizontal = ScreenPadding, vertical = 14.dp)) {
                Box(Modifier.padding(top = 1.dp).size(15.dp).background(colors.surfaceMuted, CircleShape))
                Spacer(Modifier.width(13.dp))
                Column {
                    Box(
                        Modifier
                            .fillMaxWidth(if (index % 2 == 0) 0.55f else 0.4f)
                            .height(12.dp)
                            .background(colors.surfaceMuted, AcornShapes.small),
                    )
                    Spacer(Modifier.height(8.dp))
                    Box(
                        Modifier
                            .fillMaxWidth(if (index % 3 == 0) 0.9f else 0.75f)
                            .height(10.dp)
                            .background(colors.surfaceMuted, AcornShapes.small),
                    )
                }
            }
            Hairline(inset = ScreenPadding + 28.dp)
        }
    }
}

/** A small label such as a state, a folder or a count; bordered unless given a tint. */
@Composable
fun Tag(text: String, modifier: Modifier = Modifier, color: Color = AcornTheme.colors.inkSoft, container: Color? = null) {
    val colors = AcornTheme.colors
    val shape = RoundedCornerShape(5.dp)
    Text(
        text,
        style = AcornTheme.type.caption.copy(fontSize = 11.5.sp, fontWeight = FontWeight.Medium),
        color = color,
        maxLines = 1,
        modifier = modifier
            .clip(shape)
            .then(if (container != null) Modifier.background(container) else Modifier.border(1.dp, colors.line, shape))
            .padding(horizontal = 6.dp, vertical = 1.dp),
    )
}

/** View switches as small pills; the selected one is filled and outlined. */
@Composable
fun SegmentedControl(options: List<String>, selected: Int, onSelect: (Int) -> Unit, modifier: Modifier = Modifier) {
    val haptics = rememberHaptics()
    Row(modifier = modifier, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        options.forEachIndexed { index, label ->
            ViewPill(label, index == selected, role = Role.Tab) {
                if (index != selected) haptics.tick()
                onSelect(index)
            }
        }
    }
}

/** A small selectable pill, for narrowing a list. */
@Composable
fun FilterPill(text: String, selected: Boolean, onClick: () -> Unit, modifier: Modifier = Modifier) {
    ViewPill(text, selected, modifier, Role.RadioButton, onClick)
}

@Composable
private fun ViewPill(text: String, selected: Boolean, modifier: Modifier = Modifier, role: Role, onClick: () -> Unit) {
    val colors = AcornTheme.colors
    val shape = AcornShapes.small
    Text(
        text,
        style = AcornTheme.type.footnote.copy(fontWeight = FontWeight.Medium),
        color = if (selected) colors.ink else colors.inkSoft,
        modifier = modifier
            .clip(shape)
            .then(if (selected) Modifier.background(colors.surfaceMuted).border(1.dp, colors.line, shape) else Modifier)
            .clickable(role = role, onClick = onClick)
            .padding(horizontal = 10.dp, vertical = 5.dp),
    )
}

/** A popup menu on a bordered card. */
@Composable
fun AcornMenu(expanded: Boolean, onDismiss: () -> Unit, content: @Composable ColumnScope.() -> Unit) {
    DropdownMenu(
        expanded = expanded,
        onDismissRequest = onDismiss,
        shape = AcornShapes.large,
        containerColor = AcornTheme.colors.surface,
        tonalElevation = 0.dp,
        shadowElevation = 8.dp,
        border = BorderStroke(1.dp, AcornTheme.colors.line),
        content = content,
    )
}

@Composable
fun AcornMenuItem(text: String, onClick: () -> Unit, danger: Boolean = false, icon: ImageVector? = null) {
    val colors = AcornTheme.colors
    val tint = if (danger) colors.danger else colors.ink
    DropdownMenuItem(
        text = { Text(text, style = AcornTheme.type.callout, color = tint) },
        leadingIcon = icon?.let { { Icon(it, contentDescription = null, tint = if (danger) colors.danger else colors.inkSoft, modifier = Modifier.size(18.dp)) } },
        onClick = onClick,
    )
}

/** The grip at the top of a bottom sheet. */
@Composable
fun SheetHandle() {
    Box(
        Modifier
            .padding(top = 10.dp, bottom = 12.dp)
            .size(width = 32.dp, height = 4.dp)
            .background(AcornTheme.colors.lineStrong, AcornShapes.pill),
    )
}

/** Bottom sheets round only their top corners. */
val SheetShape = RoundedCornerShape(topStart = 12.dp, topEnd = 12.dp)
