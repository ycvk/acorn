package io.ycvk.acorn.core.theme

import androidx.compose.runtime.Immutable
import androidx.compose.ui.graphics.Color

/**
 * Acorn's palette: warm paper neutrals and one acorn-brown accent.
 * Three planes stack by lightness: the canvas, white surfaces on it, and muted fills inside surfaces.
 */
@Immutable
data class AcornColors(
    val canvas: Color,
    val surface: Color,
    val surfaceMuted: Color,
    val line: Color,
    val lineStrong: Color,
    val ink: Color,
    val inkSoft: Color,
    val inkFaint: Color,
    val accent: Color,
    val onAccent: Color,
    val accentSoft: Color,
    val accentInk: Color,
    val bubble: Color,
    val success: Color,
    val warning: Color,
    val warningSoft: Color,
    val warningInk: Color,
    val danger: Color,
    val dangerSoft: Color,
    val scrim: Color,
    val shadow: Color,
    val isDark: Boolean,
)

internal val LightColors = AcornColors(
    canvas = Color(0xFFF4F2EC),
    surface = Color(0xFFFFFEFB),
    surfaceMuted = Color(0xFFEDEAE3),
    line = Color(0xFFE7E3DA),
    lineStrong = Color(0xFFD6D1C6),
    ink = Color(0xFF1C1A17),
    inkSoft = Color(0xFF5C574F),
    inkFaint = Color(0xFF98928A),
    accent = Color(0xFF8A5A2B),
    onAccent = Color(0xFFFFFFFF),
    accentSoft = Color(0xFFF2E8DB),
    accentInk = Color(0xFF6B421C),
    bubble = Color(0xFFEBE3D7),
    success = Color(0xFF3F7A52),
    warning = Color(0xFFA86A12),
    warningSoft = Color(0xFFF7EDDB),
    warningInk = Color(0xFF7A4A07),
    danger = Color(0xFFB5402F),
    dangerSoft = Color(0xFFF8E6E2),
    scrim = Color(0x59000000),
    shadow = Color(0xFF4A3520),
    isDark = false,
)

internal val DarkColors = AcornColors(
    canvas = Color(0xFF131211),
    surface = Color(0xFF1E1D1B),
    surfaceMuted = Color(0xFF2A2826),
    line = Color(0xFF2E2C29),
    lineStrong = Color(0xFF3D3A36),
    ink = Color(0xFFEFECE5),
    inkSoft = Color(0xFFB4AEA4),
    inkFaint = Color(0xFF7C776E),
    accent = Color(0xFFD9A86C),
    onAccent = Color(0xFF241708),
    accentSoft = Color(0xFF35291C),
    accentInk = Color(0xFFEBC896),
    bubble = Color(0xFF2E2923),
    success = Color(0xFF7DBB8E),
    warning = Color(0xFFE0A84E),
    warningSoft = Color(0xFF382D1A),
    warningInk = Color(0xFFF0C982),
    danger = Color(0xFFE8806F),
    dangerSoft = Color(0xFF3B221D),
    scrim = Color(0x99000000),
    shadow = Color(0xFF000000),
    isDark = true,
)
