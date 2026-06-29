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
 * Top fade overlay — background (opaque) to transparent.
 * Fades scrolling content into the background at the top edge.
 */
fun Modifier.topFadeOverlay(heightFraction: Float = 0.06f): Modifier = this.drawWithCache {
    val fadeHeight = size.height * heightFraction
    val grad = Brush.verticalGradient(
        colorStops = arrayOf(
            0.0f to AetherBackground.copy(alpha = 0.98f),
            0.28f to AetherBackground.copy(alpha = 0.92f),
            0.58f to AetherBackground.copy(alpha = 0.52f),
            0.82f to AetherBackground.copy(alpha = 0.18f),
            1.0f to Color.Transparent,
        ),
        startY = 0f,
        endY = fadeHeight * 7f,
    )
    onDrawBehind {
        drawRect(grad)
    }
}

/**
 * Composer shadow — subtle shadow beneath the composer card.
 */
val ComposerShadow = Color(0x20000000)

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
