package io.ycvk.acorn.core.theme

import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.FiniteAnimationSpec
import androidx.compose.animation.core.tween

object AcornMotion {
    val easing = CubicBezierEasing(0.2f, 0f, 0f, 1f)
    const val SHORT = 160
    const val MEDIUM = 240

    fun <T> standard(duration: Int = MEDIUM): FiniteAnimationSpec<T> = tween(duration, easing = easing)
}
