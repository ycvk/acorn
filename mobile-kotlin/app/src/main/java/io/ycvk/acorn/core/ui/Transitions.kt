package io.ycvk.acorn.core.ui

import androidx.compose.animation.core.FiniteAnimationSpec
import androidx.compose.animation.core.SeekableTransitionState
import androidx.compose.runtime.withFrameNanos

/**
 * Moves to [target] once its content has been composed and drawn at the
 * start of the transition. A page's first frame can take several frames'
 * time; started this way that time passes before the motion and not during
 * it, so the motion is seen from its beginning. [pace] says how the
 * transition's progress runs from where it stands; without one it runs
 * evenly over the transition's own time.
 */
suspend fun <S> SeekableTransitionState<S>.animateOnceDrawn(target: S, pace: (fraction: Float) -> FiniteAnimationSpec<Float>? = { null }) {
    if (targetState != target) {
        seekTo(0f, target)
        withFrameNanos { }
    }
    animateTo(target, pace(fraction))
}
