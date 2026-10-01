package io.ycvk.acorn.core.theme

import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawWithCache
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color

/**
 * Subtle vertical gradient — AetherBackgroundGradientTop fading to AetherBackground.
 * Matches Aether's background rendering.
 */
fun Modifier.gradientBackground(): Modifier = this.drawWithCache {
    val grad = Brush.verticalGradient(
        colors = listOf(AetherBackgroundGradientTop, AetherBackground),
        startY = 0f,
        endY = size.height,
    )
    onDrawBehind {
        drawRect(grad)
    }
}

/**
 * Subtle radial glow — used on loading screen behind spinner.
 */
fun Modifier.accentGlow(): Modifier = this.drawWithCache {
    val glow = Brush.radialGradient(
        colors = listOf(
            AetherPrimaryContainer.copy(alpha = 0.5f),
            Color.Transparent,
        ),
        center = Offset(size.width * 0.5f, size.height * 0.35f),
        radius = size.maxDimension * 0.8f,
    )
    onDrawBehind {
        drawRect(glow)
    }
}
