package io.ycvk.acorn.core.ui

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
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
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.outlined.CloudOff
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.SwipeToDismissBox
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.Text
import androidx.compose.material3.rememberSwipeToDismissBoxState
import androidx.compose.material3.ripple
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme

val ScreenPadding = 20.dp

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
            .padding(start = ScreenPadding, end = 12.dp, top = 12.dp, bottom = 8.dp),
        verticalAlignment = Alignment.Bottom,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            eyebrow?.let {
                Text(it, style = AcornTheme.type.footnote, color = AcornTheme.colors.inkFaint)
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
                .padding(horizontal = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconAction(Icons.AutoMirrored.Outlined.ArrowBack, "返回", onBack)
            Column(modifier = Modifier.weight(1f).padding(horizontal = 4.dp)) {
                title?.let {
                    Text(
                        it,
                        style = AcornTheme.type.headline,
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
        AnimatedVisibility(visible = showDivider, enter = fadeIn(), exit = fadeOut()) { Hairline() }
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
    Box(
        modifier = modifier
            .size(44.dp)
            .clip(CircleShape)
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .alpha(if (enabled) 1f else 0.4f),
        contentAlignment = Alignment.Center,
    ) {
        Icon(icon, contentDescription = contentDescription, tint = tint, modifier = Modifier.size(22.dp))
    }
}

/** Small grey label above a group of rows. */
@Composable
fun SectionLabel(text: String, modifier: Modifier = Modifier, trailing: @Composable RowScope.() -> Unit = {}) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .padding(start = ScreenPadding, end = ScreenPadding, top = 24.dp, bottom = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text, style = AcornTheme.type.footnote, color = AcornTheme.colors.inkFaint, modifier = Modifier.weight(1f))
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
            animationSpec = infiniteRepeatable(tween(900), RepeatMode.Reverse),
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

/** A raised block on the canvas, outlined by a hairline. */
@Composable
fun Panel(
    modifier: Modifier = Modifier,
    color: Color = AcornTheme.colors.surface,
    borderColor: Color = AcornTheme.colors.line,
    onClick: (() -> Unit)? = null,
    contentPadding: PaddingValues = PaddingValues(16.dp),
    content: @Composable ColumnScope.() -> Unit,
) {
    val shape = AcornShapes.medium
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(shape)
            .background(color)
            .border(0.5.dp, borderColor, shape)
            .then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier)
            .padding(contentPadding),
        content = content,
    )
}

/** A full-width list row with hairline-friendly padding. */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun ListRow(
    modifier: Modifier = Modifier,
    onClick: (() -> Unit)? = null,
    onLongClick: (() -> Unit)? = null,
    leading: (@Composable () -> Unit)? = null,
    trailing: (@Composable RowScope.() -> Unit)? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    val interaction = remember { MutableInteractionSource() }
    Row(
        modifier = modifier
            .fillMaxWidth()
            .then(
                if (onClick != null || onLongClick != null) {
                    Modifier.combinedClickable(
                        interactionSource = interaction,
                        indication = ripple(),
                        onClick = { onClick?.invoke() },
                        onLongClick = onLongClick,
                    )
                } else {
                    Modifier
                },
            )
            .heightIn(min = 56.dp)
            .padding(horizontal = ScreenPadding, vertical = 12.dp),
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

/** Raised is the secondary action on a tinted card, where the muted fill would sink in. */
enum class ButtonKind { Primary, Secondary, Raised, Quiet, Danger }

/** A row that asks to delete itself when swiped from the end toward the start; the row springs back either way. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SwipeToDelete(onDelete: () -> Unit, modifier: Modifier = Modifier, content: @Composable () -> Unit) {
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
    SwipeToDismissBox(
        state = state,
        enableDismissFromStartToEnd = false,
        modifier = modifier,
        backgroundContent = {
            Box(
                modifier = Modifier
                    .fillMaxSize()
                    .background(AcornTheme.colors.danger)
                    .padding(horizontal = 24.dp),
                contentAlignment = Alignment.CenterEnd,
            ) {
                Icon(Icons.Outlined.Delete, contentDescription = "删除", tint = AcornTheme.colors.onAccent)
            }
        },
    ) {
        Box(Modifier.background(AcornTheme.colors.canvas)) { content() }
    }
}

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
    val shape = if (compact) AcornShapes.small else RoundedCornerShape(12.dp)
    val active = enabled && !loading
    Box(
        modifier = modifier
            .height(if (compact) 36.dp else 48.dp)
            .alpha(if (enabled) 1f else 0.4f)
            .clip(shape)
            .background(container)
            .clickable(enabled = active, role = Role.Button, onClick = onClick)
            .padding(horizontal = if (compact) 14.dp else 20.dp),
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
                    style = if (compact) AcornTheme.type.footnote.copy(fontWeight = AcornTheme.type.label.fontWeight) else AcornTheme.type.label,
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
            .padding(horizontal = 40.dp, vertical = 48.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Box(
            modifier = Modifier
                .size(52.dp)
                .background(AcornTheme.colors.surfaceMuted, CircleShape),
            contentAlignment = Alignment.Center,
        ) {
            Icon(icon, contentDescription = null, tint = AcornTheme.colors.inkSoft, modifier = Modifier.size(24.dp))
        }
        Spacer(Modifier.height(16.dp))
        Text(title, style = AcornTheme.type.headline, color = AcornTheme.colors.ink, textAlign = TextAlign.Center)
        Spacer(Modifier.height(6.dp))
        Text(body, style = AcornTheme.type.footnote, color = AcornTheme.colors.inkSoft, textAlign = TextAlign.Center)
        action?.let {
            Spacer(Modifier.height(20.dp))
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
            .clip(AcornShapes.small)
            .background(AcornTheme.colors.dangerSoft)
            .padding(start = 14.dp, end = 4.dp, top = 4.dp, bottom = 4.dp)
            .heightIn(min = 40.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(message, style = AcornTheme.type.footnote, color = AcornTheme.colors.danger, modifier = Modifier.weight(1f))
        action?.let {
            Text(
                it,
                style = AcornTheme.type.footnote.copy(fontWeight = AcornTheme.type.label.fontWeight),
                color = AcornTheme.colors.danger,
                modifier = Modifier
                    .clip(AcornShapes.small)
                    .clickable(onClick = onAction)
                    .padding(horizontal = 10.dp, vertical = 8.dp),
            )
        }
    }
}

/** Placeholder rows that pulse while the first load runs. */
@Composable
fun SkeletonRows(modifier: Modifier = Modifier, rows: Int = 6) {
    val transition = rememberInfiniteTransition(label = "skeleton")
    val alpha by transition.animateFloat(
        initialValue = 0.55f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(tween(850), RepeatMode.Reverse),
        label = "skeleton_alpha",
    )
    val block = AcornTheme.colors.surfaceMuted
    Column(modifier = modifier.fillMaxSize().alpha(alpha)) {
        repeat(rows) { index ->
            Column(modifier = Modifier.padding(horizontal = ScreenPadding, vertical = 14.dp)) {
                Box(
                    Modifier
                        .fillMaxWidth(if (index % 2 == 0) 0.55f else 0.4f)
                        .height(14.dp)
                        .background(block, RoundedCornerShape(4.dp)),
                )
                Spacer(Modifier.height(10.dp))
                Box(
                    Modifier
                        .fillMaxWidth(if (index % 3 == 0) 0.9f else 0.75f)
                        .height(11.dp)
                        .background(block, RoundedCornerShape(4.dp)),
                )
            }
        }
    }
}

/** A small rounded tag, e.g. a state or a count. */
@Composable
fun Tag(text: String, modifier: Modifier = Modifier, color: Color = AcornTheme.colors.inkSoft, container: Color = AcornTheme.colors.surfaceMuted) {
    Text(
        text,
        style = AcornTheme.type.caption,
        color = color,
        modifier = modifier
            .clip(RoundedCornerShape(6.dp))
            .background(container)
            .padding(horizontal = 7.dp, vertical = 2.dp),
    )
}

/** Equal options in one rounded track; the selected one is raised. */
@Composable
fun SegmentedControl(options: List<String>, selected: Int, onSelect: (Int) -> Unit, modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    Row(
        modifier = modifier
            .fillMaxWidth()
            .height(36.dp)
            .clip(AcornShapes.small)
            .background(colors.surfaceMuted)
            .padding(3.dp),
    ) {
        options.forEachIndexed { index, label ->
            val active = index == selected
            Box(
                modifier = Modifier
                    .weight(1f)
                    .fillMaxHeight()
                    .clip(RoundedCornerShape(8.dp))
                    .background(if (active) colors.surface else Color.Transparent)
                    .clickable(role = Role.Tab) { onSelect(index) },
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    label,
                    style = if (active) AcornTheme.type.footnote.copy(fontWeight = AcornTheme.type.label.fontWeight) else AcornTheme.type.footnote,
                    color = if (active) colors.ink else colors.inkSoft,
                )
            }
        }
    }
}

/** A small selectable pill, for narrowing a list. */
@Composable
fun FilterPill(text: String, selected: Boolean, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    Text(
        text,
        style = AcornTheme.type.footnote,
        color = if (selected) colors.accentInk else colors.inkSoft,
        modifier = modifier
            .clip(RoundedCornerShape(50))
            .background(if (selected) colors.accentSoft else Color.Transparent)
            .border(0.5.dp, if (selected) colors.accentSoft else colors.line, RoundedCornerShape(50))
            .clickable(role = Role.RadioButton, onClick = onClick)
            .padding(horizontal = 12.dp, vertical = 6.dp),
    )
}
