package io.ycvk.acorn.core.theme

import androidx.compose.runtime.Immutable
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.LineHeightStyle
import androidx.compose.ui.unit.em
import androidx.compose.ui.unit.sp

/**
 * Chrome is sans (Noto Sans CJK on most devices); what the agent writes and
 * notes read in serif (Noto Serif CJK), so the speaker shows before a word is read.
 * Sans figures are tabular so times and counts line up in lists.
 */
@Immutable
data class AcornType(
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
    largeTitle = sans(32.0, 40, FontWeight.Bold, -0.02),
    title = sans(21.0, 28, FontWeight.SemiBold, -0.01),
    headline = sans(16.0, 22, FontWeight.Medium),
    body = sans(16.0, 24),
    reading = TextStyle(
        fontFamily = FontFamily.Serif,
        fontSize = 16.5.sp,
        lineHeight = 28.sp,
        letterSpacing = 0.01.em,
        lineHeightStyle = centered,
    ),
    callout = sans(15.0, 21),
    footnote = sans(13.5, 19),
    caption = sans(12.0, 16),
    label = sans(15.0, 20, FontWeight.Medium),
    eyebrow = sans(13.0, 18, FontWeight.Medium, 0.02),
    mono = TextStyle(fontFamily = FontFamily.Monospace, fontSize = 13.sp, lineHeight = 20.sp),
)
