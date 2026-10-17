package io.ycvk.acorn.core.theme

import androidx.compose.runtime.Immutable
import androidx.compose.ui.graphics.Color

/**
 * Acorn's palette: neutral greys with one indigo accent, used sparingly for
 * the primary action, selection and links. Structure comes from 1px rules and
 * grey steps, not from colour; dark mode is a first-class peer of light.
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
    canvas = Color(0xFFFFFFFF),
    surface = Color(0xFFFFFFFF),
    surfaceMuted = Color(0xFFF4F4F5),
    line = Color(0xFFEAEAEC),
    lineStrong = Color(0xFFDCDCE0),
    ink = Color(0xFF1B1B1F),
    inkSoft = Color(0xFF6B6B76),
    inkFaint = Color(0xFFA0A0AA),
    accent = Color(0xFF5E6AD2),
    onAccent = Color(0xFFFFFFFF),
    accentSoft = Color(0xFFEEEFFC),
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
    canvas = Color(0xFF0F1012),
    surface = Color(0xFF151619),
    surfaceMuted = Color(0xFF1C1D21),
    line = Color(0xFF232428),
    lineStrong = Color(0xFF2F3137),
    ink = Color(0xFFEEEFF1),
    inkSoft = Color(0xFF8B8E98),
    inkFaint = Color(0xFF5D606B),
    accent = Color(0xFF7A82E8),
    onAccent = Color(0xFFFFFFFF),
    accentSoft = Color(0xFF1E2140),
    accentInk = Color(0xFFA9AEF5),
    success = Color(0xFF4CC38A),
    warning = Color(0xFFF0A040),
    warningSoft = Color(0xFF2E2414),
    warningInk = Color(0xFFF5B963),
    danger = Color(0xFFF2555A),
    dangerSoft = Color(0xFF2E1517),
    scrim = Color(0x99000000),
    shadow = Color(0xFF000000),
    isDark = true,
)
