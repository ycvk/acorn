/*
 * Ported from thinking-orbs by Jakub Antalik (libraries.dev), under the MIT License:
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

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import io.ycvk.acorn.core.theme.AcornTheme
import kotlin.math.PI
import kotlin.math.abs
import kotlin.math.acos
import kotlin.math.atan2
import kotlin.math.cos
import kotlin.math.exp
import kotlin.math.floor
import kotlin.math.max
import kotlin.math.min
import kotlin.math.pow
import kotlin.math.roundToInt
import kotlin.math.sin
import kotlin.math.sqrt
import kotlinx.coroutines.isActive

/**
 * What the agent is doing, as one of the dot-sphere forms of libraries.dev's
 * thinking orbs, ported from its canvas engine: a breathing ring while it
 * thinks, a ribbon while it writes, a scanned globe while it looks things up,
 * orbits while it works on its machine, a web while it reaches a skill, a
 * turning cube while it reasons, a braid while it writes things down, and
 * slow waves while it listens.
 */
enum class OrbState(internal val mode: OrbMode) {
    Thinking(OrbMode.Ring),
    Composing(OrbMode.Ribbon),
    Searching(OrbMode.Globe),
    Working(OrbMode.Orbits),
    Connecting(OrbMode.Web),
    Solving(OrbMode.Rubik),
    Weaving(OrbMode.Braid),
    Listening(OrbMode.Wave),
}

/**
 * Draws the orb for [state], animating unless [animated] is off or the system
 * has animations turned off; a still orb shows one fixed frame.
 */
@Composable
fun ThinkingOrb(
    state: OrbState,
    modifier: Modifier = Modifier,
    size: Dp = 20.dp,
    speed: Float = 1f,
    animated: Boolean = true,
    tint: Color? = null,
    contentDescription: String? = null,
) {
    val colors = AcornTheme.colors
    val preset = remember(state, size) { resolvePreset(state.mode, size.value) }
    val engine = remember { OrbEngine() }
    val time = remember { mutableFloatStateOf(STILL_TIME) }
    val moving = animated && !rememberReducedMotion()
    LaunchedEffect(moving, preset, speed) {
        if (!moving) {
            time.floatValue = STILL_TIME
            return@LaunchedEffect
        }
        val start = withFrameNanos { it }
        val offset = time.floatValue.toDouble()
        while (isActive) {
            withFrameNanos { now ->
                time.floatValue = (offset + (now - start) / 1e9 * preset.speed * speed).toFloat()
            }
        }
    }
    val semantics = if (contentDescription != null) Modifier.semantics { this.contentDescription = contentDescription } else Modifier
    Canvas(modifier.then(semantics).size(size)) {
        val frame = engine.frame(preset, size.value, time.floatValue)
        val unit = this.size.minDimension / size.value
        val dark = colors.isDark
        for (i in 0 until frame.lineCount) {
            drawLine(
                color = inkColor(frame.lw[i], frame.la[i], dark, tint),
                start = Offset(frame.lx1[i] * unit, frame.ly1[i] * unit),
                end = Offset(frame.lx2[i] * unit, frame.ly2[i] * unit),
                strokeWidth = frame.lwidth[i] * unit,
            )
        }
        for (k in 0 until frame.count) {
            val i = frame.order[k]
            drawCircle(
                color = inkColor(frame.white[i], frame.a[i], dark, tint),
                radius = frame.r[i] * unit,
                center = Offset(frame.x[i] * unit, frame.y[i] * unit),
            )
        }
    }
}

/** The frame a still orb shows, as the original does under reduced motion. */
private const val STILL_TIME = 0.6f

/** A grey (or tinted) step between the ink and the canvas: white 0 is full ink, 1 is the background. */
private fun inkColor(white: Float, alpha: Float, dark: Boolean, tint: Color?): Color {
    val w = white.coerceIn(0f, 1f)
    val a = alpha.coerceIn(0f, 1f)
    if (tint == null) {
        val g = if (dark) 1f - w else w
        return Color(g, g, g, a)
    }
    fun ramp(c: Float) = if (dark) c * (1f - w) else c + (1f - c) * w
    return Color(ramp(tint.red), ramp(tint.green), ramp(tint.blue), a)
}

internal enum class OrbMode { Globe, Orbits, Rubik, Wave, Web, Braid, Ribbon, Ring }

/** Counts, radii and shaping knobs of one form at one size; zero counts are absent. */
internal data class OrbProfile(
    val latRings: Int = 0,
    val lonDensity: Int = 0,
    val rings: Int = 0,
    val lanes: Int = 0,
    val segs: Int = 0,
    val orbitN: Int = 0,
    val ghostN: Int = 0,
    val nodeN: Int = 0,
    val strandN: Int = 0,
    val signals: Int = 0,
    val rBase: Float = 0f,
    val rDepth: Float = 0f,
    val rActive: Float = 0f,
    val ghostR: Float = 0f,
    val partR: Float = 0f,
    val partRDepth: Float = 0f,
    val nodeR: Float = 0f,
    val nodeRDepth: Float = 0f,
    val rBoost: Float = 1f,
    val inkFar: Float = 0.62f,
    val inkSpan: Float = 0.54f,
    val rsPow: Float = 0.6f,
    val rMin: Float = 0.3f,
    val ghostA: Float = 0.5f,
    val particles: Int = 3,
    val moveCount: Int = 14,
    val thr: Float = 0.72f,
    val lineW: Float = 0.8f,
    val turns: Int = 3,
    val scanMul: Float = 1f,
    val dimBase: Float = 1f,
    val spin: Float = 1f,
    val bandMul: Float = 1f,
    val wobMul: Float = 1f,
    val faceOn: Boolean = false,
    val spread: Float = 1f,
)

internal data class OrbPreset(val mode: OrbMode, val speed: Float, val profile: OrbProfile)

private val BaseProfiles = mapOf(
    OrbMode.Globe to OrbProfile(latRings = 17, lonDensity = 44, rBase = 0.6f, rDepth = 1.7f),
    OrbMode.Orbits to OrbProfile(orbitN = 12, ghostN = 40, ghostR = 0.9f, partR = 1.2f, partRDepth = 1.6f),
    OrbMode.Rubik to OrbProfile(latRings = 15, lonDensity = 40, rBase = 0.6f, rDepth = 1.7f, rActive = 0.3f),
    OrbMode.Wave to OrbProfile(rings = 15, lonDensity = 40, rBase = 0.6f, rDepth = 1.7f),
    OrbMode.Web to OrbProfile(nodeN = 30, signals = 5, nodeR = 1.4f, nodeRDepth = 1.8f),
    OrbMode.Braid to OrbProfile(strandN = 52, ghostN = 150, rBase = 1.2f, rDepth = 1.8f),
    OrbMode.Ribbon to OrbProfile(lanes = 5, segs = 88, ghostN = 150, rBase = 1.1f, rDepth = 1.7f),
    OrbMode.Ring to OrbProfile(lanes = 5, segs = 88, faceOn = true, rBase = 1.1f, rDepth = 1.7f),
)

private class SizePreset(val speed: Float, val count: Float, val size: Float, val extra: (OrbProfile) -> OrbProfile = { it })

/** Hand-tuned speed, density and dot size per form at 64, 32 and 20 px. */
private val Presets: Map<OrbMode, Map<Int, SizePreset>> = mapOf(
    OrbMode.Orbits to mapOf(
        64 to SizePreset(1.885f, 1f, 1f),
        32 to SizePreset(2.9072f, 0.4251f, 1.6849f),
        20 to SizePreset(3.9f, 0.238f, 2.4f),
    ),
    OrbMode.Globe to mapOf(
        64 to SizePreset(2.015f, 0.42f, 1.15f) { it.copy(scanMul = 4.08f, dimBase = 0.45f) },
        32 to SizePreset(2.3803f, 0.1839f, 1.4769f) { it.copy(scanMul = 4.2301f, dimBase = 0.45f) },
        20 to SizePreset(2.665f, 0.105f, 1.75f) { it.copy(scanMul = 4.335f, dimBase = 0.45f) },
    ),
    OrbMode.Rubik to mapOf(
        64 to SizePreset(1.82f, 0.35f, 1.05f),
        32 to SizePreset(1.8964f, 0.1537f, 1.4951f),
        20 to SizePreset(1.95f, 0.088f, 1.9f),
    ),
    OrbMode.Wave to mapOf(
        64 to SizePreset(4.388f, 0.341f, 1f),
        32 to SizePreset(4.1512f, 0.169f, 1.3232f),
        20 to SizePreset(3.998f, 0.105f, 1.6f),
    ),
    OrbMode.Web to mapOf(
        64 to SizePreset(3.315f, 1.35f, 0.95f),
        32 to SizePreset(5.0104f, 0.4942f, 1.2571f),
        20 to SizePreset(6.63f, 0.25f, 1.52f),
    ),
    OrbMode.Braid to mapOf(
        64 to SizePreset(1.625f, 0.5f, 1f),
        32 to SizePreset(2.2234f, 0.2056f, 1.2011f),
        20 to SizePreset(2.75f, 0.1125f, 1.36f),
    ),
    OrbMode.Ribbon to mapOf(
        64 to SizePreset(2.34f, 0.25f, 0.85f) { it.copy(spin = 0f, bandMul = 3.9f, wobMul = 1f) },
        32 to SizePreset(2.7776f, 0.0969f, 0.9766f) { it.copy(spin = 0f, bandMul = 4.49f, wobMul = 1f) },
        20 to SizePreset(3.12f, 0.051f, 1.073f) { it.copy(spin = 0f, bandMul = 4.94f, wobMul = 1f) },
    ),
    OrbMode.Ring to mapOf(
        64 to SizePreset(3.24f, 0.25f, 0.956f) { it.copy(spin = 0f, bandMul = 3.627f, wobMul = 0.368f) },
        32 to SizePreset(3.5517f, 0.0678f, 1.31f) { it.copy(spin = 0f, bandMul = 3.8265f, wobMul = 0.4751f) },
        20 to SizePreset(3.78f, 0.028f, 1.622f) { it.copy(spin = 0f, bandMul = 3.968f, wobMul = 0.565f) },
    ),
)

/** The tuned preset nearest to [sizeDp]; larger orbs use the 64 px tuning. */
internal fun resolvePreset(mode: OrbMode, sizeDp: Float): OrbPreset {
    val key = when {
        sizeDp >= 48f -> 64
        sizeDp >= 26f -> 32
        else -> 20
    }
    val preset = Presets.getValue(mode).getValue(key)
    var profile = BaseProfiles.getValue(mode)
    if (preset.count != 1f) profile = scaleCounts(profile, preset.count)
    if (preset.size != 1f) profile = scaleRadii(profile, preset.size)
    return OrbPreset(mode, preset.speed, preset.extra(profile))
}

private fun scaleCounts(p: OrbProfile, scale: Float): OrbProfile {
    val rt = sqrt(scale)
    fun pair(v: Int) = max(2, (v * rt).roundToInt())
    fun single(v: Int) = if (v == 0) 0 else max(1, (v * scale).roundToInt())
    var out = p
    out = when {
        p.latRings > 0 && p.lonDensity > 0 -> out.copy(latRings = pair(p.latRings), lonDensity = pair(p.lonDensity))
        p.rings > 0 && p.lonDensity > 0 -> out.copy(rings = pair(p.rings), lonDensity = pair(p.lonDensity))
        else -> out
    }
    if (p.lanes > 0 && p.segs > 0) out = out.copy(lanes = pair(p.lanes), segs = pair(p.segs))
    return out.copy(
        orbitN = single(p.orbitN),
        ghostN = single(p.ghostN),
        nodeN = single(p.nodeN),
        strandN = single(p.strandN),
        signals = single(p.signals),
    )
}

private fun scaleRadii(p: OrbProfile, scale: Float) = p.copy(
    rBase = p.rBase * scale,
    rDepth = p.rDepth * scale,
    rActive = p.rActive * scale,
    ghostR = p.ghostR * scale,
    partR = p.partR * scale,
    partRDepth = p.partRDepth * scale,
    nodeR = p.nodeR * scale,
    nodeRDepth = p.nodeRDepth * scale,
)

/** One frame's dots, back to front in [order], and lines; buffers are reused between frames. */
internal class OrbFrame {
    var count = 0
    var x = FloatArray(256)
    var y = FloatArray(256)
    var z = FloatArray(256)
    var r = FloatArray(256)
    var white = FloatArray(256)
    var a = FloatArray(256)
    var order = IntArray(256)
    var lineCount = 0
    var lx1 = FloatArray(64)
    var ly1 = FloatArray(64)
    var lx2 = FloatArray(64)
    var ly2 = FloatArray(64)
    var lw = FloatArray(64)
    var la = FloatArray(64)
    var lwidth = FloatArray(64)

    fun reset() {
        count = 0
        lineCount = 0
    }

    fun dot(px: Float, py: Float, pz: Float, radius: Float, w: Float, alpha: Float = 1f) {
        if (alpha < 0.02f) return
        if (count == x.size) grow()
        x[count] = px
        y[count] = py
        z[count] = pz
        r[count] = radius
        white[count] = w
        a[count] = alpha
        count++
    }

    fun line(x1: Float, y1: Float, x2: Float, y2: Float, w: Float, alpha: Float, width: Float) {
        if (alpha < 0.02f) return
        if (lineCount == lx1.size) growLines()
        lx1[lineCount] = x1
        ly1[lineCount] = y1
        lx2[lineCount] = x2
        ly2[lineCount] = y2
        lw[lineCount] = w
        la[lineCount] = alpha
        lwidth[lineCount] = width
        lineCount++
    }

    /** Clamps radii to [rMin] and orders dots far to near (insertion sort; frames hold a few hundred dots). */
    fun finish(rMin: Float): OrbFrame {
        for (i in 0 until count) {
            if (r[i] < rMin) r[i] = rMin
            order[i] = i
        }
        for (i in 1 until count) {
            val key = order[i]
            val kz = z[key]
            var j = i - 1
            while (j >= 0 && z[order[j]] > kz) {
                order[j + 1] = order[j]
                j--
            }
            order[j + 1] = key
        }
        return this
    }

    private fun grow() {
        val n = x.size * 2
        x = x.copyOf(n)
        y = y.copyOf(n)
        z = z.copyOf(n)
        r = r.copyOf(n)
        white = white.copyOf(n)
        a = a.copyOf(n)
        order = order.copyOf(n)
    }

    private fun growLines() {
        val n = lx1.size * 2
        lx1 = lx1.copyOf(n)
        ly1 = ly1.copyOf(n)
        lx2 = lx2.copyOf(n)
        ly2 = ly2.copyOf(n)
        lw = lw.copyOf(n)
        la = la.copyOf(n)
        lwidth = lwidth.copyOf(n)
    }
}

/** A rotation by yaw, then a tilt toward the viewer, then a scale about the centre. */
private class Projection(yaw: Float, tilt: Float, private val cx: Float, private val cy: Float, private val scale: Float) {
    private val st = sin(tilt)
    private val ct = cos(tilt)
    private val sy = sin(yaw)
    private val cyw = cos(yaw)
    var px = 0f
    var py = 0f
    var pz = 0f

    fun project(x: Float, y: Float, z: Float) {
        val x1 = x * cyw + z * sy
        val z1 = -x * sy + z * cyw
        val y1 = y * ct - z1 * st
        val z2 = y * st + z1 * ct
        px = cx + x1 * scale
        py = cy - y1 * scale
        pz = z2
    }
}

/** Computes orb frames; one engine per drawn orb so buffers are never shared across threads. */
internal class OrbEngine {
    private val frame = OrbFrame()

    fun frame(preset: OrbPreset, size: Float, t: Float): OrbFrame {
        frame.reset()
        val p = preset.profile
        when (preset.mode) {
            OrbMode.Globe -> globe(size, t, p)
            OrbMode.Orbits -> orbits(size, t, p)
            OrbMode.Rubik -> rubik(size, t, p)
            OrbMode.Wave -> wave(size, t, p)
            OrbMode.Web -> web(size, t, p)
            OrbMode.Braid -> braid(size, t, p)
            OrbMode.Ribbon, OrbMode.Ring -> ribbon(size, t, p)
        }
        return frame.finish(p.rMin)
    }

    private fun globe(size: Float, t: Float, o: OrbProfile) {
        val spin = 0.5f
        val radius = size / 2 * 0.82f
        val tilt = 0.4f + 0.06f * sin(t * 0.35f)
        val pt = Projection(t * spin, tilt, size / 2, size / 2, radius)
        val scan = t * (spin + (1.7f - spin) * o.scanMul)
        val rs = radiusScale(size, o.rsPow)
        for (li in 0..o.latRings) {
            val lat = -PI.toFloat() / 2 + li.toFloat() / o.latRings * PI.toFloat()
            val cosLat = cos(lat)
            val sinLat = sin(lat)
            val lonCount = max(1, (abs(cosLat) * o.lonDensity).roundToInt())
            for (lj in 0 until lonCount) {
                val lon = lj.toFloat() / lonCount * 2 * PI.toFloat()
                pt.project(cosLat * cos(lon), sinLat, cosLat * sin(lon))
                val depth = (pt.pz + 1) / 2
                val d = angleDelta(lon + t * spin, scan)
                val boost = exp(-(d * d) / 0.18f) * max(0f, pt.pz)
                frame.dot(
                    pt.px, pt.py, pt.pz,
                    (o.rBase + o.rDepth * depth + o.rBoost * boost) * rs,
                    o.inkFar - o.inkSpan * depth,
                    o.dimBase + (1 - o.dimBase) * min(1f, boost),
                )
            }
        }
    }

    private fun orbits(size: Float, t: Float, o: OrbProfile) {
        val r = size / 2 * 0.82f
        val pt = Projection(t * 0.12f, 0.3f, size / 2, size / 2, 1f)
        val rs = radiusScale(size, o.rsPow)
        for (orb in 0 until o.orbitN) {
            val h1 = hashD(orb.toDouble(), 1.7)
            val h2 = hashD(orb.toDouble(), 5.2)
            val h3 = hashD(orb.toDouble(), 8.9)
            val ro = r * (0.45f + 0.52f * h1)
            val th = h1 * 2 * PI.toFloat()
            val phi = acos(2 * h2 - 1)
            val nx = sin(phi) * cos(th)
            val ny = cos(phi)
            val nz = sin(phi) * sin(th)
            var ux = -ny
            var uy = nx
            val uz = 0f
            val ul = max(1e-6f, sqrt(ux * ux + uy * uy))
            ux /= ul
            uy /= ul
            val vx = ny * uz - nz * uy
            val vy = nz * ux - nx * uz
            val vz = nx * uy - ny * ux
            val speed = (0.25f + 0.55f * h3) * if (h3 > 0.5f) 1f else -1f
            for (k in 0 until o.ghostN) {
                val ang = k.toFloat() / o.ghostN * 2 * PI.toFloat()
                pt.project(
                    (ux * cos(ang) + vx * sin(ang)) * ro,
                    (uy * cos(ang) + vy * sin(ang)) * ro,
                    (uz * cos(ang) + vz * sin(ang)) * ro,
                )
                val depth = (pt.pz / ro + 1) / 2
                frame.dot(pt.px, pt.py, pt.pz, o.ghostR * rs, 0.72f, o.ghostA * (0.4f + 0.6f * depth))
            }
            for (m in 0 until o.particles) {
                val ang = t * speed + m.toFloat() / o.particles * 2 * PI.toFloat() + h2 * 6
                pt.project(
                    (ux * cos(ang) + vx * sin(ang)) * ro,
                    (uy * cos(ang) + vy * sin(ang)) * ro,
                    (uz * cos(ang) + vz * sin(ang)) * ro,
                )
                val depth = (pt.pz / ro + 1) / 2
                frame.dot(pt.px, pt.py, pt.pz, (o.partR + o.partRDepth * depth) * rs, 0.3f - 0.22f * depth)
            }
        }
    }

    private val moves = HashMap<Int, List<Move>>()

    private fun rubik(size: Float, t: Float, o: OrbProfile) {
        val r = size / 2 * 0.82f
        val pt = Projection(t * 0.55f, 0.35f + 0.1f * sin(t * 0.9f), size / 2, size / 2, r)
        val rs = radiusScale(size, o.rsPow)
        val moveList = moves.getOrPut(o.moveCount) { makeMoves(o.moveCount) }
        val cycle = solveCycle(t, o.moveCount, 0.42f, 1.2f)
        for (li in 0..o.latRings) {
            val lat = -PI.toFloat() / 2 + li.toFloat() / o.latRings * PI.toFloat()
            val cosLat = cos(lat)
            val sinLat = sin(lat)
            val lonCount = max(1, (abs(cosLat) * o.lonDensity).roundToInt())
            for (lj in 0 until lonCount) {
                val lon = lj.toFloat() / lonCount * 2 * PI.toFloat()
                var x = cosLat * cos(lon)
                var y = sinLat
                var z = cosLat * sin(lon)
                var inActive = false
                for (i in moveList.indices) {
                    if (cycle.amount[i] <= 0f) continue
                    val mv = moveList[i]
                    val coord = when (mv.axis) {
                        0 -> x
                        1 -> y
                        else -> z
                    }
                    if (coord < mv.lo || coord >= mv.hi) continue
                    if (i == cycle.active) inActive = true
                    val ang = mv.ang * cycle.amount[i]
                    val ca = cos(ang)
                    val sa = sin(ang)
                    when (mv.axis) {
                        0 -> {
                            val y2 = y * ca - z * sa
                            z = y * sa + z * ca
                            y = y2
                        }
                        1 -> {
                            val x2 = x * ca + z * sa
                            z = -x * sa + z * ca
                            x = x2
                        }
                        else -> {
                            val x2 = x * ca - y * sa
                            y = x * sa + y * ca
                            x = x2
                        }
                    }
                }
                pt.project(x, y, z)
                val depth = (pt.pz + 1) / 2
                frame.dot(
                    pt.px, pt.py, pt.pz,
                    (o.rBase + o.rDepth * depth + if (inActive) o.rActive else 0f) * rs,
                    o.inkFar - o.inkSpan * depth - if (inActive) 0.14f else 0f,
                )
            }
        }
    }

    private fun wave(size: Float, t: Float, o: OrbProfile) {
        val r = size / 2 * 0.874f
        val pt = Projection(t * 0.18f, 0.38f, size / 2, size / 2, 1f)
        val rs = radiusScale(size, o.rsPow)
        for (ri in 0..o.rings) {
            val lat = -PI.toFloat() / 2 + ri.toFloat() / o.rings * PI.toFloat()
            val cosLat = cos(lat)
            val sinLat = sin(lat)
            val w = 0.62f * sin(t * 2.1f - ri * 0.52f) + 0.38f * sin(t * 1.27f + ri * 0.83f)
            val rr = r * (0.88f + 0.105f * w)
            val lonCount = max(1, (abs(cosLat) * o.lonDensity).roundToInt())
            for (lj in 0 until lonCount) {
                val lon = lj.toFloat() / lonCount * 2 * PI.toFloat()
                pt.project(cosLat * cos(lon) * rr, sinLat * rr, cosLat * sin(lon) * rr)
                val depth = (pt.pz / r + 1) / 2
                val crest = max(0f, w)
                frame.dot(pt.px, pt.py, pt.pz, (o.rBase + o.rDepth * depth) * (1 + 0.4f * crest) * rs, 0.66f - 0.56f * depth - 0.1f * crest)
            }
        }
    }

    private var nodes = FloatArray(0)

    private fun web(size: Float, t: Float, o: OrbProfile) {
        val r = size / 2 * 0.8f * o.spread
        val pt = Projection(t * 0.12f, 0.32f, size / 2, size / 2, r)
        val rs = radiusScale(size, o.rsPow)
        val n = o.nodeN
        if (nodes.size < n * 3) nodes = FloatArray(n * 3)
        for (i in 0 until n) {
            val (dx, dy, dz) = fibDir(i, n)
            val x = dx + 0.3f * (vnoise(i * 0.31f + 9, t * 0.24f) - 0.5f) * 2
            val y = dy + 0.3f * (vnoise(i * 0.53f + 27, t * 0.21f) - 0.5f) * 2
            val z = dz + 0.3f * (vnoise(i * 0.77f + 55, t * 0.27f) - 0.5f) * 2
            val l = sqrt(x * x + y * y + z * z)
            nodes[i * 3] = x / l
            nodes[i * 3 + 1] = y / l
            nodes[i * 3 + 2] = z / l
        }
        for (i in 0 until n) {
            for (j in i + 1 until n) {
                val dx = nodes[i * 3] - nodes[j * 3]
                val dy = nodes[i * 3 + 1] - nodes[j * 3 + 1]
                val dz = nodes[i * 3 + 2] - nodes[j * 3 + 2]
                val dist = sqrt(dx * dx + dy * dy + dz * dz)
                if (dist >= o.thr) continue
                pt.project(nodes[i * 3], nodes[i * 3 + 1], nodes[i * 3 + 2])
                val x1 = pt.px
                val y1 = pt.py
                val z1 = pt.pz
                pt.project(nodes[j * 3], nodes[j * 3 + 1], nodes[j * 3 + 2])
                val depth = ((z1 + pt.pz) / 2 + 1) / 2
                frame.line(x1, y1, pt.px, pt.py, 0.42f, (1 - dist / o.thr) * (0.3f + 0.55f * depth), max(0.6f, o.lineW * rs))
            }
        }
        for (i in 0 until n) {
            pt.project(nodes[i * 3], nodes[i * 3 + 1], nodes[i * 3 + 2])
            val depth = (pt.pz + 1) / 2
            val pulse = 1 + 0.25f * sin(t * 1.4f + i * 2.7f)
            frame.dot(pt.px, pt.py, pt.pz, (o.nodeR + o.nodeRDepth * depth) * pulse * rs, 0.55f - 0.45f * depth)
        }
        for (s in 0 until o.signals) {
            val seg = floor(t * 0.55f + s * 7.31f).toDouble()
            val a = (hashD(seg, s * 3.1 + 1.7) * n).toInt()
            val b = (hashD(seg, s * 5.7 + 4.2) * n).toInt()
            if (a == b) continue
            val f = frac(t * 0.55f + s * 7.31f)
            val x = lerp(nodes[a * 3], nodes[b * 3], f)
            val y = lerp(nodes[a * 3 + 1], nodes[b * 3 + 1], f)
            val z = lerp(nodes[a * 3 + 2], nodes[b * 3 + 2], f)
            val l = max(1e-6f, sqrt(x * x + y * y + z * z))
            pt.project(x / l, y / l, z / l)
            val depth = (pt.pz + 1) / 2
            frame.dot(pt.px, pt.py, pt.pz, (o.nodeR * 1.5f + o.nodeRDepth * depth) * rs, 0.05f, 0.5f + 0.5f * depth)
        }
    }

    private fun braid(size: Float, t: Float, o: OrbProfile) {
        val r = size / 2 * 0.76f
        val pt = Projection(t * 0.4f, 0.3f, size / 2, size / 2, 1f)
        val rs = radiusScale(size, o.rsPow)
        ghostSphere(pt, r, rs, o.ghostN)
        for (s in 0 until 3) {
            val phase = s / 3f * 2 * PI.toFloat()
            for (i in 0 until o.strandN) {
                val u = (frac(i.toFloat() / o.strandN + t * 0.045f) * 2 - 1) * 0.96f
                val surf = sqrt(max(0f, 1 - u * u))
                val endFade = min(1f, (1 - abs(u)) / 0.1f)
                val ang = u * PI.toFloat() * o.turns + phase
                val weave = 1 + 0.075f * sin(u * PI.toFloat() * o.turns * 2 + phase * 2 + t * 0.8f)
                val rr = surf * r * weave
                pt.project(cos(ang) * rr, u * r * weave, sin(ang) * rr)
                val depth = (pt.pz / r + 1) / 2
                frame.dot(pt.px, pt.py, pt.pz, (o.rBase + o.rDepth * depth) * rs, 0.55f - 0.45f * depth, endFade * (0.45f + 0.55f * depth))
            }
        }
    }

    /** Ribbon and its face-on ring: lanes of dots undulating around a great circle. */
    private fun ribbon(size: Float, t: Float, o: OrbProfile) {
        val r = size / 2 * 0.78f
        val camTilt = 0.3f
        val pt = Projection(t * 0.1f * o.spin, camTilt, size / 2, size / 2, 1f)
        val rs = radiusScale(size, o.rsPow)
        ghostSphere(pt, r, rs, o.ghostN)
        val ya = t * 0.24f * o.spin
        val ta = if (o.faceOn) -camTilt else 0.55f + 0.3f * sin(t * 0.18f) * o.spin
        val ux = cos(ya)
        val uy = 0f
        val uz = sin(ya)
        val vx = -uz * sin(ta)
        val vy = cos(ta)
        val vz = ux * sin(ta)
        val nx = uy * vz - uz * vy
        val ny = uz * vx - ux * vz
        val nz = ux * vy - uy * vx
        val wobAmp = 0.23f * o.wobMul
        val baseR = if (o.faceOn) r / (1 + 0.85f * wobAmp) else r
        val lanes = max(1, (o.lanes * o.bandMul).roundToInt())
        val mid = (lanes - 1) / 2f
        for (w in 0 until lanes) {
            val laneOff = (w - mid) * 0.075f
            val edge = abs(w - mid) / max(1f, mid)
            for (k in 0 until o.segs) {
                val ang = k.toFloat() / o.segs * 2 * PI.toFloat()
                val wob = (0.16f * sin(ang * 3 - t * 1.7f + w * 0.22f) + 0.07f * sin(ang * 5 + t * 1.1f)) * o.wobMul
                val radial = if (o.faceOn) 1 + wob else 1f
                val off = if (o.faceOn) laneOff else laneOff + wob
                val x = ux * cos(ang) + vx * sin(ang) + nx * off
                val y = uy * cos(ang) + vy * sin(ang) + ny * off
                val z = uz * cos(ang) + vz * sin(ang) + nz * off
                val l = sqrt(x * x + y * y + z * z)
                val rr = baseR * radial
                pt.project(x / l * rr, y / l * rr, z / l * rr)
                val depth = (pt.pz / r + 1) / 2
                frame.dot(
                    pt.px, pt.py, pt.pz,
                    (o.rBase + o.rDepth * depth) * (1 - 0.25f * edge) * rs,
                    0.52f - 0.44f * depth + 0.18f * edge,
                    0.4f + 0.6f * depth,
                )
            }
        }
    }

    /** Faint dots on the sphere behind braid and ribbon. */
    private fun ghostSphere(pt: Projection, r: Float, rs: Float, n: Int) {
        for (i in 0 until n) {
            val (dx, dy, dz) = fibDir(i, n)
            pt.project(dx * r, dy * r, dz * r)
            val depth = (pt.pz / r + 1) / 2
            frame.dot(pt.px, pt.py, pt.pz, 0.8f * rs, 0.78f, 0.1f + 0.22f * depth)
        }
    }
}

private class Move(val axis: Int, val lo: Float, val hi: Float, val ang: Float)

private class Cycle(val amount: FloatArray, val active: Int)

private fun makeMoves(count: Int): List<Move> = List(count) { i ->
    val axis = min(2, (hashD(i.toDouble(), 2.3) * 3).toInt())
    val lo = -1 + 0.5f * min(3, (hashD(i.toDouble(), 5.9) * 4).toInt())
    val dir = if (hashD(i.toDouble(), 7.7) < 0.5f) 1 else -1
    Move(axis, lo, lo + 0.5f, dir * PI.toFloat() / 2)
}

/** Scrambles the cube one slice at a time, then solves it in reverse, then rests. */
private fun solveCycle(time: Float, count: Int, slotDur: Float, rest: Float): Cycle {
    val cyc = 2 * count * slotDur + rest
    val tc = time % cyc
    val amount = FloatArray(count)
    var active = -1
    if (tc < 2 * count * slotDur) {
        val slot = floor(tc / slotDur).toInt()
        val p = (tc - slot * slotDur) / slotDur
        val cl = min(1f, p / 0.7f)
        val ep = 1 - (1 - cl).pow(3)
        if (slot < count) {
            for (i in 0 until slot) amount[i] = 1f
            amount[slot] = ep
            active = slot
        } else {
            val u = 2 * count - 1 - slot
            for (i in 0 until u) amount[i] = 1f
            amount[u] = 1 - ep
            active = u
        }
    }
    return Cycle(amount, active)
}

private fun radiusScale(size: Float, pow: Float) = (size / 300f).pow(pow)

private fun lerp(a: Float, b: Float, f: Float) = a + (b - a) * f

private fun frac(x: Float) = x - floor(x)

/** The engine's sine hash; kept in doubles so large arguments hash as in the original. */
private fun hashD(a: Double, b: Double): Float {
    val h = sin(a * 12.9898 + b * 78.233) * 43758.5453
    return (h - floor(h)).toFloat()
}

private fun vnoise(x: Float, y: Float): Float {
    val xi = floor(x)
    val yi = floor(y)
    var fx = x - xi
    var fy = y - yi
    fx = fx * fx * (3 - 2 * fx)
    fy = fy * fy * (3 - 2 * fy)
    val a = hashD(xi.toDouble(), yi.toDouble())
    val b = hashD(xi + 1.0, yi.toDouble())
    val c = hashD(xi.toDouble(), yi + 1.0)
    val d = hashD(xi + 1.0, yi + 1.0)
    return a + (b - a) * fx + (c - a) * fy + (a - b - c + d) * fx * fy
}

private fun fibDir(i: Int, n: Int): Triple<Float, Float, Float> {
    val golden = PI * (3 - sqrt(5.0))
    val y = 1 - 2 * (i + 0.5) / n
    val rad = sqrt(1 - y * y)
    val a = i * golden
    return Triple((rad * cos(a)).toFloat(), y.toFloat(), (rad * sin(a)).toFloat())
}

private fun angleDelta(a: Float, b: Float) = atan2(sin(a - b), cos(a - b))
