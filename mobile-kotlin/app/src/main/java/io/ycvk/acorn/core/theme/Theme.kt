package io.ycvk.acorn.core.theme

import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalContext

private val LightColors = lightColorScheme(
    primary = AcornPrimary,
    onPrimary = AcornOnPrimary,
    primaryContainer = AcornPrimaryContainer,
    onPrimaryContainer = AcornOnPrimaryContainer,
    secondary = AcornSecondary,
    onSecondary = AcornOnSecondary,
    secondaryContainer = AcornSecondaryContainer,
    surface = AcornSurface,
    onSurface = AcornOnSurface,
    background = AcornBackground,
)

private val DarkColors = darkColorScheme(
    primary = AcornPrimary,
    onPrimary = AcornOnPrimary,
    primaryContainer = AcornPrimaryContainer,
    onPrimaryContainer = AcornOnPrimaryContainer,
)

@Composable
fun AcornTheme(
    darkTheme: Boolean = isSystemInDarkTheme(),
    dynamicColor: Boolean = true,
    content: @Composable () -> Unit,
) {
    val colorScheme = when {
        dynamicColor && Build.VERSION.SDK_INT >= Build.VERSION_CODES.S -> {
            val context = LocalContext.current
            if (darkTheme) dynamicDarkColorScheme(context) else dynamicLightColorScheme(context)
        }
        darkTheme -> DarkColors
        else -> LightColors
    }
    MaterialTheme(
        colorScheme = colorScheme,
        typography = AcornTypography,
        content = content,
    )
}
