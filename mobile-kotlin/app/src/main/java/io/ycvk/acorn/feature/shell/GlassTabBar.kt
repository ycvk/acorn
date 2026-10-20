package io.ycvk.acorn.feature.shell

import android.os.Build
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.spring
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.scaleIn
import androidx.compose.animation.scaleOut
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.util.lerp
import com.kyant.backdrop.Backdrop
import com.kyant.backdrop.BackdropEffectScope
import com.kyant.backdrop.backdrops.layerBackdrop
import com.kyant.backdrop.backdrops.rememberCombinedBackdrop
import com.kyant.backdrop.backdrops.rememberLayerBackdrop
import com.kyant.backdrop.drawBackdrop
import com.kyant.backdrop.effects.blur
import com.kyant.backdrop.effects.lens
import com.kyant.backdrop.effects.vibrancy
import com.kyant.backdrop.highlight.Highlight
import com.kyant.backdrop.shadow.Shadow
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornIcons
import io.ycvk.acorn.core.ui.rememberHaptics
import kotlin.math.abs
import kotlin.math.roundToInt
import kotlinx.coroutines.launch

/**
 * The tabs on a capsule of liquid glass over the page: the page blurs and
 * bends through it, and a clear lens rests on the selected tab, showing it in
 * the accent colour. The lens springs to a tapped tab, swells while a finger
 * is on the bar and follows a drag across it. Blur needs Android 12 and the
 * lens Android 13; below them the capsule's tint thickens to keep the labels
 * readable. The layering follows the LiquidBottomTabs example of Kyant's
 * backdrop library.
 */
@Composable
fun GlassTabBar(
    tab: Tab,
    pendingCount: Int,
    backdrop: Backdrop,
    onSelect: (Tab) -> Unit,
    modifier: Modifier = Modifier,
) {
    val colors = AcornTheme.colors
    val haptics = rememberHaptics()
    val tabs = Tab.entries
    val selected = tabs.indexOf(tab)
    val scope = rememberCoroutineScope()
    val onSelectNow by rememberUpdatedState(onSelect)
    val selectedNow by rememberUpdatedState(selected)

    // Where the lens sits, in tabs from the left, and how far it has swollen under a finger.
    val position = remember { Animatable(selected.toFloat()) }
    val press = remember { Animatable(0f) }
    var dragging by remember { mutableStateOf(false) }
    LaunchedEffect(selected) {
        if (!dragging) position.animateTo(selected.toFloat(), spring(dampingRatio = 0.72f, stiffness = 380f))
    }

    val surface = colors.surface.copy(alpha = glassTint(colors.isDark))
    val glass: BackdropEffectScope.() -> Unit = {
        vibrancy()
        blur(10.dp.toPx())
        lens(20.dp.toPx(), 26.dp.toPx())
    }
    // The tabs again in the accent colour, on the same glass, kept out of sight: the lens refracts this copy.
    val tinted = rememberLayerBackdrop()

    BoxWithConstraints(
        modifier = modifier
            .fillMaxWidth()
            .height(BarHeight)
            .pointerInput(tabs.size) {
                val inset = BarInset.toPx()
                val slot = (size.width - 2 * inset) / tabs.size
                awaitEachGesture {
                    val down = awaitFirstDown(requireUnconsumed = false)
                    scope.launch { press.animateTo(1f, spring(dampingRatio = 1f, stiffness = 900f)) }
                    var moved = false
                    while (true) {
                        val change = awaitPointerEvent().changes.firstOrNull { it.id == down.id } ?: break
                        if (!change.pressed) break
                        if (!moved && abs(change.position.x - down.position.x) > viewConfiguration.touchSlop) {
                            moved = true
                            dragging = true
                        }
                        if (moved) {
                            change.consume()
                            val at = ((change.position.x - inset) / slot - 0.5f).coerceIn(0f, (tabs.size - 1).toFloat())
                            scope.launch { position.animateTo(at, spring(dampingRatio = 1f, stiffness = 1600f)) }
                        }
                    }
                    scope.launch { press.animateTo(0f, spring(dampingRatio = 0.6f, stiffness = 420f)) }
                    if (moved) {
                        dragging = false
                        val target = position.targetValue.roundToInt().coerceIn(0, tabs.lastIndex)
                        if (target != selectedNow) {
                            haptics.tick()
                            onSelectNow(tabs[target])
                        }
                        scope.launch { position.animateTo(target.toFloat(), spring(dampingRatio = 0.72f, stiffness = 380f)) }
                    }
                }
            },
    ) {
        val slot = (maxWidth - BarInset * 2) / tabs.size

        Row(
            modifier = Modifier
                .fillMaxSize()
                .drawBackdrop(
                    backdrop = backdrop,
                    shape = { AcornShapes.pill },
                    effects = glass,
                    shadow = { Shadow(radius = 24.dp, alpha = if (colors.isDark) 0.5f else 0.18f) },
                    onDrawSurface = { drawRect(surface) },
                )
                .padding(horizontal = BarInset),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            tabs.forEach { item ->
                TabItem(
                    tab = item,
                    tint = colors.inkSoft,
                    badge = if (item == Tab.Today) pendingCount else 0,
                    modifier = Modifier
                        .weight(1f)
                        .fillMaxHeight()
                        .semantics { this.selected = item == tab }
                        .clickable(
                            interactionSource = remember { MutableInteractionSource() },
                            indication = null,
                            role = Role.Tab,
                        ) {
                            if (item != tab) haptics.tick()
                            onSelect(item)
                        },
                )
            }
        }

        Row(
            modifier = Modifier
                .fillMaxSize()
                .clearAndSetSemantics {}
                .alpha(0f)
                .layerBackdrop(tinted)
                .drawBackdrop(
                    backdrop = backdrop,
                    shape = { AcornShapes.pill },
                    effects = glass,
                    shadow = { null },
                    onDrawSurface = { drawRect(surface) },
                )
                .padding(horizontal = BarInset),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            tabs.forEach { item ->
                TabItem(
                    tab = item,
                    tint = colors.accent,
                    badge = if (item == Tab.Today) pendingCount else 0,
                    modifier = Modifier.weight(1f).fillMaxHeight(),
                )
            }
        }

        Box(
            modifier = Modifier
                .padding(BarInset)
                .graphicsLayer { translationX = position.value * slot.toPx() }
                .width(slot)
                .fillMaxHeight()
                .drawBackdrop(
                    backdrop = rememberCombinedBackdrop(backdrop, tinted),
                    shape = { AcornShapes.pill },
                    effects = {
                        // Nearly flat at rest; under a finger the lens deepens and splits colour at its rim.
                        val p = press.value
                        lens(lerp(4f, 12f, p).dp.toPx(), lerp(6f, 14f, p).dp.toPx(), chromaticAberration = p > 0.05f)
                    },
                    highlight = { Highlight.Default.copy(alpha = lerp(0.55f, 1f, press.value)) },
                    shadow = { Shadow(radius = 12.dp, alpha = 0.25f * press.value) },
                    layerBlock = {
                        val scale = lerp(1f, 1.14f, press.value)
                        scaleX = scale
                        scaleY = scale
                    },
                    onDrawSurface = {
                        drawRect(if (colors.isDark) Color.White.copy(alpha = 0.06f) else Color.Black.copy(alpha = 0.04f))
                    },
                ),
        )
    }
}

/** How thick the capsule's tint is: thin over the lens, thicker over blur alone, near solid with neither. */
private fun glassTint(dark: Boolean): Float = when {
    Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU -> if (dark) 0.5f else 0.55f
    Build.VERSION.SDK_INT >= Build.VERSION_CODES.S -> 0.72f
    else -> 0.94f
}

private val BarHeight = 64.dp
private val BarInset = 4.dp

@Composable
private fun TabItem(tab: Tab, tint: Color, badge: Int, modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    val (label, icon) = tabLabel(tab)
    Column(
        modifier = modifier,
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Box {
            Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.size(22.dp))
            androidx.compose.animation.AnimatedVisibility(
                visible = badge > 0,
                enter = scaleIn() + fadeIn(),
                exit = scaleOut() + fadeOut(),
                modifier = Modifier
                    .align(Alignment.TopEnd)
                    .offset(x = 12.dp, y = (-5).dp),
            ) {
                Text(
                    if (badge > 9) "9+" else badge.toString(),
                    style = AcornTheme.type.caption.copy(fontSize = 10.sp, fontWeight = FontWeight.SemiBold, lineHeight = 14.sp),
                    color = colors.onAccent,
                    modifier = Modifier
                        .background(colors.accent, AcornShapes.pill)
                        .padding(horizontal = 5.dp),
                )
            }
        }
        Spacer(Modifier.height(3.dp))
        Text(label, style = AcornTheme.type.caption.copy(fontSize = 10.5.sp, fontWeight = FontWeight.Medium), color = tint)
    }
}

private fun tabLabel(tab: Tab): Pair<String, ImageVector> = when (tab) {
    Tab.Today -> "今天" to AcornIcons.Today
    Tab.Threads -> "对话" to AcornIcons.Threads
    Tab.Knowledge -> "知识" to AcornIcons.Knowledge
}
