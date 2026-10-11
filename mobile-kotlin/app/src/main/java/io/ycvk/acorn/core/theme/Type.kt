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
    val mono: TextStyle,
)

private val centered = LineHeightStyle(
    alignment = LineHeightStyle.Alignment.Center,
    trim = LineHeightStyle.Trim.None,
)

private fun sans(size: Int, line: Int, weight: FontWeight = FontWeight.Normal, tracking: Double = 0.0) = TextStyle(
    fontFamily = FontFamily.SansSerif,
    fontWeight = weight,
    fontSize = size.sp,
    lineHeight = line.sp,
    letterSpacing = tracking.em,
    lineHeightStyle = centered,
)

internal val DefaultType = AcornType(
    largeTitle = sans(30, 38, FontWeight.SemiBold, -0.01),
    title = sans(20, 28, FontWeight.SemiBold),
    headline = sans(16, 23, FontWeight.Medium),
    body = sans(16, 25),
    reading = TextStyle(
        fontFamily = FontFamily.Serif,
        fontSize = 17.sp,
        lineHeight = 30.sp,
        lineHeightStyle = centered,
    ),
    callout = sans(15, 22),
    footnote = sans(13, 19),
    caption = sans(12, 16),
    label = sans(15, 20, FontWeight.Medium),
    mono = TextStyle(fontFamily = FontFamily.Monospace, fontSize = 13.sp, lineHeight = 20.sp),
)
