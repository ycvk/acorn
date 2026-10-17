package io.ycvk.acorn.core.ui

import androidx.compose.animation.core.Animatable
import androidx.compose.foundation.border
import androidx.compose.foundation.interaction.InteractionSource
import androidx.compose.foundation.interaction.PressInteraction
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import io.ycvk.acorn.core.theme.AcornMotion
import io.ycvk.acorn.core.theme.AcornTheme
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

/**
 * Shrinks the element a little while pressed and springs it back on release.
 * Put it before clip and background so the whole surface scales.
 */
@Composable
fun Modifier.pressScale(interactionSource: InteractionSource, pressed: Float = 0.97f): Modifier {
    val scale = remember { Animatable(1f) }
    LaunchedEffect(interactionSource) {
        var press: Job? = null
        interactionSource.interactions.collect { interaction ->
            when (interaction) {
                is PressInteraction.Press -> press = launch { scale.animateTo(pressed, AcornMotion.press()) }
                is PressInteraction.Release, is PressInteraction.Cancel -> {
                    val pending = press
                    launch {
                        pending?.join()
                        scale.animateTo(1f, AcornMotion.release())
                    }
                }
            }
        }
    }
    return graphicsLayer {
        scaleX = scale.value
        scaleY = scale.value
    }
}

/**
 * Lifts a surface off the canvas with a soft, warm shadow. Shadows barely
 * read on a dark canvas, so there a hairline edge does the work.
 */
@Composable
fun Modifier.raised(shape: Shape, elevation: Dp = 12.dp): Modifier {
    val colors = AcornTheme.colors
    return if (colors.isDark) {
        border(0.5.dp, colors.lineStrong, shape)
    } else {
        shadow(
            elevation = elevation,
            shape = shape,
            clip = false,
            ambientColor = colors.shadow.copy(alpha = 0.35f),
            spotColor = colors.shadow.copy(alpha = 0.45f),
        )
    }
}
