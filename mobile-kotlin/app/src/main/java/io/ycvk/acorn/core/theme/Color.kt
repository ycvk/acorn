package io.ycvk.acorn.core.theme

import androidx.compose.runtime.Immutable
import androidx.compose.ui.graphics.Color

/** Acorn's palette: warm paper neutrals and one acorn-brown accent. */
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
    val success: Color,
    val warning: Color,
    val warningSoft: Color,
    val warningInk: Color,
    val danger: Color,
    val dangerSoft: Color,
    val scrim: Color,
    val isDark: Boolean,
)

internal val LightColors = AcornColors(
    canvas = Color(0xFFF7F5F0),
    surface = Color(0xFFFDFCF9),
    surfaceMuted = Color(0xFFEFECE5),
    line = Color(0xFFE6E2D9),
    lineStrong = Color(0xFFD3CEC3),
    ink = Color(0xFF1F1D1A),
    inkSoft = Color(0xFF5E5A53),
    inkFaint = Color(0xFF9A958C),
    accent = Color(0xFF8A5A2B),
    onAccent = Color(0xFFFFFFFF),
    accentSoft = Color(0xFFF1E6D8),
    accentInk = Color(0xFF6B421C),
    success = Color(0xFF3F7A52),
    warning = Color(0xFFA86A12),
    warningSoft = Color(0xFFF6EBD6),
    warningInk = Color(0xFF7A4A07),
    danger = Color(0xFFB5402F),
    dangerSoft = Color(0xFFF7E3DF),
    scrim = Color(0x52000000),
    isDark = false,
)

internal val DarkColors = AcornColors(
    canvas = Color(0xFF161513),
    surface = Color(0xFF1E1D1A),
    surfaceMuted = Color(0xFF2A2825),
    line = Color(0xFF2F2D29),
    lineStrong = Color(0xFF3D3A35),
    ink = Color(0xFFEDEAE3),
    inkSoft = Color(0xFFB3ADA2),
    inkFaint = Color(0xFF7C776E),
    accent = Color(0xFFD9A86C),
    onAccent = Color(0xFF241708),
    accentSoft = Color(0xFF3A2C1C),
    accentInk = Color(0xFFEBC896),
    success = Color(0xFF7DBB8E),
    warning = Color(0xFFE0A84E),
    warningSoft = Color(0xFF3A2E19),
    warningInk = Color(0xFFF0C982),
    danger = Color(0xFFE8806F),
    dangerSoft = Color(0xFF3D211C),
    scrim = Color(0x8A000000),
    isDark = true,
)
