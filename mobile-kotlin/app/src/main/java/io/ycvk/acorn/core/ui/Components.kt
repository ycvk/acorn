package io.ycvk.acorn.core.ui

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateDpAsState
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
import androidx.compose.foundation.layout.offset
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
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.composed
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.draw.scale
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.CompositingStrategy
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.onPlaced
import androidx.compose.ui.layout.positionInParent
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
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
import io.ycvk.acorn.core.theme.CardRadius
import kotlinx.coroutines.launch

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

/** The Acorn mark on a graphite tile, as on the pairing screen. */
@Composable
fun AcornMark(modifier: Modifier = Modifier, size: Dp = 24.dp) {
    IconTile(modifier = modifier, size = size) {
        Icon(painterResource(R.drawable.ic_acorn), contentDescription = null, tint = AcornTheme.colors.ink, modifier = Modifier.size(size * 0.5f))
    }
}

/**
 * A small tile that frames an icon: a soft top-lit gradient inside a hairline,
 * the way a physical key catches light from above.
 */
@Composable
fun IconTile(modifier: Modifier = Modifier, size: Dp = 36.dp, content: @Composable () -> Unit) {
    val colors = AcornTheme.colors
    val shape = RoundedCornerShape(size * 0.3f)
    val fill = if (colors.isDark) listOf(colors.surfaceMuted, colors.surface) else listOf(colors.surface, colors.surfaceMuted)
    Box(
        modifier = modifier
            .size(size)
            .clip(shape)
            .background(Brush.verticalGradient(fill))
            .hairline(shape),
        contentAlignment = Alignment.Center,
    ) {
        content()
    }
}

/**
 * A 1px edge that catches light at the top and fades toward the bottom, for
 * cards and tiles; it reads as a bevel in dark mode and a plain rule in light.
 */
fun Modifier.hairline(shape: Shape): Modifier = composed {
    val colors = AcornTheme.colors
    val edge = if (colors.isDark) Brush.verticalGradient(listOf(colors.lineStrong, colors.line)) else SolidColor(colors.line)
    border(1.dp, edge, shape)
}

/** Tab root bar: a large page title and trailing round actions. */
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
            .height(64.dp)
            .padding(start = ScreenPadding + 2.dp, end = ScreenPadding - 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(title, style = AcornTheme.type.largeTitle, color = AcornTheme.colors.ink, modifier = Modifier.weight(1f))
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp), content = trailing)
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
    }
}

/** A round icon button; [filled] sets it on a raised disc, as the actions in a page header. */
@Composable
fun IconAction(
    icon: ImageVector,
    contentDescription: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    tint: Color = AcornTheme.colors.inkSoft,
    enabled: Boolean = true,
    filled: Boolean = false,
) {
    val colors = AcornTheme.colors
    val interaction = remember { MutableInteractionSource() }
    Box(
        modifier = modifier
            .size(40.dp)
            .pressScale(interaction, 0.9f)
            .clip(CircleShape)
            .then(if (filled) Modifier.background(colors.surface).hairline(CircleShape) else Modifier)
            .clickable(interaction, LocalIndication.current, enabled = enabled, role = Role.Button, onClick = onClick)
            .alpha(if (enabled) 1f else 0.4f),
        contentAlignment = Alignment.Center,
    ) {
        Icon(icon, contentDescription = contentDescription, tint = if (filled) colors.ink else tint, modifier = Modifier.size(if (filled) 18.dp else 20.dp))
    }
}

/** A small label above a bordered group or a card, as in settings and on 今天, with an optional count. */
@Composable
fun SectionLabel(text: String, modifier: Modifier = Modifier, count: Int? = null, trailing: @Composable RowScope.() -> Unit = {}) {
    val colors = AcornTheme.colors
    Row(
        modifier = modifier
            .fillMaxWidth()
            .padding(start = ScreenPadding + 6.dp, end = ScreenPadding + 2.dp, top = 26.dp, bottom = 10.dp),
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

/**
 * One row's slice of a grouped card in a lazy list: the first row rounds the
 * top corners, the last the bottom, and the rows between sit flush.
 */
fun Modifier.groupItem(index: Int, count: Int): Modifier = composed {
    val radius = CardRadius
    val shape = RoundedCornerShape(
        topStart = if (index == 0) radius else 0.dp,
        topEnd = if (index == 0) radius else 0.dp,
        bottomStart = if (index == count - 1) radius else 0.dp,
        bottomEnd = if (index == count - 1) radius else 0.dp,
    )
    padding(horizontal = ScreenPadding)
        .clip(shape)
        .background(AcornTheme.colors.surface)
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

/** A card in the flow of a page, lit along its top edge; tinted cards drop the edge. */
@Composable
fun Panel(
    modifier: Modifier = Modifier,
    color: Color = AcornTheme.colors.surface,
    onClick: (() -> Unit)? = null,
    contentPadding: PaddingValues = PaddingValues(16.dp),
    content: @Composable ColumnScope.() -> Unit,
) {
    val colors = AcornTheme.colors
    val shape = AcornShapes.card
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(shape)
            .background(color)
            .then(if (color == colors.surface) Modifier.hairline(shape) else Modifier)
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
            .heightIn(min = 56.dp)
            .padding(horizontal = ScreenPadding, vertical = 13.dp),
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
    background: Color = AcornTheme.colors.surface,
    content: @Composable () -> Unit,
) {
    val haptics = rememberHaptics()
    val scope = rememberCoroutineScope()
    val state = rememberSwipeToDismissBoxState()
    val armed = state.targetValue == SwipeToDismissBoxValue.EndToStart
    val iconScale by animateFloatAsState(if (armed) 1.15f else 0.85f, AcornMotion.pop(), label = "swipe_icon")
    // Painted only mid-swipe, so a resting row never shows red at its edges.
    val swiping = state.dismissDirection == SwipeToDismissBoxValue.EndToStart
    SwipeToDismissBox(
        state = state,
        enableDismissFromStartToEnd = false,
        modifier = modifier,
        onDismiss = { value ->
            if (value == SwipeToDismissBoxValue.EndToStart) {
                haptics.longPress()
                onDelete()
            }
            scope.launch { state.reset() }
        },
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

/** Danger is a soft destructive action; Destructive fills it, for the confirming step. */
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
        ButtonKind.Secondary -> colors.surfaceMuted to colors.ink
        ButtonKind.Quiet -> Color.Transparent to colors.accent
        ButtonKind.Danger -> colors.dangerSoft to colors.danger
        ButtonKind.Destructive -> colors.danger to colors.onAccent
    }
    val shape = AcornShapes.pill
    val active = enabled && !loading
    val interaction = remember { MutableInteractionSource() }
    Box(
        modifier = modifier
            .pressScale(interaction, 0.96f)
            .height(if (compact) 34.dp else 44.dp)
            .alpha(if (enabled) 1f else 0.4f)
            .clip(shape)
            .background(container)
            .clickable(interaction, LocalIndication.current, enabled = active, role = Role.Button, onClick = onClick)
            .padding(horizontal = if (compact) 14.dp else 20.dp),
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
        IconTile(size = 48.dp) {
            Icon(icon, contentDescription = null, tint = colors.inkSoft, modifier = Modifier.size(22.dp))
        }
        Spacer(Modifier.height(18.dp))
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
            .clip(AcornShapes.large)
            .background(colors.dangerSoft)
            .padding(start = 14.dp, end = 4.dp, top = 4.dp, bottom = 4.dp)
            .heightIn(min = 44.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(message, style = AcornTheme.type.footnote, color = colors.danger, modifier = Modifier.weight(1f))
        action?.let {
            Text(
                it,
                style = AcornTheme.type.footnote.copy(fontWeight = FontWeight.Medium),
                color = colors.danger,
                modifier = Modifier
                    .clip(AcornShapes.pill)
                    .clickable(onClick = onAction)
                    .padding(horizontal = 12.dp, vertical = 8.dp),
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
                            .background(colors.surfaceMuted, AcornShapes.pill),
                    )
                    Spacer(Modifier.height(8.dp))
                    Box(
                        Modifier
                            .fillMaxWidth(if (index % 3 == 0) 0.9f else 0.75f)
                            .height(10.dp)
                            .background(colors.surfaceMuted, AcornShapes.pill),
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
    val shape = AcornShapes.pill
    Text(
        text,
        style = AcornTheme.type.caption.copy(fontSize = 11.5.sp, fontWeight = FontWeight.Medium),
        color = color,
        maxLines = 1,
        modifier = modifier
            .clip(shape)
            .then(if (container != null) Modifier.background(container) else Modifier.border(1.dp, colors.line, shape))
            .padding(horizontal = 8.dp, vertical = 2.dp),
    )
}

/** View switches on a pill track; a raised thumb glides to the selected one. */
@Composable
fun SegmentedControl(options: List<String>, selected: Int, onSelect: (Int) -> Unit, modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    val haptics = rememberHaptics()
    val density = LocalDensity.current
    val bounds = remember(options) { mutableStateMapOf<Int, Pair<Float, Float>>() }
    val target = bounds[selected]
    val thumbX by animateDpAsState(with(density) { (target?.first ?: 0f).toDp() }, AcornMotion.glide(), label = "segment_x")
    val thumbW by animateDpAsState(with(density) { (target?.second ?: 0f).toDp() }, AcornMotion.glide(), label = "segment_w")
    Box(
        modifier = modifier
            .clip(AcornShapes.pill)
            .background(colors.surfaceMuted)
            .padding(3.dp),
    ) {
        if (target != null) {
            Box(
                Modifier
                    .offset(x = thumbX)
                    .width(thumbW)
                    .height(30.dp)
                    .shadow(if (colors.isDark) 0.dp else 2.dp, AcornShapes.pill, clip = false)
                    .background(if (colors.isDark) colors.lineStrong else colors.surface, AcornShapes.pill),
            )
        }
        Row {
            options.forEachIndexed { index, label ->
                val tint by animateColorAsState(if (index == selected) colors.ink else colors.inkSoft, AcornMotion.standard(AcornMotion.SHORT), label = "segment_tint")
                Box(
                    modifier = Modifier
                        .onPlaced { bounds[index] = it.positionInParent().x to it.size.width.toFloat() }
                        .height(30.dp)
                        .clip(AcornShapes.pill)
                        .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, role = Role.Tab) {
                            if (index != selected) haptics.tick()
                            onSelect(index)
                        }
                        .semantics { this.selected = index == selected }
                        .padding(horizontal = 16.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(label, style = AcornTheme.type.footnote.copy(fontWeight = FontWeight.Medium), color = tint)
                }
            }
        }
    }
}

/** A selectable pill for narrowing a list; the selected one is filled with ink. */
@Composable
fun FilterPill(text: String, selected: Boolean, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    val shape = AcornShapes.pill
    val fill by animateColorAsState(if (selected) colors.ink else Color.Transparent, AcornMotion.standard(AcornMotion.SHORT), label = "filter_fill")
    val tint by animateColorAsState(if (selected) colors.canvas else colors.inkSoft, AcornMotion.standard(AcornMotion.SHORT), label = "filter_tint")
    Text(
        text,
        style = AcornTheme.type.footnote.copy(fontWeight = FontWeight.Medium),
        color = tint,
        modifier = modifier
            .clip(shape)
            .background(fill)
            .then(if (selected) Modifier else Modifier.border(1.dp, colors.line, shape))
            .clickable(role = Role.RadioButton, onClick = onClick)
            .padding(horizontal = 12.dp, vertical = 6.dp),
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
        shadowElevation = 12.dp,
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
val SheetShape = RoundedCornerShape(topStart = 28.dp, topEnd = 28.dp)
