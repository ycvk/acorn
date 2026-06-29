package io.ycvk.acorn.core.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.SideEffect
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.LineHeightStyle
import androidx.compose.ui.unit.sp

private val LightAetherColors = lightColorScheme(
    primary = LightAetherPalette.primary,
    onPrimary = LightAetherPalette.onPrimary,
    primaryContainer = LightAetherPalette.primaryContainer,
    onPrimaryContainer = LightAetherPalette.onPrimaryContainer,
    secondary = LightAetherPalette.secondary,
    onSecondary = LightAetherPalette.onSecondary,
    secondaryContainer = LightAetherPalette.secondaryContainer,
    onSecondaryContainer = LightAetherPalette.onSecondaryContainer,
    background = LightAetherPalette.background,
    surface = LightAetherPalette.surface,
    surfaceVariant = LightAetherPalette.surfaceVariant,
    onSurface = LightAetherPalette.onSurface,
    onSurfaceVariant = LightAetherPalette.onSurfaceVariant,
    tertiary = LightAetherPalette.tertiary,
    error = LightAetherPalette.error,
    outline = LightAetherPalette.outline,
)

private val DarkAetherColors = darkColorScheme(
    primary = DarkAetherPalette.primary,
    onPrimary = DarkAetherPalette.onPrimary,
    primaryContainer = DarkAetherPalette.primaryContainer,
    onPrimaryContainer = DarkAetherPalette.onPrimaryContainer,
    secondary = DarkAetherPalette.secondary,
    onSecondary = DarkAetherPalette.onSecondary,
    secondaryContainer = DarkAetherPalette.secondaryContainer,
    onSecondaryContainer = DarkAetherPalette.onSecondaryContainer,
    background = DarkAetherPalette.background,
    surface = DarkAetherPalette.surface,
    surfaceVariant = DarkAetherPalette.surfaceVariant,
    onSurface = DarkAetherPalette.onSurface,
    onSurfaceVariant = DarkAetherPalette.onSurfaceVariant,
    tertiary = DarkAetherPalette.tertiary,
    error = DarkAetherPalette.error,
    outline = DarkAetherPalette.outline,
)

private val trim = LineHeightStyle(
    alignment = LineHeightStyle.Alignment.Top,
    trim = LineHeightStyle.Trim.LastLineBottom,
)

private val AetherTypography = Typography(
    headlineLarge = TextStyle(
        fontFamily = FontFamily.SansSerif,
        fontWeight = FontWeight.SemiBold,
        fontSize = 34.sp,
        lineHeight = 40.sp,
        letterSpacing = (-0.9).sp,
        lineHeightStyle = trim,
    ),
    headlineMedium = TextStyle(
        fontFamily = FontFamily.SansSerif,
        fontWeight = FontWeight.SemiBold,
        fontSize = 29.sp,
        lineHeight = 36.sp,
        letterSpacing = (-0.5).sp,
        lineHeightStyle = trim,
    ),
    titleLarge = TextStyle(
        fontFamily = FontFamily.SansSerif,
        fontWeight = FontWeight.SemiBold,
        fontSize = 24.sp,
        lineHeight = 31.sp,
        lineHeightStyle = trim,
    ),
    titleMedium = TextStyle(
        fontFamily = FontFamily.SansSerif,
        fontWeight = FontWeight.Medium,
        fontSize = 18.sp,
        lineHeight = 25.sp,
        lineHeightStyle = trim,
    ),
    bodyLarge = TextStyle(
        fontFamily = FontFamily.SansSerif,
        fontWeight = FontWeight.Normal,
        fontSize = 17.sp,
        lineHeight = 28.sp,
    ),
    bodyMedium = TextStyle(
        fontFamily = FontFamily.SansSerif,
        fontWeight = FontWeight.Normal,
        fontSize = 15.sp,
        lineHeight = 24.sp,
    ),
    bodySmall = TextStyle(
        fontFamily = FontFamily.SansSerif,
        fontWeight = FontWeight.Normal,
        fontSize = 13.sp,
        lineHeight = 18.sp,
    ),
    labelLarge = TextStyle(
        fontFamily = FontFamily.SansSerif,
        fontWeight = FontWeight.Medium,
        fontSize = 14.sp,
        lineHeight = 20.sp,
    ),
    labelMedium = TextStyle(
        fontFamily = FontFamily.SansSerif,
        fontWeight = FontWeight.Medium,
        fontSize = 13.sp,
        lineHeight = 18.sp,
    ),
    labelSmall = TextStyle(
        fontFamily = FontFamily.SansSerif,
        fontWeight = FontWeight.Medium,
        fontSize = 11.sp,
        lineHeight = 16.sp,
    ),
)

@Composable
fun AcornTheme(
    darkTheme: Boolean = isSystemInDarkTheme(),
    content: @Composable () -> Unit,
) {
    SideEffect {
        updateAetherPalette(darkTheme)
    }
    MaterialTheme(
        colorScheme = if (darkTheme) DarkAetherColors else LightAetherColors,
        typography = AetherTypography,
        content = content,
    )
}
