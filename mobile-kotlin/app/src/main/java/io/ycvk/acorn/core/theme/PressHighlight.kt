package io.ycvk.acorn.core.theme

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.tween
import androidx.compose.foundation.IndicationNodeFactory
import androidx.compose.foundation.interaction.InteractionSource
import androidx.compose.foundation.interaction.PressInteraction
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.drawscope.ContentDrawScope
import androidx.compose.ui.node.CompositionLocalConsumerModifierNode
import androidx.compose.ui.node.DelegatableNode
import androidx.compose.ui.node.DrawModifierNode
import androidx.compose.ui.node.currentValueOf
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

/**
 * A pressed surface dims under a soft ink wash. A quick tap still shows the
 * whole wash: the fade-out waits for the fade-in to finish.
 */
internal object PressHighlight : IndicationNodeFactory {
    override fun create(interactionSource: InteractionSource): DelegatableNode = PressHighlightNode(interactionSource)

    override fun equals(other: Any?): Boolean = other === this

    override fun hashCode(): Int = javaClass.hashCode()
}

private class PressHighlightNode(
    private val interactionSource: InteractionSource,
) : Modifier.Node(), DrawModifierNode, CompositionLocalConsumerModifierNode {
    private val strength = Animatable(0f)

    override fun onAttach() {
        coroutineScope.launch {
            var press: Job? = null
            interactionSource.interactions.collect { interaction ->
                when (interaction) {
                    is PressInteraction.Press -> press = launch { strength.animateTo(1f, tween(AcornMotion.SHORT / 2)) }
                    is PressInteraction.Release, is PressInteraction.Cancel -> {
                        val pending = press
                        launch {
                            pending?.join()
                            strength.animateTo(0f, AcornMotion.standard(AcornMotion.MEDIUM))
                        }
                    }
                }
            }
        }
    }

    override fun ContentDrawScope.draw() {
        drawContent()
        val amount = strength.value
        if (amount > 0f) {
            val colors = currentValueOf(LocalAcornColors)
            drawRect(colors.ink.copy(alpha = amount * if (colors.isDark) 0.09f else 0.055f))
        }
    }
}
