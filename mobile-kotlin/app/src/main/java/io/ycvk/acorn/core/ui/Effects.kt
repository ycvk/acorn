/*
 * Ported from border-beam by Jakub Antalik (libraries.dev), under the MIT License:
 *
 * Copyright (c) 2026 Jakub Antalik
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */
package io.ycvk.acorn.core.ui

import android.provider.Settings
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.tween
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.CompositingStrategy
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import io.ycvk.acorn.core.theme.AcornTheme
import kotlin.math.abs
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

/** Whether the owner turned animations off (developer options or accessibility); loops then rest on one frame. */
@Composable
fun rememberReducedMotion(): Boolean {
    val context = LocalContext.current
    return remember(context) {
        Settings.Global.getFloat(context.contentResolver, Settings.Global.ANIMATOR_DURATION_SCALE, 1f) == 0f
    }
}

/**
 * The colours a beam lights up as it passes, pinned around the edge (angles
 * clockwise from three o'clock), after libraries.dev's border beam (MIT).
 */
enum class BeamPalette(internal val stops: List<Pair<Float, Color>>) {
    /** The full spectrum, for the agent at work. */
    Colorful(
        listOf(
            0f to Color(240, 50, 180),
            30f to Color(180, 40, 240),
            80f to Color(100, 70, 255),
            115f to Color(40, 140, 255),
            160f to Color(30, 185, 170),
            195f to Color(50, 200, 80),
            235f to Color(40, 140, 255),
            268f to Color(255, 50, 100),
            312f to Color(255, 120, 40),
        ),
    ),

    /** Warm ambers and reds, for something waiting on the owner. */
    Sunset(
        listOf(
            0f to Color(255, 100, 80),
            60f to Color(255, 160, 40),
            120f to Color(255, 80, 50),
            180f to Color(255, 200, 50),
            240f to Color(255, 120, 60),
            300f to Color(255, 170, 70),
        ),
    ),
}

/**
 * A soft coloured light that rides the border of a box rounded by
 * [cornerRadius] while [active]: a window turns around the edge every
 * [period] ms and lights the palette's colours where it passes, with a
 * brighter head and a glow inside the edge. It fades in and out instead of
 * switching, and rests in place when the system has animations off.
 */
@Composable
fun Modifier.borderBeam(
    active: Boolean,
    cornerRadius: Dp,
    palette: BeamPalette = BeamPalette.Colorful,
    width: Dp = 1.dp,
    period: Int = 2400,
): Modifier {
    val colors = AcornTheme.colors
    val reduced = rememberReducedMotion()
    val fade = remember { Animatable(0f) }
    val angle = remember { mutableFloatStateOf(300f) }
    LaunchedEffect(active, reduced) {
        launch { fade.animateTo(if (active) 1f else 0f, tween(if (active) 600 else 500, easing = LinearEasing)) }
        if (reduced) return@LaunchedEffect
        var last = withFrameNanos { it }
        while (isActive && (active || fade.value > 0f)) {
            withFrameNanos { now ->
                angle.floatValue = (angle.floatValue + (now - last) / 1e6f / period * 360f) % 360f
                last = now
            }
        }
    }
    return drawWithContent {
        drawContent()
        val strength = fade.value
        if (strength > 0f) drawBeam(cornerRadius.toPx(), palette, angle.floatValue, strength, width.toPx(), colors.isDark)
    }
}

private fun DrawScope.drawBeam(radius: Float, palette: BeamPalette, angle: Float, strength: Float, width: Float, dark: Boolean) {
    val center = Offset(size.width / 2, size.height / 2)
    val steps = 48
    // The beam's window: dark until 30% of the turn, full from 52% to 80%, gone by 95%.
    val lit = Brush.sweepGradient(
        List(steps + 1) { i ->
            val theta = i * 360f / steps
            val w = beamWindow(theta, angle)
            val c = paletteAt(palette, theta)
            c.copy(alpha = w * strength)
        },
        center,
    )
    val headColor = if (dark) Color.White else Color.Black
    val head = Brush.sweepGradient(
        List(steps + 1) { i ->
            val theta = i * 360f / steps
            headColor.copy(alpha = beamHead(theta, angle) * strength * if (dark) 0.75f else 0.35f)
        },
        center,
    )
    // Every layer is a band just inside the edge, drawn as a rounded-rect stroke so the GPU
    // renders it analytically, without a clip or a rasterised path.
    fun band(w: Float, brush: Brush, alpha: Float) {
        val inset = w / 2
        drawRoundRect(
            brush = brush,
            topLeft = Offset(inset, inset),
            size = Size(size.width - w, size.height - w),
            cornerRadius = CornerRadius((radius - inset).coerceAtLeast(0f)),
            alpha = alpha,
            style = Stroke(w),
        )
    }
    // Inside glow: wide, faint bands stacked toward the edge, read as a blur.
    val glow = if (dark) 0.09f else 0.07f
    listOf(11.dp, 8.dp, 5.5.dp, 3.5.dp, 2.dp).forEach { band(it.toPx(), lit, glow) }
    band(3.dp.toPx(), head, 0.35f)
    band(width, lit, if (dark) 0.95f else 0.85f)
    band(width, head, 1f)
}

/** Where on the turn a point at [theta] (from three o'clock) sits relative to the beam, as the CSS conic mask. */
private fun beamFraction(theta: Float, angle: Float): Float {
    // CSS conic gradients start at twelve o'clock; sweep gradients at three.
    val f = ((theta + 90f - angle) % 360f + 360f) % 360f
    return f / 360f
}

private fun beamWindow(theta: Float, angle: Float): Float = ramp(
    beamFraction(theta, angle),
    listOf(0f to 0f, 0.30f to 0f, 0.36f to 0.1f, 0.44f to 0.35f, 0.52f to 1f, 0.80f to 1f, 0.86f to 0.35f, 0.92f to 0.1f, 0.95f to 0f, 1f to 0f),
)

private fun beamHead(theta: Float, angle: Float): Float = ramp(
    beamFraction(theta, angle),
    listOf(0f to 0f, 0.54f to 0f, 0.57f to 0.13f, 0.60f to 0.4f, 0.63f to 0.8f, 0.66f to 1f, 0.69f to 0.8f, 0.72f to 0.4f, 0.75f to 0.13f, 0.78f to 0f, 1f to 0f),
)

private fun ramp(x: Float, points: List<Pair<Float, Float>>): Float {
    for (i in 1 until points.size) {
        val (x1, y1) = points[i]
        if (x <= x1) {
            val (x0, y0) = points[i - 1]
            return if (x1 == x0) y1 else y0 + (y1 - y0) * (x - x0) / (x1 - x0)
        }
    }
    return points.last().second
}

private fun paletteAt(palette: BeamPalette, theta: Float): Color {
    val stops = palette.stops
    for (i in stops.indices) {
        val (a0, c0) = stops[i]
        val (a1raw, c1) = stops[(i + 1) % stops.size]
        val a1 = if (a1raw <= a0) a1raw + 360f else a1raw
        val t = if (theta < a0) theta + 360f else theta
        if (t in a0..a1) return lerp(c0, c1, (t - a0) / (a1 - a0))
    }
    return stops.first().second
}

/**
 * A chrome ring around a round control, after the look of libraries.dev's
 * liquid metal: bands of light and shade around the rim with one bright halo
 * that wanders round it while [active].
 */
@Composable
fun Modifier.metalRing(active: Boolean, width: Dp = 1.5.dp, period: Int = 4200): Modifier {
    val dark = AcornTheme.colors.isDark
    val reduced = rememberReducedMotion()
    val angle = remember { mutableFloatStateOf(35f) }
    LaunchedEffect(active, reduced) {
        if (!active || reduced) return@LaunchedEffect
        var last = withFrameNanos { it }
        while (isActive) {
            withFrameNanos { now ->
                angle.floatValue = (angle.floatValue + (now - last) / 1e6f / period * 360f) % 360f
                last = now
            }
        }
    }
    return drawWithContent {
        drawContent()
        val stroke = width.toPx()
        val radius = size.minDimension / 2 - stroke / 2
        val center = Offset(size.width / 2, size.height / 2)
        val bands = if (dark) {
            listOf(Color(0xFF5C5C60), Color(0xFFE8E8EC), Color(0xFF3A3A3E), Color(0xFFB8B8BE), Color(0xFF4A4A4E), Color(0xFFF4F4F7), Color(0xFF5C5C60))
        } else {
            listOf(Color(0xFF8E8E94), Color(0xFFFFFFFF), Color(0xFF5E5E64), Color(0xFFD9D9DE), Color(0xFF6E6E74), Color(0xFFFFFFFF), Color(0xFF8E8E94))
        }
        drawCircle(Brush.sweepGradient(bands, center), radius, center, style = Stroke(stroke))
        // The halo: a short bright arc that drifts round the rim, with a faint bloom outside it.
        val a = angle.floatValue
        val halo = Brush.sweepGradient(
            List(37) { i ->
                val theta = i * 10f
                val d = abs(((theta - a) % 360f + 540f) % 360f - 180f)
                Color.White.copy(alpha = (1f - d / 50f).coerceIn(0f, 1f))
            },
            center,
        )
        drawCircle(halo, radius, center, style = Stroke(stroke))
        drawCircle(halo, radius + stroke * 1.5f, center, alpha = if (dark) 0.25f else 0.12f, style = Stroke(stroke * 3))
    }
}

/**
 * A light band that sweeps across text (or any content) while [active], for
 * a status that is still in progress.
 */
@Composable
fun Modifier.shimmer(active: Boolean, period: Int = 1800): Modifier {
    val colors = AcornTheme.colors
    val reduced = rememberReducedMotion()
    val sweep = remember { mutableFloatStateOf(-0.3f) }
    LaunchedEffect(active, reduced) {
        if (!active || reduced) return@LaunchedEffect
        val start = withFrameNanos { it }
        while (isActive) {
            withFrameNanos { now -> sweep.floatValue = (((now - start) / 1e6f) % period) / period * 1.6f - 0.3f }
        }
    }
    if (!active || reduced) return this
    val shine = if (colors.isDark) Color.White else colors.ink
    return graphicsLayer(compositingStrategy = CompositingStrategy.Offscreen)
        .drawWithContent {
            drawContent()
            val band = size.width * 0.28f
            val x = size.width * sweep.floatValue
            drawRect(
                brush = Brush.horizontalGradient(
                    listOf(Color.Transparent, shine.copy(alpha = 0.9f), Color.Transparent),
                    startX = x - band,
                    endX = x + band,
                ),
                blendMode = BlendMode.SrcAtop,
            )
        }
}
