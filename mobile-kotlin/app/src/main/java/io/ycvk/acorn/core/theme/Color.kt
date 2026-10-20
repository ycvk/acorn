package io.ycvk.acorn.core.theme

import androidx.compose.runtime.Immutable
import androidx.compose.ui.graphics.Color

/**
 * Acorn's palette: graphite greys with one indigo accent for the primary
 * action, selection and links. Cards sit a step above the canvas and wells a
 * step below their card; colour otherwise belongs to the living effects (the
 * orb, the beam, the metal ring). Dark mode is a first-class peer of light.
 */
@Immutable
data class AcornColors(
    val canvas: Color,
    val surface: Color,
    val surfaceMuted: Color,
    val well: Color,
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
    val shadow: Color,
    val isDark: Boolean,
)

internal val LightColors = AcornColors(
    canvas = Color(0xFFF4F4F5),
    surface = Color(0xFFFFFFFF),
    surfaceMuted = Color(0xFFEDEDEF),
    well = Color(0xFFF7F7F8),
    line = Color(0xFFE6E6E9),
    lineStrong = Color(0xFFD7D7DC),
    ink = Color(0xFF131315),
    inkSoft = Color(0xFF68686F),
    inkFaint = Color(0xFFA0A0A8),
    accent = Color(0xFF5E6AD2),
    onAccent = Color(0xFFFFFFFF),
    accentSoft = Color(0xFFECEDFB),
    accentInk = Color(0xFF4852C2),
    success = Color(0xFF26A269),
    warning = Color(0xFFE5871A),
    warningSoft = Color(0xFFFDF3E6),
    warningInk = Color(0xFFB2620A),
    danger = Color(0xFFE5484D),
    dangerSoft = Color(0xFFFDECEC),
    scrim = Color(0x59000000),
    shadow = Color(0xFF000000),
    isDark = false,
)

internal val DarkColors = AcornColors(
    canvas = Color(0xFF0E0E0F),
    surface = Color(0xFF18181A),
    surfaceMuted = Color(0xFF232326),
    well = Color(0xFF121214),
    line = Color(0xFF242427),
    lineStrong = Color(0xFF313135),
    ink = Color(0xFFEDEDEF),
    inkSoft = Color(0xFF9C9CA3),
    inkFaint = Color(0xFF606067),
    accent = Color(0xFF7C84F0),
    onAccent = Color(0xFFFFFFFF),
    accentSoft = Color(0xFF1E2042),
    accentInk = Color(0xFFAAB0F7),
    success = Color(0xFF4CC38A),
    warning = Color(0xFFF0A040),
    warningSoft = Color(0xFF2B2213),
    warningInk = Color(0xFFF5B963),
    danger = Color(0xFFF2555A),
    dangerSoft = Color(0xFF2C1516),
    scrim = Color(0x99000000),
    shadow = Color(0xFF000000),
    isDark = true,
)
