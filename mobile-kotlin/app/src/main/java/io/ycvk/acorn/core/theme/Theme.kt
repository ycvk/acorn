package io.ycvk.acorn.core.theme

import androidx.compose.foundation.LocalIndication
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Shapes
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp

internal val LocalAcornColors = staticCompositionLocalOf { LightColors }
private val LocalAcornType = staticCompositionLocalOf { DefaultType }

object AcornTheme {
    val colors: AcornColors
        @Composable @ReadOnlyComposable get() = LocalAcornColors.current
    val type: AcornType
        @Composable @ReadOnlyComposable get() = LocalAcornType.current
}

/** Tight corners: chips and fields, buttons and blocks, cards and menus, sheets and dialogs. */
object AcornShapes {
    val small = RoundedCornerShape(6.dp)
    val medium = RoundedCornerShape(8.dp)
    val large = RoundedCornerShape(10.dp)
    val sheet = RoundedCornerShape(12.dp)
    val pill = RoundedCornerShape(percent = 50)
}

@Composable
fun AcornTheme(
    darkTheme: Boolean = isSystemInDarkTheme(),
    content: @Composable () -> Unit,
) {
    val colors = if (darkTheme) DarkColors else LightColors
    CompositionLocalProvider(
        LocalAcornColors provides colors,
        LocalAcornType provides DefaultType,
    ) {
        MaterialTheme(
            colorScheme = materialColors(colors),
            typography = materialTypography(DefaultType),
            shapes = Shapes(
                extraSmall = AcornShapes.small,
                small = AcornShapes.small,
                medium = AcornShapes.medium,
                large = AcornShapes.large,
                extraLarge = AcornShapes.sheet,
            ),
        ) {
            // Every clickable without its own indication dims under a soft wash instead of a ripple.
            CompositionLocalProvider(LocalIndication provides PressHighlight, content = content)
        }
    }
}

/** Material components (dialogs, sheets, switches) draw from the same palette. */
private fun materialColors(c: AcornColors) = if (c.isDark) {
    darkColorScheme(
        primary = c.accent, onPrimary = c.onAccent,
        primaryContainer = c.accentSoft, onPrimaryContainer = c.accentInk,
        secondary = c.inkSoft, onSecondary = c.canvas,
        secondaryContainer = c.surfaceMuted, onSecondaryContainer = c.ink,
        background = c.canvas, onBackground = c.ink,
        surface = c.surface, onSurface = c.ink,
        surfaceVariant = c.surfaceMuted, onSurfaceVariant = c.inkSoft,
        surfaceContainerLowest = c.canvas, surfaceContainerLow = c.surface,
        surfaceContainer = c.surface, surfaceContainerHigh = c.surface, surfaceContainerHighest = c.surfaceMuted,
        outline = c.lineStrong, outlineVariant = c.line,
        error = c.danger, onError = c.onAccent, errorContainer = c.dangerSoft, onErrorContainer = c.danger,
        scrim = c.scrim,
    )
} else {
    lightColorScheme(
        primary = c.accent, onPrimary = c.onAccent,
        primaryContainer = c.accentSoft, onPrimaryContainer = c.accentInk,
        secondary = c.inkSoft, onSecondary = c.canvas,
        secondaryContainer = c.surfaceMuted, onSecondaryContainer = c.ink,
        background = c.canvas, onBackground = c.ink,
        surface = c.surface, onSurface = c.ink,
        surfaceVariant = c.surfaceMuted, onSurfaceVariant = c.inkSoft,
        surfaceContainerLowest = c.canvas, surfaceContainerLow = c.surface,
        surfaceContainer = c.surface, surfaceContainerHigh = c.surface, surfaceContainerHighest = c.surfaceMuted,
        outline = c.lineStrong, outlineVariant = c.line,
        error = c.danger, onError = c.onAccent, errorContainer = c.dangerSoft, onErrorContainer = c.danger,
        scrim = c.scrim,
    )
}

private fun materialTypography(t: AcornType) = Typography(
    displayLarge = t.largeTitle,
    displayMedium = t.largeTitle,
    displaySmall = t.largeTitle,
    headlineLarge = t.title,
    headlineMedium = t.title,
    headlineSmall = t.title,
    titleLarge = t.title,
    titleMedium = t.headline,
    titleSmall = t.label,
    bodyLarge = t.body,
    bodyMedium = t.callout,
    bodySmall = t.footnote,
    labelLarge = t.label,
    labelMedium = t.footnote.copy(fontWeight = FontWeight.Medium),
    labelSmall = t.caption,
)
