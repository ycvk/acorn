package io.ycvk.acorn.core.ui

import androidx.compose.animation.core.SeekableTransitionState
import androidx.compose.runtime.withFrameNanos

/**
 * Moves to [target] once its content has been composed and drawn at the
 * start of the transition. A page's first frame can take several frames'
 * time; started this way that time passes before the motion and not during
 * it, so the motion is seen from its beginning.
 */
suspend fun <S> SeekableTransitionState<S>.animateOnceDrawn(target: S) {
    if (targetState != target) {
        seekTo(0f, target)
        withFrameNanos { }
    }
    animateTo(target)
}
