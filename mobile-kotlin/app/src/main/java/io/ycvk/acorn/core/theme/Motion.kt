package io.ycvk.acorn.core.theme

import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.FiniteAnimationSpec
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.SpringSpec
import androidx.compose.animation.core.spring
import androidx.compose.animation.core.tween

/**
 * Durations step 150 / 250 / 400 ms. Things arriving decelerate, things leaving
 * accelerate; touch feedback and moving indicators run on springs so they can be
 * interrupted mid-flight without a jump.
 */
object AcornMotion {
    const val SHORT = 150
    const val MEDIUM = 250
    const val LONG = 400

    val standard = CubicBezierEasing(0.2f, 0f, 0f, 1f)
    val decelerate = CubicBezierEasing(0.05f, 0.7f, 0.1f, 1f)
    val accelerate = CubicBezierEasing(0.3f, 0f, 0.8f, 0.15f)

    fun <T> standard(duration: Int = MEDIUM): FiniteAnimationSpec<T> = tween(duration, easing = standard)
    fun <T> enter(duration: Int = MEDIUM): FiniteAnimationSpec<T> = tween(duration, easing = decelerate)
    fun <T> exit(duration: Int = SHORT): FiniteAnimationSpec<T> = tween(duration, easing = accelerate)

    /** A finger landing on a surface: quick, no overshoot. */
    fun <T> press(): SpringSpec<T> = spring(dampingRatio = Spring.DampingRatioNoBouncy, stiffness = 1400f)

    /** The surface coming back up, with a small settle. */
    fun <T> release(): SpringSpec<T> = spring(dampingRatio = 0.55f, stiffness = 500f)

    /** Indicators and thumbs gliding to a new place. */
    fun <T> glide(): SpringSpec<T> = spring(dampingRatio = 0.82f, stiffness = 420f)

    /** Small things appearing, such as a badge or a send button. */
    fun <T> pop(): SpringSpec<T> = spring(dampingRatio = 0.5f, stiffness = 520f)
}
