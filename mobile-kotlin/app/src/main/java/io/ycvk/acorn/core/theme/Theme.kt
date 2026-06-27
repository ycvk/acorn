package io.ycvk.acorn.core.theme

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable

// Terminal aesthetic — dark-only, no dynamic color.
private val AcornColors = darkColorScheme(
    primary = Accent,
    onPrimary = OnAccent,
    primaryContainer = AccentDim,
    onPrimaryContainer = OnAccent,
    secondary = Info,
    onSecondary = OnAccent,
    tertiary = Warning,
    onTertiary = Bg,
    error = Danger,
    onError = OnAccent,
    background = Bg,
    onBackground = TextPrimary,
    surface = Surface,
    onSurface = TextPrimary,
    surfaceVariant = SurfaceVariant,
    onSurfaceVariant = TextSecondary,
    surfaceContainer = Surface,
    outline = Border,
    outlineVariant = Border,
)

@Composable
fun AcornTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = AcornColors,
        typography = AcornTypography,
        shapes = AcornShapes,
        content = content,
    )
}
