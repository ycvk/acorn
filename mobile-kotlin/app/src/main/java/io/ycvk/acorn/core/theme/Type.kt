package io.ycvk.acorn.core.theme

import androidx.compose.runtime.Immutable
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.LineHeightStyle
import androidx.compose.ui.unit.em
import androidx.compose.ui.unit.sp

/**
 * One sans family throughout (Noto Sans CJK on most devices). Display sizes
 * run large and tightly tracked, as on an editorial page; body text stays
 * compact and hierarchy below the titles comes from weight and grey steps.
 * Figures are tabular so times and counts line up, and mono marks metadata.
 */
@Immutable
data class AcornType(
    val display: TextStyle,
    val largeTitle: TextStyle,
    val title: TextStyle,
    val headline: TextStyle,
    val body: TextStyle,
    val reading: TextStyle,
    val callout: TextStyle,
    val footnote: TextStyle,
    val caption: TextStyle,
    val label: TextStyle,
    val eyebrow: TextStyle,
    val mono: TextStyle,
)

private val centered = LineHeightStyle(
    alignment = LineHeightStyle.Alignment.Center,
    trim = LineHeightStyle.Trim.None,
)

private fun sans(size: Double, line: Int, weight: FontWeight = FontWeight.Normal, tracking: Double = 0.0) = TextStyle(
    fontFamily = FontFamily.SansSerif,
    fontWeight = weight,
    fontSize = size.sp,
    lineHeight = line.sp,
    letterSpacing = tracking.em,
    lineHeightStyle = centered,
    fontFeatureSettings = "tnum",
)

internal val DefaultType = AcornType(
    display = sans(32.0, 38, FontWeight.SemiBold, -0.035),
    largeTitle = sans(26.0, 32, FontWeight.SemiBold, -0.03),
    title = sans(19.0, 26, FontWeight.SemiBold, -0.02),
    headline = sans(15.0, 20, FontWeight.Medium, -0.01),
    body = sans(15.0, 22, tracking = -0.005),
    reading = sans(15.0, 24, tracking = -0.005),
    callout = sans(14.0, 20),
    footnote = sans(13.0, 18),
    caption = sans(12.0, 16),
    label = sans(14.0, 20, FontWeight.Medium),
    eyebrow = sans(12.0, 16, FontWeight.Medium),
    mono = TextStyle(fontFamily = FontFamily.Monospace, fontSize = 12.5.sp, lineHeight = 19.sp),
)
