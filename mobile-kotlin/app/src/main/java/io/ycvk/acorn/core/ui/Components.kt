package io.ycvk.acorn.core.ui

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.LocalIndication
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
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
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.draw.scale
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.CompositingStrategy
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import io.ycvk.acorn.core.theme.AcornMotion
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme

val ScreenPadding = 20.dp

/** Inner padding of a row inside a rounded group. */
val GroupPadding = 16.dp

/** A thin rule between rows; inset so it starts under the row text. */
@Composable
fun Hairline(modifier: Modifier = Modifier, inset: Dp = 0.dp) {
    HorizontalDivider(
        modifier = modifier.padding(start = inset),
        thickness = 0.5.dp,
        color = AcornTheme.colors.line,
    )
}

/** Tab root header: an optional small line above a large title, with trailing actions. */
@Composable
fun LargeHeader(
    title: String,
    modifier: Modifier = Modifier,
    eyebrow: String? = null,
    trailing: @Composable RowScope.() -> Unit = {},
) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .statusBarsPadding()
            .padding(start = ScreenPadding, end = 10.dp, top = 18.dp, bottom = 10.dp),
        verticalAlignment = Alignment.Bottom,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            eyebrow?.let {
                Text(it, style = AcornTheme.type.eyebrow, color = AcornTheme.colors.inkFaint)
                Spacer(Modifier.height(2.dp))
            }
            Text(title, style = AcornTheme.type.largeTitle, color = AcornTheme.colors.ink)
        }
        Row(verticalAlignment = Alignment.CenterVertically, content = trailing)
    }
}

/** Detail page bar: back, a one-line title with an optional status line, actions; the rule shows once content scrolls under it. */
@Composable
fun TopBar(
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
    title: String? = null,
    subtitle: String? = null,
    subtitleColor: Color = AcornTheme.colors.inkFaint,
    showDivider: Boolean = false,
    actions: @Composable RowScope.() -> Unit = {},
) {
    Column(modifier = modifier.background(AcornTheme.colors.canvas)) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .statusBarsPadding()
                .height(56.dp)
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
        AnimatedVisibility(visible = showDivider, enter = fadeIn(AcornMotion.enter()), exit = fadeOut(AcornMotion.exit())) { Hairline() }
    }
}

@Composable
fun IconAction(
    icon: ImageVector,
    contentDescription: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    tint: Color = AcornTheme.colors.ink,
    enabled: Boolean = true,
) {
    val interaction = remember { MutableInteractionSource() }
    Box(
        modifier = modifier
            .size(44.dp)
            .pressScale(interaction, 0.88f)
            .clip(CircleShape)
            .clickable(interaction, LocalIndication.current, enabled = enabled, role = Role.Button, onClick = onClick)
            .alpha(if (enabled) 1f else 0.4f),
        contentAlignment = Alignment.Center,
    ) {
        Icon(icon, contentDescription = contentDescription, tint = tint, modifier = Modifier.size(22.dp))
    }
}

/** Small label above a group of rows, aligned with the text inside the group. */
@Composable
fun SectionLabel(text: String, modifier: Modifier = Modifier, trailing: @Composable RowScope.() -> Unit = {}) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .padding(start = ScreenPadding + 4.dp, end = ScreenPadding, top = 28.dp, bottom = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text, style = AcornTheme.type.eyebrow, color = AcornTheme.colors.inkFaint, modifier = Modifier.weight(1f))
        trailing()
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

/** A card on the canvas; raised ones float on a soft shadow and give a little under the finger. */
@Composable
fun Panel(
    modifier: Modifier = Modifier,
    color: Color = AcornTheme.colors.surface,
    elevated: Boolean = true,
    onClick: (() -> Unit)? = null,
    contentPadding: PaddingValues = PaddingValues(18.dp),
    content: @Composable ColumnScope.() -> Unit,
) {
    val shape = AcornShapes.large
    val interaction = remember { MutableInteractionSource() }
    Column(
        modifier = modifier
            .fillMaxWidth()
            .then(if (onClick != null) Modifier.pressScale(interaction, 0.98f) else Modifier)
            .then(if (elevated) Modifier.raised(shape) else Modifier)
            .clip(shape)
            .background(color)
            .then(if (onClick != null) Modifier.clickable(interaction, LocalIndication.current, onClick = onClick) else Modifier)
            .padding(contentPadding),
        content = content,
    )
}

/** The corners a row gets as one slice of a rounded group: the first and last rows round the group's ends. */
fun groupShape(index: Int, count: Int): Shape {
    val radius = 20.dp
    val top = if (index == 0) radius else 0.dp
    val bottom = if (index == count - 1) radius else 0.dp
    return RoundedCornerShape(topStart = top, topEnd = top, bottomStart = bottom, bottomEnd = bottom)
}

/** Draws a list item as one slice of a white group inset from the screen edges. */
fun Modifier.groupItem(index: Int, count: Int, color: Color): Modifier =
    padding(horizontal = ScreenPadding).clip(groupShape(index, count)).background(color)

/** A full-width list row; inside a rounded group pass [GroupPadding] as the horizontal padding. */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun ListRow(
    modifier: Modifier = Modifier,
    onClick: (() -> Unit)? = null,
    onLongClick: (() -> Unit)? = null,
    horizontalPadding: Dp = ScreenPadding,
    leading: (@Composable () -> Unit)? = null,
    trailing: (@Composable RowScope.() -> Unit)? = null,
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
            .heightIn(min = 58.dp)
            .padding(horizontal = horizontalPadding, vertical = 13.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        leading?.let {
            it()
            Spacer(Modifier.width(14.dp))
        }
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp), content = content)
        trailing?.let {
            Spacer(Modifier.width(12.dp))
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
    // Painted only mid-swipe, so it can't tint the antialiased rounded corners of a resting row.
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
                    .padding(horizontal = 26.dp),
                contentAlignment = Alignment.CenterEnd,
            ) {
                Icon(AcornIcons.Trash, contentDescription = "删除", tint = AcornTheme.colors.onAccent, modifier = Modifier.scale(iconScale))
            }
        },
    ) {
        Box(Modifier.background(background)) { content() }
    }
}

/** Raised is the secondary action on a tinted card, where the muted fill would sink in. */
enum class ButtonKind { Primary, Secondary, Raised, Quiet, Danger }

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
        ButtonKind.Raised -> colors.surface to colors.ink
        ButtonKind.Quiet -> Color.Transparent to colors.accent
        ButtonKind.Danger -> colors.dangerSoft to colors.danger
    }
    val shape = AcornShapes.pill
    val active = enabled && !loading
    val interaction = remember { MutableInteractionSource() }
    Box(
        modifier = modifier
            .pressScale(interaction, 0.96f)
            .height(if (compact) 38.dp else 50.dp)
            .alpha(if (enabled) 1f else 0.4f)
            .clip(shape)
            .background(container)
            .clickable(interaction, LocalIndication.current, enabled = active, role = Role.Button, onClick = onClick)
            .padding(horizontal = if (compact) 16.dp else 22.dp),
        contentAlignment = Alignment.Center,
    ) {
        if (loading) {
            CircularProgressIndicator(color = content, strokeWidth = 2.dp, modifier = Modifier.size(18.dp))
        } else {
            Row(verticalAlignment = Alignment.CenterVertically) {
                leadingIcon?.let {
                    Icon(it, contentDescription = null, tint = content, modifier = Modifier.size(18.dp))
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
    Column(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = 40.dp, vertical = 56.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Box(
            modifier = Modifier
                .size(64.dp)
                .background(AcornTheme.colors.accentSoft, RoundedCornerShape(20.dp)),
            contentAlignment = Alignment.Center,
        ) {
            Icon(icon, contentDescription = null, tint = AcornTheme.colors.accent, modifier = Modifier.size(28.dp))
        }
        Spacer(Modifier.height(20.dp))
        Text(title, style = AcornTheme.type.headline.copy(fontSize = 18.sp, fontWeight = FontWeight.SemiBold), color = AcornTheme.colors.ink, textAlign = TextAlign.Center)
        Spacer(Modifier.height(8.dp))
        Text(body, style = AcornTheme.type.callout, color = AcornTheme.colors.inkSoft, textAlign = TextAlign.Center)
        action?.let {
            Spacer(Modifier.height(24.dp))
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
    Row(
        modifier = modifier
            .fillMaxWidth()
            .clip(AcornShapes.medium)
            .background(AcornTheme.colors.dangerSoft)
            .padding(start = 16.dp, end = 4.dp, top = 4.dp, bottom = 4.dp)
            .heightIn(min = 44.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(message, style = AcornTheme.type.footnote, color = AcornTheme.colors.danger, modifier = Modifier.weight(1f))
        action?.let {
            Text(
                it,
                style = AcornTheme.type.footnote.copy(fontWeight = FontWeight.Medium),
                color = AcornTheme.colors.danger,
                modifier = Modifier
                    .clip(AcornShapes.pill)
                    .clickable(onClick = onAction)
                    .padding(horizontal = 12.dp, vertical = 8.dp),
            )
        }
    }
}

/** Placeholder rows in a group card while the first load runs; a light band sweeps across them. */
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
    Column(modifier = modifier.fillMaxSize().padding(top = 12.dp)) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .groupItem(0, 1, colors.surface)
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
                Column(modifier = Modifier.padding(horizontal = GroupPadding, vertical = 16.dp)) {
                    Box(
                        Modifier
                            .fillMaxWidth(if (index % 2 == 0) 0.55f else 0.4f)
                            .height(13.dp)
                            .background(colors.surfaceMuted, AcornShapes.pill),
                    )
                    Spacer(Modifier.height(10.dp))
                    Box(
                        Modifier
                            .fillMaxWidth(if (index % 3 == 0) 0.9f else 0.75f)
                            .height(10.dp)
                            .background(colors.surfaceMuted, AcornShapes.pill),
                    )
                }
            }
        }
    }
}

/** A small rounded tag, e.g. a state or a count. */
@Composable
fun Tag(text: String, modifier: Modifier = Modifier, color: Color = AcornTheme.colors.inkSoft, container: Color = AcornTheme.colors.surfaceMuted) {
    Text(
        text,
        style = AcornTheme.type.caption.copy(fontWeight = FontWeight.Medium),
        color = color,
        modifier = modifier
            .clip(RoundedCornerShape(7.dp))
            .background(container)
            .padding(horizontal = 8.dp, vertical = 2.dp),
    )
}

/** Equal options in one rounded track; a raised thumb glides to the selected one. */
@Composable
fun SegmentedControl(options: List<String>, selected: Int, onSelect: (Int) -> Unit, modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    val haptics = rememberHaptics()
    BoxWithConstraints(
        modifier = modifier
            .fillMaxWidth()
            .height(38.dp)
            .clip(AcornShapes.small)
            .background(colors.surfaceMuted)
            .padding(3.dp),
    ) {
        val segment = maxWidth / options.size
        val offset by animateDpAsState(segment * selected, AcornMotion.glide(), label = "segment_thumb")
        val thumbShape = RoundedCornerShape(9.dp)
        Box(
            Modifier
                .offset(x = offset)
                .width(segment)
                .fillMaxHeight()
                .raised(thumbShape, 3.dp)
                .background(colors.surface, thumbShape),
        )
        Row(Modifier.fillMaxSize()) {
            options.forEachIndexed { index, label ->
                val active = index == selected
                Box(
                    modifier = Modifier
                        .weight(1f)
                        .fillMaxHeight()
                        .clip(thumbShape)
                        .clickable(
                            interactionSource = remember { MutableInteractionSource() },
                            indication = null,
                            role = Role.Tab,
                        ) {
                            if (!active) haptics.tick()
                            onSelect(index)
                        },
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        label,
                        style = AcornTheme.type.footnote.copy(fontWeight = if (active) FontWeight.Medium else FontWeight.Normal),
                        color = if (active) colors.ink else colors.inkSoft,
                    )
                }
            }
        }
    }
}

/** A small selectable pill, for narrowing a list; the selected one is filled with ink. */
@Composable
fun FilterPill(text: String, selected: Boolean, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    val interaction = remember { MutableInteractionSource() }
    Text(
        text,
        style = AcornTheme.type.footnote.copy(fontWeight = FontWeight.Medium),
        color = if (selected) colors.canvas else colors.inkSoft,
        modifier = modifier
            .pressScale(interaction, 0.94f)
            .clip(AcornShapes.pill)
            .background(if (selected) colors.ink else colors.surface)
            .clickable(interaction, LocalIndication.current, role = Role.RadioButton, onClick = onClick)
            .padding(horizontal = 14.dp, vertical = 7.dp),
    )
}

/** A popup menu on a raised rounded card. */
@Composable
fun AcornMenu(expanded: Boolean, onDismiss: () -> Unit, content: @Composable ColumnScope.() -> Unit) {
    DropdownMenu(
        expanded = expanded,
        onDismissRequest = onDismiss,
        shape = AcornShapes.medium,
        containerColor = AcornTheme.colors.surface,
        tonalElevation = 0.dp,
        shadowElevation = 12.dp,
        content = content,
    )
}

@Composable
fun AcornMenuItem(text: String, onClick: () -> Unit, danger: Boolean = false, icon: ImageVector? = null) {
    val colors = AcornTheme.colors
    val tint = if (danger) colors.danger else colors.ink
    DropdownMenuItem(
        text = { Text(text, style = AcornTheme.type.callout, color = tint) },
        leadingIcon = icon?.let { { Icon(it, contentDescription = null, tint = if (danger) colors.danger else colors.inkSoft, modifier = Modifier.size(20.dp)) } },
        onClick = onClick,
        modifier = Modifier.padding(horizontal = 4.dp),
    )
}

/** The grip at the top of a bottom sheet. */
@Composable
fun SheetHandle() {
    Box(
        Modifier
            .padding(top = 10.dp, bottom = 14.dp)
            .size(width = 36.dp, height = 5.dp)
            .background(AcornTheme.colors.lineStrong, AcornShapes.pill),
    )
}

/** Bottom sheets round only their top corners. */
val SheetShape = RoundedCornerShape(topStart = 28.dp, topEnd = 28.dp)
